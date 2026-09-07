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
from typing import Any, Dict, Optional, Tuple

import numpy as np

from .base_agent import BaseAgent, RolloutBuffer


def _torch():
    """Import and return the torch module lazily."""
    import torch

    return torch


# Cache for the lazily-built ActorCritic class.
_ACTOR_CRITIC_CLS = None


def _actor_critic_class():
    """Build (once) and return the ``ActorCritic`` nn.Module class."""
    global _ACTOR_CRITIC_CLS
    if _ACTOR_CRITIC_CLS is None:
        torch = _torch()
        nn = torch.nn

        class _ActorCritic(nn.Module):
            """CNN actor-critic over multi-channel grid observations.

            Conv stack -> adaptive average pool (fixed-size flatten, so a
            single trained model works across board sizes) -> shared fc ->
            policy head (logits) + value head.

            Args:
                obs_shape: observation shape ``(channels, H, W)`` or plainly
                    the number of channels (int) - only the channel count is
                    consumed by the CNN encoder.
                n_actions: size of the discrete action space.
                hidden: width of the shared fully-connected layer.
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
                self.conv = nn.Sequential(
                    nn.Conv2d(in_channels, 32, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.Conv2d(32, 64, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.Conv2d(64, 64, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.AdaptiveAvgPool2d((pool, pool)),
                )
                self.flat_dim = 64 * pool * pool
                self.fc = nn.Sequential(nn.Linear(self.flat_dim, hidden), nn.ReLU())
                self.policy_head = nn.Linear(hidden, n_actions)
                self.value_head = nn.Linear(hidden, 1)

            def forward(self, x):  # type: (torch.Tensor) -> Tuple[torch.Tensor, torch.Tensor]
                h = self.conv(x).flatten(1)
                h = self.fc(h)
                return self.policy_head(h), self.value_head(h).squeeze(-1)

        _ACTOR_CRITIC_CLS = _ActorCritic
    return _ACTOR_CRITIC_CLS


def __getattr__(name: str):
    """PEP 562: expose ``ActorCritic`` without importing torch at import time."""
    if name == "ActorCritic":
        return _actor_critic_class()
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
        config: Optional[PPOConfig] = None,
        in_channels: int = 5,
        n_actions: int = 12,
        device: Optional[str] = None,
    ) -> None:
        """Build the agent (first torch import happens here).

        Args:
            config: PPO hyper-parameters.
            in_channels: observation channels (``layer_count + 3``).
            n_actions: size of the discrete action space.
            device: ``"cpu"``/``"cuda"``; auto-detected when None.
        """
        torch = _torch()
        self.cfg = config if config is not None else PPOConfig()
        self.in_channels = int(in_channels)
        self.n_actions = int(n_actions)
        self.rng = random.Random(self.cfg.seed)
        if device is None:
            device = "cuda" if torch.cuda.is_available() else "cpu"
        self.device = torch.device(device)
        torch.manual_seed(self.cfg.seed)
        self.net = _actor_critic_class()(
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

    def act_with_info(self, obs: np.ndarray) -> Tuple[int, float, float]:
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

    def update(self, batch: Dict[str, Any]) -> Dict[str, float]:
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

    def load(self, path: str) -> bool:
        """Load weights from ``path``; return True on success."""
        torch = _torch()
        if not os.path.isfile(path):
            return False
        try:
            payload = torch.load(path, map_location=self.device)
            state = payload.get("model_state_dict", payload)
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
        rollout_len: Optional[int] = None,
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
        self.history: Dict[str, list] = {
            "steps": [],
            "episode_reward": [],
            "episode_length": [],
            "policy_loss": [],
            "value_loss": [],
            "entropy": [],
        }

    def collect_rollout(self, env) -> Tuple[RolloutBuffer, float, int, float]:
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

    def train(self) -> Dict[str, list]:
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
                    f"value_loss={stats.get('value_loss', 0.0):.4f}"
                )
            env = self.env_factory()
        return self.history
