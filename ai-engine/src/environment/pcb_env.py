"""Environnement PCB type Gymnasium — routage d'un net sur grille 2 couches.

Pipeline R&D YahriaCad (référence PyTorch, NON requis en production :
l'inférence de l'application utilise le moteur TypeScript déterministe).

Grille : hauteur x largeur x 2 couches (F.Cu = 0, B.Cu = 1).
Observation multi-canaux (float32) :
    canal 0 : pads du net à router (cible)
    canal 1 : pads des autres nets
    canal 2 : obstacles (pistes déjà posées, contour bloqué)
    canal 3 : cible (pad d'arrivée)
    canal 4 : congestion (distance normalisée aux obstacles, mémo A*-like)
Actions (discret, 6) : {haut, bas, gauche, droite, via, fin} — voir
``action_space.ActionSpace``.

Récompense : progression vers la cible + bonus de connexion, pénalités de via,
de collision et de temps (voir ``reward.py``).

Exemple :
    env = PcbEnv(grid_h=32, grid_w=32, seed=42)
    obs, info = env.reset()
    obs, reward, terminated, truncated, info = env.step(3)
"""

from __future__ import annotations

import random
from dataclasses import dataclass, field
from typing import Dict, List, Tuple

import numpy as np

try:  # Gymnasium optionnel : l'env reste utilisable sans
    import gymnasium as gym
    from gymnasium import spaces

    _HAS_GYM = True
except Exception:  # pragma: no cover
    gym = object  # type: ignore
    spaces = None  # type: ignore
    _HAS_GYM = False

try:  # import relatif si utilisé comme paquet (environment.pcb_env)
    from .action_space import NUM_ACTIONS, ActionSpace, action_delta
    from .reward import RewardConfig, compute_reward
except ImportError:  # exécution directe depuis le dossier (python pcb_env.py)
    from action_space import NUM_ACTIONS, ActionSpace, action_delta  # type: ignore
    from reward import RewardConfig, compute_reward  # type: ignore


@dataclass
class NetToRoute:
    """Net à router : paire de pads (start, target) sur la grille."""

    name: str = "N1"
    start: Tuple[int, int, int] = (0, 0, 0)   # (y, x, layer)
    target: Tuple[int, int, int] = (0, 0, 0)  # (y, x, layer)


@dataclass
class PcbEnvConfig:
    grid_h: int = 32
    grid_w: int = 32
    num_layers: int = 2
    via_penalty: float = 5.0
    collision_penalty: float = 25.0
    step_penalty: float = 0.05
    progress_scale: float = 1.0
    connect_bonus: float = 50.0
    max_steps_factor: int = 4  # max_steps = factor * (h + w)
    seed: int | None = None


@dataclass
class _State:
    pos: Tuple[int, int, int] = (0, 0, 0)
    steps: int = 0
    via_count: int = 0
    path: List[Tuple[int, int, int]] = field(default_factory=list)


