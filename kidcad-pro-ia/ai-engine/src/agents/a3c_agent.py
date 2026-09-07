"""Asynchronous Advantage Actor-Critic (A3C) agent, workers and trainer.

Pure-torch implementation using ``threading`` ( Hogwild!-style shared global
network + lock-protected gradient application). torch is imported lazily:
importing this module without torch does NOT crash - the classes raise a
clear RuntimeError only when actually instantiated.
"""

from __future__ import annotations

import random
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

import numpy as np

from .base_agent import BaseAgent

_GLOBAL_NET_LOCK = threading.Lock()


def _torch():
    """Import and return the torch module lazily."""
    import torch

    return torch


def _require_torch() -> None:
    """Raise a clear error when torch is missing."""
    try:
        _torch()
    except ImportError as exc:  # pragma: no cover - depends on env
        raise RuntimeError(
            "A3C requires PyTorch; install torch or use the A* fallback router"
        ) from exc


_NETWORK_CLS = None


def _network_class():
    """Build (once) and return the ``A3CNetwork`` nn.Module class."""
    global _NETWORK_CLS
    if _NETWORK_CLS is None:
        torch = _torch()
        nn = torch.nn

        class _A3CNetwork(nn.Module):
            """Shared CNN encoder with policy + value heads (same encoder as
            the PPO ActorCritic so checkpoints stay interchangeable).

            Args:
                obs_shape: observation shape ``(channels, H, W)`` or plainly
                    the number of channels (int).
                n_actions: size of the discrete action space.
                hidden: width of the shared fully-connected layer.
            """

            def __init__(self, obs_shape, n_actions: int, hidden: int = 256) -> None:
                super().__init__()
                in_channels = obs_shape[0] if isinstance(obs_shape, (tuple, list)) else int(obs_shape)
                self.encoder = nn.Sequential(
                    nn.Conv2d(in_channels, 32, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.Conv2d(32, 64, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.Conv2d(64, 64, kernel_size=3, stride=2, padding=1),
                    nn.ReLU(),
                    nn.AdaptiveAvgPool2d((6, 6)),
                )
                self.flat_dim = 64 * 6 * 6
                self.fc = nn.Sequential(nn.Linear(self.flat_dim, hidden), nn.ReLU())
                self.policy_head = nn.Linear(hidden, n_actions)
                self.value_head = nn.Linear(hidden, 1)

            def forward(self, x):  # type: (torch.Tensor) -> Tuple[torch.Tensor, torch.Tensor]
                h = self.encoder(x).flatten(1)
                h = self.fc(h)
                return self.policy_head(h), self.value_head(h).squeeze(-1)

        _NETWORK_CLS = _A3CNetwork
    return _NETWORK_CLS


def __getattr__(name: str):
    """PEP 562: expose ``A3CNetwork`` lazily (torch required on first use)."""
    if name == "A3CNetwork":
        return _network_class()
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


@dataclass
class A3CConfig:
    """A3C hyper-parameters."""

    lr: float = 1e-4
    gamma: float = 0.99
    n_steps: int = 20
    n_workers: int = 4
    ent_coef: float = 0.01
    vf_coef: float = 0.5
    max_grad_norm: float = 0.5
    total_steps: int = 100_000
    seed: int = 0


class A3CAgent(BaseAgent):
    """A3C holder for the GLOBAL network + shared Adam optimizer.

    The object itself is thread-safe for gradient application
    (:meth:`apply_gradients`) and weight synchronisation
    (:meth:`get_weights` / :meth:`set_weights`), both guarded by an internal
    ``threading.Lock``.
    """

    def __init__(
        self,
        config: A3CConfig | None = None,
        in_channels=5,
        n_actions: int = 12,
        device: str | None = None,
    ) -> None:
        """Build the global network (first torch import happens here).

        Args:
            config: A3C hyper-parameters.
            in_channels: observation channel count, or the full observation
                shape ``(channels, H, W)`` (only the channels are used).
            n_actions: size of the discrete action space.
            device: ``"cpu"``/``"cuda"``; auto-detected when None.
        """
        _require_torch()
        torch = _torch()
        self.cfg = config if config is not None else A3CConfig()
        self.obs_shape = tuple(in_channels) if isinstance(in_channels, (tuple, list)) else None
        self.in_channels = int(in_channels[0]) if isinstance(in_channels, (tuple, list)) else int(in_channels)
        self.n_actions = int(n_actions)
        if device is None:
            device = "cuda" if torch.cuda.is_available() else "cpu"
        self.device = torch.device(device)
        self.lock = threading.Lock()
        self.net = _network_class()(self.in_channels, self.n_actions).to(self.device)
        self.optimizer = torch.optim.Adam(self.net.parameters(), lr=self.cfg.lr)
        self.steps = 0  # global step counter shared by the workers

    @property
    def name(self) -> str:
        return "a3c"

    # ------------------------------------------------------------- inference

    def _obs_to_tensor(self, obs: np.ndarray):
        torch = _torch()
        arr = np.asarray(obs, dtype=np.float32)
        if arr.ndim == 2:
            arr = arr[None, :, :]
        return torch.as_tensor(arr, device=self.device).unsqueeze(0)

    def select_action(self, obs: np.ndarray, greedy: bool = False) -> int:
        """Sample (or argmax) an action from the global policy."""
        torch = _torch()
        with torch.no_grad():
            logits, _value = self.net(self._obs_to_tensor(obs))
            if greedy:
                return int(logits.argmax(dim=-1).item())
            dist = torch.distributions.Categorical(logits=logits)
            return int(dist.sample().item())

    def value_of(self, obs: np.ndarray) -> float:
        torch = _torch()
        with torch.no_grad():
            _logits, value = self.net(self._obs_to_tensor(obs))
            return float(value.item())

    # ------------------------------------------------------------ training

    def get_weights(self) -> dict[str, Any]:
        """Thread-safe copy of the global network weights."""
        with self.lock:
            return {k: v.detach().clone() for k, v in self.net.state_dict().items()}

    def set_weights(self, state: dict[str, Any]) -> None:
        """Thread-safe load of weights into the global network."""
        with self.lock:
            self.net.load_state_dict(state)

    def apply_gradients(self, gradients) -> None:
        """Apply worker gradients to the global network under the lock."""
        torch = _torch()
        with self.lock:
            for param, grad in zip(self.net.parameters(), gradients, strict=False):
                param.grad = grad.detach().to(self.device)
            torch.nn.utils.clip_grad_norm_(self.net.parameters(), self.cfg.max_grad_norm)
            self.optimizer.step()
            self.optimizer.zero_grad(set_to_none=True)

    def update(self, batch: dict[str, Any]) -> dict[str, float]:
        """Single-process n-step A2C update (kept for API parity/tests).

        ``batch`` contains ``obs`` (N,C,H,W), ``actions``, ``returns``.
        """
        torch = _torch()
        obs = torch.as_tensor(
            np.asarray(batch["obs"], dtype=np.float32), device=self.device
        )
        actions = torch.as_tensor(
            np.asarray(batch["actions"], dtype=np.int64), device=self.device
        )
        returns = torch.as_tensor(
            np.asarray(batch["returns"], dtype=np.float32), device=self.device
        )
        logits, values = self.net(obs)
        dist = torch.distributions.Categorical(logits=logits)
        policy_loss = -(dist.log_prob(actions) * (returns - values.detach())).mean()
        value_loss = 0.5 * (returns - values).pow(2).mean()
        entropy = dist.entropy().mean()
        loss = policy_loss + self.cfg.vf_coef * value_loss - self.cfg.ent_coef * entropy
        self.optimizer.zero_grad()
        loss.backward()
        torch.nn.utils.clip_grad_norm_(self.net.parameters(), self.cfg.max_grad_norm)
        with self.lock:
            self.optimizer.step()
        self.steps += len(actions)
        return {
            "policy_loss": float(policy_loss.item()),
            "value_loss": float(value_loss.item()),
            "entropy": float(entropy.item()),
        }

    # -------------------------------------------------------- persistence

    def save(self, path: str) -> bool:
        torch = _torch()
        try:
            with self.lock:
                torch.save(
                    {
                        "model_state_dict": self.net.state_dict(),
                        "in_channels": self.in_channels,
                        "n_actions": self.n_actions,
                    },
                    path,
                )
            return True
        except Exception:
            return False

    def load(self, path: str) -> bool:
        torch = _torch()
        try:
            payload = torch.load(path, map_location=self.device)
            state = payload.get("model_state_dict", payload)
            self.net.load_state_dict(state)
            return True
        except Exception:
            return False


class A3CWorker(threading.Thread):
    """Daemon worker thread: local network copy + n-step asynchronous updates."""

    def __init__(
        self,
        agent: A3CAgent,
        env_factory: Callable[[], Any],
        config: A3CConfig | None = None,
        worker_id: int = 0,
        stop_event: threading.Event | None = None,
    ) -> None:
        _require_torch()
        super().__init__(daemon=True, name=f"a3c-worker-{worker_id}")
        self.agent = agent
        self.env_factory = env_factory
        self.cfg = config if config is not None else A3CConfig()
        self.worker_id = int(worker_id)
        self.stop_event = stop_event or threading.Event()
        self.rng = random.Random(self.cfg.seed + 1000 * self.worker_id)
        self.local_net = _network_class()(agent.in_channels, agent.n_actions).to(agent.device)
        self.local_net.load_state_dict(agent.get_weights())
        self.env = env_factory()

    def run(self) -> None:  # pragma: no cover - requires torch + threads
        torch = _torch()
        obs = self.env.reset(self.rng.randrange(self.env.n_nets))
        while not self.stop_event.is_set() and self.agent.steps < self.cfg.total_steps:
            self.local_net.load_state_dict(self.agent.get_weights())

            logps, values, entropies, rewards, dones = [], [], [], [], []
            for _ in range(self.cfg.n_steps):
                tensor = torch.as_tensor(
                    np.asarray(obs, dtype=np.float32), device=self.agent.device
                ).unsqueeze(0)
                logits, value = self.local_net(tensor)
                dist = torch.distributions.Categorical(logits=logits)
                action = dist.sample()
                logps.append(dist.log_prob(action))
                entropies.append(dist.entropy())
                values.append(value)
                obs_next, reward, terminated, truncated, _info = self.env.step(int(action.item()))
                rewards.append(float(reward))
                dones.append(bool(terminated or truncated))
                obs = obs_next
                if terminated or truncated:
                    obs = self.env.reset(self.rng.randrange(self.env.n_nets))
                    break

            # n-step bootstrapped return
            with torch.no_grad():
                bootstrap = torch.zeros((), device=self.agent.device)
                if not dones[-1]:
                    obs_t = torch.as_tensor(
                        np.asarray(obs, dtype=np.float32), device=self.agent.device
                    ).unsqueeze(0)
                    _logits, bootstrap = self.local_net(obs_t)
            running = bootstrap
            policy_loss = torch.zeros((), device=self.agent.device)
            value_loss = torch.zeros((), device=self.agent.device)
            for t in reversed(range(len(rewards))):
                running = rewards[t] + self.cfg.gamma * running * (0.0 if dones[t] else 1.0)
                advantage = running - values[t]
                policy_loss = policy_loss - logps[t] * advantage.detach()
                value_loss = value_loss + 0.5 * (running.detach() - values[t]).pow(2)
            entropy = torch.stack(entropies).mean()
            loss = (
                policy_loss / max(1, len(rewards))
                + self.cfg.vf_coef * value_loss / max(1, len(rewards))
                - self.cfg.ent_coef * entropy
            )
            self.local_net.zero_grad()
            loss.backward()
            gradients = [p.grad for p in self.local_net.parameters()]
            self.agent.apply_gradients(gradients)
            with _GLOBAL_NET_LOCK:
                self.agent.steps += len(rewards)

    def request_stop(self) -> None:
        """Ask the worker loop to exit at the next iteration."""
        self.stop_event.set()


class A3CTrainer:
    """Spawn daemon A3C workers and join them once ``total_steps`` is reached.

    Args:
        env_factory: callable returning a fresh env per worker.
        obs_shape: observation shape ``(channels, H, W)`` (or channel count).
        n_actions: size of the discrete action space.
        config: optional :class:`A3CConfig` (a copy is kept).
        total_steps: global step target (overrides ``config.total_steps``).
        agent: optional pre-built :class:`A3CAgent` (created when omitted).
    """

    def __init__(
        self,
        env_factory: Callable[[], Any],
        obs_shape=None,
        n_actions: int = 12,
        config: A3CConfig | None = None,
        total_steps: int | None = None,
        agent: A3CAgent | None = None,
    ) -> None:
        _require_torch()
        self.env_factory = env_factory
        cfg = A3CConfig() if config is None else A3CConfig(**{
            key: getattr(config, key) for key in (
                "lr", "gamma", "n_steps", "n_workers", "ent_coef",
                "vf_coef", "max_grad_norm", "total_steps", "seed",
            )
        })
        if total_steps is not None:
            cfg.total_steps = int(total_steps)
        self.cfg = cfg
        if agent is None:
            shape = obs_shape if obs_shape is not None else 5
            agent = A3CAgent(cfg, in_channels=shape, n_actions=int(n_actions))
        self.agent = agent
        self.history: dict[str, list] = {"steps": [], "elapsed_s": []}

    def train(self) -> dict[str, list]:  # pragma: no cover - requires torch
        """Run asynchronous training; returns the history dict."""
        stop_event = threading.Event()
        workers = [
            A3CWorker(
                self.agent,
                self.env_factory,
                self.cfg,
                worker_id=i,
                stop_event=stop_event,
            )
            for i in range(max(1, self.cfg.n_workers))
        ]
        start = time.monotonic()
        for worker in workers:
            worker.start()
        while any(worker.is_alive() for worker in workers):
            time.sleep(0.1)
            self.history["steps"].append(self.agent.steps)
            self.history["elapsed_s"].append(round(time.monotonic() - start, 3))
            if self.agent.steps >= self.cfg.total_steps:
                stop_event.set()
        for worker in workers:
            worker.join(timeout=2.0)
        return self.history
