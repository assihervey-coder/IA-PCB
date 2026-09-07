"""Agent PPO (Proximal Policy Optimization) — pipeline R&D YahriaCad.

Actor-critic MLP sur observations multi-canaux (5 x H x W) de l'environnement
PCB. Implémente : collecte rollouts, GAE(lambda), clipping d'objectif, epochs
de minimisation, entropie bonus, sauvegarde/chargement de checkpoints ``.pt``.

Référence : Schulman et al., "Proximal Policy Optimization Algorithms", 2017.
Non requis en production (inférence = moteur TS déterministe, cf. ADR-002).
"""

from __future__ import annotations

import math
import random
from dataclasses import dataclass
from typing import Any, Dict, Tuple

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F
from torch.distributions import Categorical

try:  # import relatif si utilisé comme paquet, direct sinon
    from .base_agent import BaseAgent
except ImportError:  # pragma: no cover
    from base_agent import BaseAgent  # type: ignore


# --------------------------------------------------------------------------- réseau
class ActorCriticMLP(nn.Module):
    """Tête actor-critic partagée (MLP conv-less sur l'observation aplatie)."""

    def __init__(self, obs_shape: Tuple[int, ...], n_actions: int, hidden: int = 256) -> None:
        super().__init__()
        in_dim = int(np.prod(obs_shape))
        self.trunk = nn.Sequential(
            nn.Flatten(),
            nn.Linear(in_dim, hidden),
            nn.ReLU(),
            nn.Linear(hidden, hidden),
            nn.ReLU(),
        )
        self.policy_head = nn.Linear(hidden, n_actions)
        self.value_head = nn.Linear(hidden, 1)
        # Init orthogonale (stabilité PPO)
        for module in self.trunk.modules():
            if isinstance(module, nn.Linear):
                nn.init.orthogonal_(module.weight, gain=math.sqrt(2))
                nn.init.zeros_(module.bias)
        nn.init.orthogonal_(self.policy_head.weight, gain=0.01)
        nn.init.zeros_(self.policy_head.bias)
        nn.init.orthogonal_(self.value_head.weight, gain=1.0)
        nn.init.zeros_(self.value_head.bias)

    def forward(self, obs: torch.Tensor) -> Tuple[torch.Tensor, torch.Tensor]:
        """Retourne (logits de politique, valeur d'état)."""
        feats = self.trunk(obs)
        return self.policy_head(feats), self.value_head(feats).squeeze(-1)


# --------------------------------------------------------------------------- config
@dataclass
class PpoConfig:
    """Hyperparamètres PPO (valeurs par défaut = training/router/config.yaml)."""

    lr: float = 3e-4
    gamma: float = 0.99
    gae_lambda: float = 0.95
    clip_epsilon: float = 0.2
    entropy_coef: float = 0.01
    value_coef: float = 0.5
    max_grad_norm: float = 0.5
    epochs: int = 4              # epochs d'optimisation par rollout
    batch_size: int = 2048
    device: str = "cpu"