class PcbEnv(gym.Env if _HAS_GYM else object):  # type: ignore[misc]
    """Environnement de routage mono-net sur grille PCB 2 couches.

    Un épisode = routage d'un net (pads start → target) sans collision avec
    les obstacles (pistes des autres nets, contour). ``terminated`` est vrai
    quand le net est connecté ou quand l'agent abandonne/échoue.
    """

    metadata = {"render_modes": ["ansi"]}

    def __init__(self, config: PcbEnvConfig | None = None, **overrides) -> None:
        self.cfg = config or PcbEnvConfig(**overrides)
        self.action_space_impl = ActionSpace()
        self.reward_cfg = RewardConfig(
            via_penalty=self.cfg.via_penalty,
            collision_penalty=self.cfg.collision_penalty,
            step_penalty=self.cfg.step_penalty,
            connect_bonus=self.cfg.connect_bonus,
        )
        self.rng = random.Random(self.cfg.seed)
        self.np_rng = np.random.default_rng(self.cfg.seed)
        self.max_steps = self.cfg.max_steps_factor * (self.cfg.grid_h + self.cfg.grid_w)

        self.grid_shape: Tuple[int, int, int] = (
            self.cfg.grid_h, self.cfg.grid_w, self.cfg.num_layers,
        )
        # Occupation des pistes (y, x, layer) → nom de net
        self.occupied: Dict[Tuple[int, int, int], str] = {}
        # Pads des composants (bloqués, y compris pour le net courant)
        self.pads: Dict[Tuple[int, int, int], str] = {}
        self.net: NetToRoute = NetToRoute()
        self._state = _State()

        if _HAS_GYM and spaces is not None:
            self.observation_space = spaces.Box(
                low=0.0, high=1.0,
                shape=(5, self.cfg.grid_h, self.cfg.grid_w),
                dtype=np.float32,
            )
            self.action_space = spaces.Discrete(NUM_ACTIONS)

    # ------------------------------------------------------------------ reset
    def reset(self, *, seed: int | None = None, options: dict | None = None):
        """Réinitialise l'environnement et tire un nouveau net à router."""
        if seed is not None:
            self.rng = random.Random(seed)
            self.np_rng = np.random.default_rng(seed)
        self.occupied.clear()
        self.pads.clear()
        self.net = self._random_net(options)
        for cell, owner in ((self.net.start, self.net.name), (self.net.target, self.net.name)):
            self.pads[cell] = owner
        # Quelques pistes parasites (congestion initiale réaliste)
        for _ in range(self.rng.randint(2, 8)):
            self._random_blocked_trace()
        self._state = _State(pos=self.net.start, path=[self.net.start])
        return self._observation(), {"net": self.net.name, "target": self.net.target}

    def _random_net(self, options: dict | None) -> NetToRoute:
        if options and "net" in options and isinstance(options["net"], NetToRoute):
            return options["net"]
        h, w = self.cfg.grid_h, self.cfg.grid_w

        def rand_cell() -> Tuple[int, int, int]:
            return (
                self.rng.randint(1, h - 2),
                self.rng.randint(1, w - 2),
                self.rng.randint(0, self.cfg.num_layers - 1),
            )

        start, target = rand_cell(), rand_cell()
        while target == start:
            target = rand_cell()
        return NetToRoute(name=f"N{self.rng.randint(1, 999)}", start=start, target=target)

    def _random_blocked_trace(self) -> None:
        """Pose une courte trace d'un autre net (obstacle)."""
        h, w = self.cfg.grid_h, self.cfg.grid_w
        y, x, layer = (
            self.rng.randint(1, h - 2), self.rng.randint(1, w - 2),
            self.rng.randint(0, self.cfg.num_layers - 1),
        )
        for _ in range(self.rng.randint(2, 6)):
            cell = (y, x, layer)
            if cell not in self.pads:
                self.occupied[cell] = f"blocker_{layer}"
            dy, dx, via = action_delta(self.rng.randrange(NUM_ACTIONS - 2))
            y = min(max(y + dy, 1), h - 2)
            x = min(max(x + dx, 1), w - 2)

    # ------------------------------------------------------------------ steps
    def step(self, action: int):
        """Applique une action ; retourne (obs, reward, terminated, truncated, info)."""
        action = int(action)
        if not self.action_space_impl.contains(action):
            raise ValueError(f"Action invalide : {action}")
        s = self._state
        s.steps += 1
        dy, dx, is_via = action_delta(action)

        terminated = False
        truncated = False
        prev_dist = self._manhattan(s.pos, self.net.target)

        if action == self.action_space_impl.FINISH:
            connected = s.pos == self.net.target
            reward, terminated = compute_reward(
                self.reward_cfg, progress_delta=0.0,
                connected=connected, gave_up=not connected, steps=s.steps,
            )
            info = self._info(connected=connected)
            return self._observation(), reward, terminated, False, info

        ny, nx, nlayer = s.pos[0] + dy, s.pos[1] + dx, s.pos[2]
        if is_via:
            nlayer = (s.pos[2] + 1) % self.cfg.num_layers
            s.via_count += 1

        in_bounds = 0 <= ny < self.cfg.grid_h and 0 <= nx < self.cfg.grid_w
        target_cell = (ny, nx, nlayer)
        collision = (
            not in_bounds
            or (target_cell in self.occupied)
            or (target_cell in self.pads and target_cell != self.net.target)
        )

        if collision:
            reward, terminated = compute_reward(
                self.reward_cfg, progress_delta=0.0,
                collision=True, steps=s.steps,
            )
            # L'agent reste sur place (pas de déplacement invalide)
            return self._observation(), reward, terminated, False, self._info()

        s.pos = target_cell
        s.path.append(target_cell)
        self.occupied[target_cell] = self.net.name
        new_dist = self._manhattan(s.pos, self.net.target)
        progress_delta = float(prev_dist - new_dist)
        connected = s.pos == self.net.target

        reward, terminated = compute_reward(
            self.reward_cfg,
            progress_delta=progress_delta * self.cfg.progress_scale,
            via=is_via,
            connected=connected,
            steps=s.steps,
        )
        truncated = s.steps >= self.max_steps
        if truncated:
            terminated = False
        return self._observation(), reward, terminated, truncated, self._info(connected=connected)

    # ------------------------------------------------------------ observation
    def _observation(self) -> np.ndarray:
        """Observation multi-canaux 5 x H x W normalisée dans [0, 1]."""
        h, w, _ = self.grid_shape
        obs = np.zeros((5, h, w), dtype=np.float32)
        y0, x0, l0 = self.net.start
        y1, x1, l1 = self.net.target

        # Canal 0 : pad de départ du net ; canal 3 : cible (sur les 2 couches)
        obs[0, y0, x0] = 1.0
        obs[3, y1, x1] = 1.0
        # Canal 1 : pads des autres nets
        for (y, x, _l), owner in self.pads.items():
            if owner != self.net.name:
                obs[1, y, x] = 1.0
        # Canal 2 : obstacles (pistes autres nets, toutes couches confondues)
        for (y, x, _layer), owner in self.occupied.items():
            if owner != self.net.name:
                obs[2, y, x] = 1.0
        # Canal 4 : congestion = distance de Chebyshev normalisée à l'obstacle le plus proche
        obstacle_cells = [c for c, o in self.occupied.items() if o != self.net.name]
        if obstacle_cells:
            for yy in range(h):
                for xx in range(w):
                    d = min(max(abs(yy - cy), abs(xx - cx)) for cy, cx, _ in obstacle_cells)
                    obs[4, yy, xx] = min(d / 8.0, 1.0)
        return obs

    # ------------------------------------------------------------------ utils
    @staticmethod
    def _manhattan(a: Tuple[int, int, int], b: Tuple[int, int, int]) -> int:
        """Distance de Manhattan tenant compte du coût de changement de couche."""
        layer_cost = 1 if a[2] != b[2] else 0
        return abs(a[0] - b[0]) + abs(a[1] - b[1]) + layer_cost

    def _info(self, connected: bool = False) -> dict:
        return {
            "connected": connected,
            "steps": self._state.steps,
            "vias": self._state.via_count,
            "position": self._state.pos,
            "target": self.net.target,
        }

    def render(self, mode: str = "ansi") -> str:
        """Rendu texte ASCII de la couche F.Cu (débug)."""
        h, w, _ = self.grid_shape
        rows = []
        for y in range(h):
            row = []
            for x in range(w):
                cell = (y, x, 0)
                if cell == self.net.start:
                    row.append("S")
                elif cell == self.net.target:
                    row.append("T")
                elif cell == self._state.pos:
                    row.append("*")
                elif cell in self.occupied:
                    row.append("#")
                elif cell in self.pads:
                    row.append("o")
                else:
                    row.append(".")
            rows.append("".join(row))
        return "\n".join(rows)
