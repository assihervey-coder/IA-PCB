"""Abstract agent interface and on-policy rollout buffer.

All agents (PPO, A3C, scripted baselines) implement :class:`BaseAgent` so the
gRPC service and the benchmark can swap them transparently. Torch-dependent
subclasses import torch lazily inside their constructors/methods; this module
itself only needs numpy.
"""

from __future__ import annotations

import abc
from typing import Any

import numpy as np


class BaseAgent(abc.ABC):
    """Common interface implemented by every routing agent."""

    @property
    @abc.abstractmethod
    def name(self) -> str:
        """Human-readable agent name (e.g. ``"ppo"``)."""

    @abc.abstractmethod
    def select_action(self, obs: np.ndarray, greedy: bool = False) -> int:
        """Return the action index chosen for ``obs``.

        Args:
            obs: environment observation (``float32`` array ``C x H x W``).
            greedy: when True pick the argmax action instead of sampling.
        """

    @abc.abstractmethod
    def update(self, batch: dict[str, Any]) -> dict[str, float]:
        """Run one learning update on ``batch`` and return loss statistics."""

    @abc.abstractmethod
    def save(self, path: str) -> bool:
        """Persist the model weights to ``path``; return True on success."""

    @abc.abstractmethod
    def load(self, path: str) -> bool:
        """Restore the model weights from ``path``; return True on success."""


class RolloutBuffer:
    """Minimal on-policy buffer for one rollout (PPO / A2C style).

    Stores observations, actions, log-probabilities, rewards, dones and
    value estimates; ``compute_returns_and_advantages`` implements
    Generalized Advantage Estimation (GAE).
    """

    def __init__(self) -> None:
        self.obs: list[Any] = []
        self.actions: list[int] = []
        self.logprobs: list[float] = []
        self.rewards: list[float] = []
        self.dones: list[float] = []
        self.values: list[float] = []

    def add(
        self,
        obs: Any,
        action: int,
        logprob: float,
        reward: float,
        done: float,
        value: float,
    ) -> None:
        """Append one transition."""
        self.obs.append(obs)
        self.actions.append(int(action))
        self.logprobs.append(float(logprob))
        self.rewards.append(float(reward))
        self.dones.append(float(done))
        self.values.append(float(value))

    def last_value(self) -> float:
        """Value estimate of the most recent stored state (0.0 when empty)."""
        return self.values[-1] if self.values else 0.0

    def __len__(self) -> int:
        return len(self.rewards)

    def reset(self) -> None:
        """Drop every stored transition."""
        self.obs.clear()
        self.actions.clear()
        self.logprobs.clear()
        self.rewards.clear()
        self.dones.clear()
        self.values.clear()

    def compute_returns_and_advantages(
        self,
        gamma: float,
        gae_lambda: float,
        last_value: float = 0.0,
    ) -> tuple[np.ndarray, np.ndarray]:
        """Compute discounted returns and GAE advantages.

        Args:
            gamma: discount factor.
            gae_lambda: GAE exponential weighting factor.
            last_value: bootstrap value of the state following the rollout
                (0.0 when the rollout ended on a terminal transition).

        Returns:
            ``(returns, advantages)`` as ``float64`` numpy arrays.
        """
        n = len(self.rewards)
        returns = np.zeros(n, dtype=np.float64)
        advantages = np.zeros(n, dtype=np.float64)
        gae = 0.0
        next_value = float(last_value)
        next_non_done = 1.0
        for t in reversed(range(n)):
            delta = (
                self.rewards[t]
                + gamma * next_value * next_non_done
                - self.values[t]
            )
            gae = delta + gamma * gae_lambda * next_non_done * gae
            advantages[t] = gae
            returns[t] = gae + self.values[t]
            next_value = self.values[t]
            next_non_done = 1.0 - self.dones[t]
        return returns, advantages
