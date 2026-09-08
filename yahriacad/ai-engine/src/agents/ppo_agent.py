"""Proximal Policy Optimization agent and trainer for the routing env.

torch is imported lazily (inside functions/factories): importing this module
NEVER requires torch, which keeps the gRPC service runnable with the pure
A* fallback. The ``ActorCritic`` class is built on first use through the
module-level ``__getattr__`` (PEP 562) so ``from ppo_agent import ActorCritic``
keeps working when torch is installed.
"""

from __future__ import annotations

import os
import random
from dataclasses import dataclass
from typing import Any

import numpy as np

from .base_agent import BaseAgent, RolloutBuffer


def _torch():
    """Import and return the torch module lazily."""
    import torch

    return torch


# Cache for the lazily-built ActorCritic classes, keyed by architecture name.
_ACTOR_CRITIC_CLS: dict = {}

# Architectures disponibles (voir :func:`_actor_critic_class`).
_ARCHITECTURES = ("base", "compass", "wide")


def _actor_critic_class(arch: str = "base"):
    """Build (once) and return the ``ActorCritic`` nn.Module class for ``arch``.

    Architectures :

    * ``base`` — CNN historique : EXACTEMENT le format des checkpoints
      existants (state_dict inchange, retro-compatibilite totale) ;
    * ``compass`` — ``base`` + plan de boussole vers la cible : 2 canaux
      (dx, dy) normalises, calcules a la resolution 1/8 dans le forward
      depuis le canal cible et le canal position de l'observation, concat
      aux features du tronc avant une tete conv 1x1 supplementaire dont les
      logits sont echantillonnes a la cellule de l'agent. Corrige le goulot
      d'information de la branche globale (la direction de la cible n'est
      portee que par un pool avg+max -> fc 256) identifie comme plafond
      structurel du one-shot ;
    * ``wide`` — queue du tronc elargie a 128 canaux (conv1/conv2 inchanges
      donc transferables depuis ``base``), global_fc 2*128*36 -> 256,
      fusion 384 -> 256 ; ~2,8M parametres contre ~1,4M.

    Toutes les archis partagent le masquage d'actions invalides et les memes
    arguments de construction.
    """
    arch = str(arch or "base")
    if arch in _ACTOR_CRITIC_CLS:
        return _ACTOR_CRITIC_CLS[arch]
    if arch not in _ARCHITECTURES:
        raise ValueError(
            f"architecture inconnue : {arch!r} (choix : {'|'.join(_ARCHITECTURES)})"
        )
    torch = _torch()
    nn = torch.nn
    wide = arch == "wide"
    compass = arch == "compass"
    deep = 128 if wide else 64

    class _ActorCritic(nn.Module):
        """CNN actor-critic over multi-channel grid observations.

        Conv backbone (stride 2 x3, 1/8 resolution) feeding TWO heads:

        * a fully-convolutional ``local_head`` producing per-cell action
          logits, sampled at the agent cell (recovered from the graded
          position plane): this gives real local vision - walls and
          neighbours at 1/8 resolution, translation equivariant;
        * a global branch (dual avg+max pooling -> fc) carrying the target
          direction over long distances, fused with the local features
          before the policy/value heads.

        Final logits = policy head + local head (reactive ensemble).

        ``compass`` adds a third per-cell logits source: a target-heading
        plane (2 channels, unit vector towards the target centroid) fused
        with the trunk features through a 1x1 conv — every cell knows WHERE
        the target is without squeezing it through the global pooling.

        Args:
            obs_shape: observation shape ``(channels, H, W)`` or plainly
                the number of channels (int) - only the channel count is
                consumed by the CNN encoder.
            n_actions: size of the discrete action space.
            hidden: width of the fully-connected layers.
            pool: side of the global pooling grids.
        """

        def __init__(
            self,
            obs_shape,
            n_actions: int,
            hidden: int = 256,
            pool: int = 6,
        ) -> None:
            super().__init__()
            in_channels = obs_shape[0] if isinstance(obs_shape, (tuple, list)) else int(obs_shape)
            self.n_actions = int(n_actions)
            self.arch = arch
            self.stride = 8  # 3 convs stride 2
            self.conv = nn.Sequential(
                nn.Conv2d(in_channels, 32, kernel_size=3, stride=2, padding=1),
                nn.ReLU(),
                nn.Conv2d(32, 64, kernel_size=3, stride=2, padding=1),
                nn.ReLU(),
                nn.Conv2d(64, deep, kernel_size=3, stride=2, padding=1),
                nn.ReLU(),
                # Convs dilatees (stride 1) : elargissent le champ
                # receptif (~152 px pleine resolution) pour voir autour
                # des obstacles et eviter les minimaux locaux du champ
                # reactif (aller-retours entre deux cellules).
                nn.Conv2d(deep, deep, kernel_size=3, padding=2, dilation=2),
                nn.ReLU(),
                nn.Conv2d(deep, deep, kernel_size=3, padding=4, dilation=4),
                nn.ReLU(),
            )
            # Branch globale : le max preserve les pics epars (position,
            # pads) que la moyenne dilue ; l'avg decrit le contexte.
            self.pool_avg = nn.AdaptiveAvgPool2d((pool, pool))
            self.pool_max = nn.AdaptiveMaxPool2d((pool, pool))
            self.global_fc = nn.Sequential(
                nn.Linear(2 * deep * pool * pool, hidden), nn.ReLU()
            )
            # Branch locale : logits par cellule a la resolution 1/8.
            self.local_head = nn.Conv2d(deep, self.n_actions, kernel_size=1)
            if compass:
                # Boussole cible : feats (deep) + plan (dx, dy) -> logits.
                self.target_head = nn.Conv2d(deep + 2, self.n_actions, kernel_size=1)
            self.fusion = nn.Sequential(
                nn.Linear(hidden + deep, hidden), nn.ReLU()
            )
            self.policy_head = nn.Linear(hidden, n_actions)
            self.value_head = nn.Linear(hidden, 1)

        def _target_compass(self, x, layer_count: int, h8: int, w8: int):
            """Boussole (B, 2, h8, w8) : vecteur unitaire vers la cible.

            Le centroide des cellules cibles restantes (canal
            ``layer_count + 1``) est compare aux centres des cellules 1/8 ;
            aucun parametre appris ici — le reseau n'a plus qu'a apprendre
            a SUIVRE la direction (et a la negocier avec les murs).
            """
            batch, _c, height, width = x.shape
            dt = x.dtype
            tgt = x[:, layer_count + 1]
            mass = tgt.sum(dim=(1, 2))
            ys = torch.arange(height, device=x.device, dtype=dt)
            xs = torch.arange(width, device=x.device, dtype=dt)
            ty = (tgt.sum(dim=2) * ys).sum(dim=1) / mass.clamp(min=1e-6)
            tx = (tgt.sum(dim=1) * xs).sum(dim=1) / mass.clamp(min=1e-6)
            half = (self.stride - 1) / 2.0
            cy8 = torch.arange(h8, device=x.device, dtype=dt) * self.stride + half
            cx8 = torch.arange(w8, device=x.device, dtype=dt) * self.stride + half
            dyy = (ty.view(batch, 1) - cy8.view(1, h8)).view(batch, h8, 1).expand(batch, h8, w8)
            dxx = (tx.view(batch, 1) - cx8.view(1, w8)).view(batch, 1, w8).expand(batch, h8, w8)
            dist = torch.sqrt(dxx * dxx + dyy * dyy).clamp(min=1.0)
            plane = torch.stack((dxx / dist, dyy / dist), dim=1)
            empty = (mass <= 0).view(batch, 1, 1, 1)
            return torch.where(empty, torch.zeros_like(plane), plane)

        def forward(self, x):  # type: (torch.Tensor) -> Tuple[torch.Tensor, torch.Tensor]
            batch, channels, height, width = x.shape
            layer_count = channels - 4
            feats = self.conv(x)
            h8, w8 = feats.shape[2], feats.shape[3]
            # Position de l'agent : centre unique du plan blob (canal -1).
            flat_pos = x[:, -1].reshape(batch, -1)
            idx = flat_pos.argmax(dim=1)
            py = idx // width
            px = idx % width
            gy = (py // self.stride).clamp(0, h8 - 1)
            gx = (px // self.stride).clamp(0, w8 - 1)
            rows = torch.arange(batch, device=x.device)
            local_feat = feats[rows, :, gy, gx]
            local_logits = self.local_head(feats)[rows, :, gy, gx]
            if compass:
                compass_map = self.target_head(
                    torch.cat((feats, self._target_compass(x, layer_count, h8, w8)), dim=1)
                )
                target_logits = compass_map[rows, :, gy, gx]
            else:
                target_logits = 0.0
            pooled = torch.cat(
                (self.pool_avg(feats).flatten(1), self.pool_max(feats).flatten(1)),
                dim=1,
            )
            z = self.fusion(torch.cat((self.global_fc(pooled), local_feat), dim=1))
            logits = self.policy_head(z) + local_logits + target_logits

            # Masquage des actions invalides : fonction deterministe de
            # l'observation (obstacles + position + couche), identique en
            # train et en inference — le rollout glouton ne peut plus
            # percuter un mur ni un via vers une cellule bloque.
            logits = logits + self._action_mask(x, layer_count, py, px, width, height)
            return logits, self.value_head(z).squeeze(-1)

        def _action_mask(self, x, layer_count, py, px, width, height):
            """Ajoute -inf (masque) ou 0 (autorisise) par action."""
            batch = x.shape[0]
            rows = torch.arange(batch, device=x.device)
            blocked = x[:, :layer_count]  # (B, L, H, W)
            layer = (
                x[:, layer_count + 2, 0, 0] * max(1, layer_count - 1)
            ).round().long().clamp(0, layer_count - 1)
            mask = torch.zeros(batch, self.n_actions, device=x.device)
            moves = ((0, -1), (1, 0), (0, 1), (-1, 0))
            for move_idx, (dx, dy) in enumerate(moves):
                nx = (px + dx).clamp(0, width - 1)
                ny = (py + dy).clamp(0, height - 1)
                in_bounds = (
                    (px + dx >= 0) & (px + dx < width)
                    & (py + dy >= 0) & (py + dy < height)
                )
                free = blocked[rows, layer, ny, nx] < 0.5
                mask[:, move_idx * 3] = torch.where(
                    in_bounds & free, 0.0, -1.0e9
                )
            up_free = (
                (layer + 1 < layer_count)
                & (blocked[rows, (layer + 1).clamp(0, layer_count - 1), py, px] < 0.5)
            )
            down_free = (
                (layer - 1 >= 0)
                & (blocked[rows, (layer - 1).clamp(0, layer_count - 1), py, px] < 0.5)
            )
            for move_idx in range(4):  # les vias ignorent le composant move
                mask[:, move_idx * 3 + 1] = torch.where(up_free, 0.0, -1.0e9)
                mask[:, move_idx * 3 + 2] = torch.where(down_free, 0.0, -1.0e9)
            return mask

    _ACTOR_CRITIC_CLS[arch] = _ActorCritic
    return _ACTOR_CRITIC_CLS[arch]


def __getattr__(name: str):
    """PEP 562: expose ``ActorCritic`` without importing torch at import time."""
    if name == "ActorCritic":
        return _actor_critic_class("base")
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


@dataclass
class PPOConfig:
    """PPO hyper-parameters (mirrored by ``training/router/config.yaml``)."""

    lr: float = 3e-4
    gamma: float = 0.99
    gae_lambda: float = 0.95
    clip_eps: float = 0.2
    epochs: int = 4
    batch_size: int = 64
    ent_coef: float = 0.01
    vf_coef: float = 0.5
    max_grad_norm: float = 0.5
    rollout_len: int = 256
    seed: int = 0


class PPOAgent(BaseAgent):
    """Clipped PPO agent with an actor-critic CNN policy."""

    def __init__(
        self,
        config: PPOConfig | None = None,
        in_channels: int = 5,
        n_actions: int = 12,
        device: str | None = None,
        arch: str = "base",
    ) -> None:
        """Build the agent (first torch import happens here).

        Args:
            config: PPO hyper-parameters.
            in_channels: observation channels (``layer_count + 4``).
            n_actions: size of the discrete action space.
            device: ``"cpu"``/``"cuda"``; auto-detected when None.
            arch: network architecture (``base`` | ``compass`` | ``wide``).
                ``load()`` re-construit automatiquement le reseau selon
                l'archi declaree dans le checkpoint quand ``follow_arch``
                est actif (service/eval n'ont donc RIEN a changer pour
                charger une archi nouvelle generation).
        """
        torch = _torch()
        self.cfg = config if config is not None else PPOConfig()
        self.in_channels = int(in_channels)
        self.n_actions = int(n_actions)
        self.arch = str(arch or "base")
        self.rng = random.Random(self.cfg.seed)
        if device is None:
            device = "cuda" if torch.cuda.is_available() else "cpu"
        self.device = torch.device(device)
        torch.manual_seed(self.cfg.seed)
        self.net = _actor_critic_class(self.arch)(
            self.in_channels, self.n_actions
        ).to(self.device)
        self.optimizer = torch.optim.Adam(self.net.parameters(), lr=self.cfg.lr)

    # ---------------------------------------------------------------- policy

    @property
    def name(self) -> str:
        return "ppo"

    def _obs_to_tensor(self, obs: np.ndarray):
        """Convert a numpy observation (C,H,W) to a batched torch tensor."""
        torch = _torch()
        arr = np.asarray(obs, dtype=np.float32)
        if arr.ndim == 2:  # tolerate (H, W): add a channel axis
            arr = arr[None, :, :]
        return torch.as_tensor(arr, device=self.device).unsqueeze(0)

    def select_action(self, obs: np.ndarray, greedy: bool = False) -> int:
        """Pick an action index from the policy (greedy = argmax)."""
        torch = _torch()
        with torch.no_grad():
            logits, _value = self.net(self._obs_to_tensor(obs))
            if greedy:
                return int(logits.argmax(dim=-1).item())
            dist = torch.distributions.Categorical(logits=logits)
            return int(dist.sample().item())

    def act_with_info(self, obs: np.ndarray) -> tuple[int, float, float]:
        """Sample an action and return ``(action, logprob, value)``."""
        torch = _torch()
        with torch.no_grad():
            logits, value = self.net(self._obs_to_tensor(obs))
            dist = torch.distributions.Categorical(logits=logits)
            action = dist.sample()
            return (
                int(action.item()),
                float(dist.log_prob(action).item()),
                float(value.item()),
            )

    def value_of(self, obs: np.ndarray) -> float:
        """Value estimate of a single observation (no gradient)."""
        torch = _torch()
        with torch.no_grad():
            _logits, value = self.net(self._obs_to_tensor(obs))
            return float(value.item())

    def evaluate_actions(self, obs, actions):
        """Re-evaluate stored actions: returns (logprobs, entropy, values)."""
        torch = _torch()
        logits, values = self.net(obs)
        dist = torch.distributions.Categorical(logits=logits)
        logprobs = dist.log_prob(actions)
        entropy = dist.entropy()
        return logprobs, entropy, values

    # --------------------------------------------------------------- training

    def update(self, batch: dict[str, Any]) -> dict[str, float]:
        """One PPO update over ``batch`` (clipped objective + GAE targets).

        Args:
            batch: dict with numpy arrays ``obs`` (N,C,H,W), ``actions``,
                ``logprobs``, ``returns`` and ``advantages``.

        Returns:
            Mean loss statistics: ``policy_loss``, ``value_loss``,
            ``entropy`` and ``approx_kl``.
        """
        torch = _torch()
        n = len(batch["actions"])
        obs = torch.as_tensor(
            np.asarray(batch["obs"], dtype=np.float32), device=self.device
        )
        actions = torch.as_tensor(
            np.asarray(batch["actions"], dtype=np.int64), device=self.device
        )
        old_logprobs = torch.as_tensor(
            np.asarray(batch["logprobs"], dtype=np.float32), device=self.device
        )
        returns = torch.as_tensor(
            np.asarray(batch["returns"], dtype=np.float32), device=self.device
        )
        advantages = torch.as_tensor(
            np.asarray(batch["advantages"], dtype=np.float32), device=self.device
        )
        if n > 1:
            advantages = (advantages - advantages.mean()) / (advantages.std() + 1e-8)

        generator = torch.Generator(device="cpu").manual_seed(self.cfg.seed)
        stats = {"policy_loss": 0.0, "value_loss": 0.0, "entropy": 0.0, "approx_kl": 0.0}
        updates = 0
        for _epoch in range(self.cfg.epochs):
            perm = torch.randperm(n, generator=generator).to(self.device)
            for start in range(0, n, self.cfg.batch_size):
                mb = perm[start : start + self.cfg.batch_size]
                logprobs, entropy, values = self.evaluate_actions(obs[mb], actions[mb])
                ratio = torch.exp(logprobs - old_logprobs[mb])
                surr1 = ratio * advantages[mb]
                surr2 = torch.clamp(
                    ratio, 1.0 - self.cfg.clip_eps, 1.0 + self.cfg.clip_eps
                ) * advantages[mb]
                policy_loss = -torch.min(surr1, surr2).mean()
                value_loss = 0.5 * (returns[mb] - values).pow(2).mean()
                loss = (
                    policy_loss
                    + self.cfg.vf_coef * value_loss
                    - self.cfg.ent_coef * entropy.mean()
                )
                self.optimizer.zero_grad()
                loss.backward()
                torch.nn.utils.clip_grad_norm_(
                    self.net.parameters(), self.cfg.max_grad_norm
                )
                self.optimizer.step()
                with torch.no_grad():
                    logratio = logprobs - old_logprobs[mb]
                    approx_kl = ((ratio - 1.0) - logratio).mean()
                stats["policy_loss"] += float(policy_loss.item())
                stats["value_loss"] += float(value_loss.item())
                stats["entropy"] += float(entropy.mean().item())
                stats["approx_kl"] += float(approx_kl.item())
                updates += 1
        if updates:
            for key in stats:
                stats[key] /= updates
        return stats

    # ------------------------------------------------------------ persistence

    def save(self, path: str) -> bool:
        """Save weights + metadata to a ``.pt`` file; return True on success."""
        torch = _torch()
        try:
            directory = os.path.dirname(os.path.abspath(path))
            os.makedirs(directory, exist_ok=True)
            torch.save(
                {
                    "model_state_dict": self.net.state_dict(),
                    "in_channels": self.in_channels,
                    "n_actions": self.n_actions,
                    "arch": self.arch,
                    "config": {
                        "lr": self.cfg.lr,
                        "gamma": self.cfg.gamma,
                        "gae_lambda": self.cfg.gae_lambda,
                        "clip_eps": self.cfg.clip_eps,
                    },
                },
                path,
            )
            return True
        except Exception:
            return False

    def load(self, path: str, follow_arch: bool = True) -> bool:
        """Load weights from ``path``; return True on success.

        Args:
            path: checkpoint file.
            follow_arch: quand True (service, eval, bench), le reseau est
                reconstruit selon l'archi declaree dans le checkpoint
                (``payload["arch"]``, defaut ``base`` pour les fichiers
                d'avant la modularisation) puis charge strict — les
                checkpoints nouvelle archi se chargent donc sans rien
                changer chez l'appelant. Quand False (train/BC avec une
                archi FORCEE differente du checkpoint ``--init-from``),
                chargement PARTIEL : les tenseurs de forme compatible sont
                transferes, les autres restent a leur initialisation.
        """
        torch = _torch()
        if not os.path.isfile(path):
            return False
        try:
            payload = torch.load(path, map_location=self.device)
            state = payload.get("model_state_dict", payload)
            ckpt_arch = "base"
            if isinstance(payload, dict):
                ckpt_arch = str(payload.get("arch", "base"))
            if follow_arch and ckpt_arch != self.arch:
                self.arch = ckpt_arch
                self.net = _actor_critic_class(self.arch)(
                    self.in_channels, self.n_actions
                ).to(self.device)
                self.optimizer = torch.optim.Adam(
                    self.net.parameters(), lr=self.cfg.lr
                )
            if ckpt_arch != self.arch:
                # Archi forcee != archi du checkpoint : transfert partiel.
                own = self.net.state_dict()
                shared = {
                    key: value
                    for key, value in state.items()
                    if key in own and own[key].shape == value.shape
                }
                skipped = len(own) - len(shared)
                own.update(shared)
                self.net.load_state_dict(own)
                print(
                    f"[load] archi checkpoint={ckpt_arch} != agent={self.arch}: "
                    f"{len(shared)} tenseur(s) transferes, {skipped} reinitialise(s)",
                    flush=True,
                )
            else:
                self.net.load_state_dict(state)
            self.net.eval()
            return True
        except Exception:
            return False


class PPOTrainer:
    """Simple synchronous PPO training loop over a routing env factory."""

    def __init__(
        self,
        env_factory,
        agent: PPOAgent,
        total_steps: int = 100_000,
        rollout_len: int | None = None,
        log=print,
    ) -> None:
        """Args:
        env_factory: callable returning a fresh ``PCBRouteEnv``-like env.
        agent: the :class:`PPOAgent` to train.
        total_steps: total environment steps to collect before stopping.
        rollout_len: steps per rollout (defaults to ``agent.cfg.rollout_len``).
        log: callable used for periodic progress lines (default ``print``).
        """
        self.env_factory = env_factory
        self.agent = agent
        self.total_steps = int(total_steps)
        self.rollout_len = int(rollout_len or agent.cfg.rollout_len)
        self.log = log if log is not None else print
        self.log_interval = 5
        self.rng = random.Random(agent.cfg.seed)
        self.history: dict[str, list] = {
            "steps": [],
            "episode_reward": [],
            "episode_length": [],
            "policy_loss": [],
            "value_loss": [],
            "entropy": [],
        }

    def collect_rollout(self, env) -> tuple[RolloutBuffer, float, int, float]:
        """Collect one rollout.

        Returns:
            ``(buffer, mean_episode_reward, episodes, last_value)`` where
            ``last_value`` bootstraps the value of the state following the
            rollout (0.0 when the rollout ended on a terminal transition).
        """
        buffer = RolloutBuffer()
        episode_reward = 0.0
        episodes = 0
        completed_rewards = []
        obs = env.reset(self.rng.randrange(env.n_nets))
        for _ in range(self.rollout_len):
            action, logprob, value = self.agent.act_with_info(obs)
            next_obs, reward, terminated, truncated, _info = env.step(action)
            buffer.add(obs, action, logprob, reward, float(terminated or truncated), value)
            episode_reward += reward
            obs = next_obs
            if terminated or truncated:
                completed_rewards.append(episode_reward)
                episode_reward = 0.0
                episodes += 1
                obs = env.reset(self.rng.randrange(env.n_nets))
        last_value = 0.0 if bool(buffer.dones and buffer.dones[-1]) else self.agent.value_of(obs)
        mean_reward = float(np.mean(completed_rewards)) if completed_rewards else 0.0
        return buffer, mean_reward, episodes, last_value

    def train(self) -> dict[str, list]:
        """Run the full loop; returns the populated ``history`` dict."""
        steps_done = 0
        rollout_id = 0
        env = self.env_factory()
        while steps_done < self.total_steps:
            buffer, mean_reward, episodes, last_value = self.collect_rollout(env)
            steps_done += len(buffer)
            returns, advantages = buffer.compute_returns_and_advantages(
                self.agent.cfg.gamma, self.agent.cfg.gae_lambda, last_value=last_value
            )
            stats = self.agent.update(
                {
                    "obs": np.asarray(buffer.obs, dtype=np.float32),
                    "actions": np.asarray(buffer.actions, dtype=np.int64),
                    "logprobs": np.asarray(buffer.logprobs, dtype=np.float32),
                    "returns": returns,
                    "advantages": advantages,
                }
            )
            rollout_id += 1
            self.history["steps"].append(steps_done)
            self.history["episode_reward"].append(mean_reward)
            self.history["episode_length"].append(episodes)
            self.history["policy_loss"].append(stats.get("policy_loss", 0.0))
            self.history["value_loss"].append(stats.get("value_loss", 0.0))
            self.history["entropy"].append(stats.get("entropy", 0.0))
            if rollout_id % self.log_interval == 0 or steps_done >= self.total_steps:
                self.log(
                    f"ppo: steps={steps_done}/{self.total_steps} "
                    f"reward={mean_reward:.2f} "
                    f"policy_loss={stats.get('policy_loss', 0.0):.4f} "
                    f"value_loss={stats.get('value_loss', 0.0):.4f} "
                    f"entropy={stats.get('entropy', 0.0):.4f}"
                )
            env = self.env_factory()
        return self.history