# --------------------------------------------------------------------------- agent
class PpoAgent(BaseAgent):
    """Agent PPO complet : rollout buffer, GAE, update par clipping."""

    def __init__(
        self,
        observation_shape: Tuple[int, ...] = (5, 32, 32),
        n_actions: int = 6,
        config: PpoConfig | None = None,
    ) -> None:
        super().__init__(observation_shape, n_actions)
        self.cfg = config or PpoConfig()
        self.device = torch.device(self.cfg.device)
        self.net = ActorCriticMLP(observation_shape, n_actions).to(self.device)
        self.optimizer = torch.optim.Adam(self.net.parameters(), lr=self.cfg.lr)

    # ------------------------------------------------------------------ politique
    @torch.no_grad()
    def select_action(self, obs: np.ndarray, deterministic: bool = False) -> int:
        """Échantillonne (ou prend en argmax) une action selon la politique."""
        x = torch.as_tensor(self._as_batch(obs), device=self.device)
        logits, value = self.net(x)
        if deterministic:
            action = int(torch.argmax(logits, dim=-1).item())
        else:
            action = int(Categorical(logits=logits).sample().item())
        return action

    @torch.no_grad()
    def evaluate_obs(self, obs: np.ndarray) -> Tuple[np.ndarray, np.ndarray, np.ndarray]:
        """Logits, valeurs et log-probs des actions courantes (pour le rollout)."""
        x = torch.as_tensor(self._as_batch(obs), device=self.device)
        logits, values = self.net(x)
        dist = Categorical(logits=logits)
        return logits.cpu().numpy(), values.cpu().numpy(), dist.logits.log_softmax(-1).cpu().numpy()

    # ------------------------------------------------------------------ apprentissage
    def compute_gae(
        self,
        rewards: np.ndarray,
        values: np.ndarray,
        dones: np.ndarray,
        next_value: float,
    ) -> Tuple[np.ndarray, np.ndarray]:
        """Generalized Advantage Estimation → (advantages, returns)."""
        T = len(rewards)
        advantages = np.zeros(T, dtype=np.float32)
        gae = 0.0
        for t in reversed(range(T)):
            next_v = next_value if t == T - 1 else values[t + 1]
            non_terminal = 0.0 if dones[t] else 1.0
            delta = rewards[t] + self.cfg.gamma * next_v * non_terminal - values[t]
            gae = delta + self.cfg.gamma * self.cfg.gae_lambda * non_terminal * gae
            advantages[t] = gae
        returns = advantages + values
        return advantages, returns

    def learn(self, batch: Dict[str, Any]) -> Dict[str, float]:
        """Mise à jour PPO sur un batch déjà préparé (obs/actions/old_logp/adv/ret)."""
        obs = torch.as_tensor(batch["obs"], dtype=torch.float32, device=self.device)
        actions = torch.as_tensor(batch["actions"], dtype=torch.long, device=self.device)
        old_logp = torch.as_tensor(batch["old_logp"], dtype=torch.float32, device=self.device)
        advantages = torch.as_tensor(batch["advantages"], dtype=torch.float32, device=self.device)
        returns = torch.as_tensor(batch["returns"], dtype=torch.float32, device=self.device)

        # Normalisation des avantages (stabilité)
        advantages = (advantages - advantages.mean()) / (advantages.std() + 1e-8)

        n = obs.shape[0]
        idx = np.arange(n)
        metrics = {"policy_loss": 0.0, "value_loss": 0.0, "entropy": 0.0, "kl": 0.0}
        updates = 0

        for _ in range(self.cfg.epochs):
            np.random.shuffle(idx)
            for start in range(0, n, self.cfg.batch_size):
                mb = idx[start:start + self.cfg.batch_size]
                if len(mb) == 0:
                    continue
                mb_t = torch.as_tensor(mb, dtype=torch.long, device=self.device)
                logits, values = self.net(obs[mb_t])
                dist = Categorical(logits=logits)
                logp = dist.log_prob(actions[mb_t])

                ratio = torch.exp(logp - old_logp[mb_t])
                adv = advantages[mb_t]
                surr1 = ratio * adv
                surr2 = torch.clamp(ratio, 1.0 - self.cfg.clip_epsilon, 1.0 + self.cfg.clip_epsilon) * adv
                policy_loss = -torch.min(surr1, surr2).mean()

                value_loss = F.mse_loss(values, returns[mb_t])
                entropy = dist.entropy().mean()

                loss = policy_loss + self.cfg.value_coef * value_loss - self.cfg.entropy_coef * entropy

                self.optimizer.zero_grad(set_to_none=True)
                loss.backward()
                nn.utils.clip_grad_norm_(self.net.parameters(), self.cfg.max_grad_norm)
                self.optimizer.step()

                with torch.no_grad():
                    kl = (old_logp[mb_t] - logp).mean().abs()
                metrics["policy_loss"] += float(policy_loss.item())
                metrics["value_loss"] += float(value_loss.item())
                metrics["entropy"] += float(entropy.item())
                metrics["kl"] += float(kl.item())
                updates += 1

        if updates:
            for k in metrics:
                metrics[k] /= updates
        metrics["total_steps"] = float(self.total_steps)
        return metrics

    # ------------------------------------------------------------------ persistance
    def save(self, path: str) -> None:
        """Checkpoint : state_dict + hyperparamètres + shapes."""
        torch.save(
            {
                "model_state_dict": self.net.state_dict(),
                "obs_shape": self.observation_shape,
                "n_actions": self.n_actions,
                "config": self.cfg.__dict__,
                "total_steps": self.total_steps,
            },
            path,
        )

    def load(self, path: str) -> None:
        """Recharge un checkpoint écrit par :meth:`save`."""
        ckpt = torch.load(path, map_location=self.device, weights_only=False)
        self.net.load_state_dict(ckpt["model_state_dict"])
        if "total_steps" in ckpt:
            self.total_steps = int(ckpt["total_steps"])

    # ------------------------------------------------------------------ utilitaires
    def set_seed(self, seed: int) -> None:
        """Fixe les graines Python/torch (reproductibilité)."""
        random.seed(seed)
        np.random.seed(seed)
        torch.manual_seed(seed)

    def train_mode(self, flag: bool = True) -> None:
        self.net.train(flag)
