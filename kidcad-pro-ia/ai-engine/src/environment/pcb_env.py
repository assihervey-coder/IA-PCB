"""Grid-based PCB routing environment with a deterministic A* fallback.

The environment models the routing of ONE net at a time on a regular grid
extracted from the board description. It follows the gym-like API::

    env = PCBRouteEnv(board, nets, EnvConfig(...))
    obs = env.reset(net_index)
    obs, reward, terminated, truncated, info = env.step(action)

Grid and coordinates
--------------------
All input/output coordinates are millimetres with the origin at the
top-left corner of the board; layer 0 is F.Cu. Internally the board is
discretised on a regular grid of resolution ``res`` (the board's
``grid_resolution_mm`` when provided, otherwise ``EnvConfig.grid_mm``):
cell ``(gx, gy)`` is centred at ``(gx * res, gy * res)`` and internal
cells are ``(x, y, layer)`` tuples with numpy arrays indexed ``[l, y, x]``.

Blocking rules
--------------
* board border: a ring of ``clearance_cells`` cells is blocked;
* pads of OTHER nets: the pad cell plus a square halo of
  ``clearance_cells`` cells (on the pad's own layer) is blocked;
* pads of the CURRENT net: terminal cells (free, they are the goals);
* routes registered through :meth:`set_routes` (or by previous successful
  router calls): blocked for every other net.

Observation
-----------
``float32`` array of shape ``(layer_count + 3, H, W)``:

* channels ``0 .. layer_count-1``: obstacle plane per layer;
* channel ``layer_count``: source mask (episode start pad cell);
* channel ``layer_count + 1``: target mask (remaining target pad cells);
* channel ``layer_count + 2``: layer indicator (constant plane equal to
  ``current_layer / max(1, layer_count - 1)``).

Actions
-------
12 discrete actions from :class:`~src.environment.action_space.ActionSpace`
(4 moves x {stay, via-up, via-down}). An invalid move (blocked cell, off
grid, via onto a blocked layer cell) is a collision: the agent stays in
place and receives a negative reward.

Success
-------
The episode terminates successfully as soon as the agent reaches any
remaining target pad cell of the net. The deterministic fallback router
:meth:`astar_route` handles full multi-terminal nets by chaining A* legs
between pads (nearest-terminal-first), which is what the gRPC service uses
when no RL model is available.
"""

from __future__ import annotations

import heapq
import math
import random
from dataclasses import dataclass, field
from typing import Dict, Iterable, Iterator, List, Optional, Sequence, Set, Tuple

import numpy as np

from .action_space import ActionSpace
from .reward import RewardConfig, RewardShaper

Cell = Tuple[int, int, int]  # (x, y, layer) in grid cells

_MOVES_4: Tuple[Tuple[int, int], ...] = ((0, -1), (1, 0), (0, 1), (-1, 0))


@dataclass
class EnvConfig:
    """Environment / router configuration.

    Attributes:
        grid_mm: default grid resolution (mm) when the board does not
            provide ``grid_resolution_mm``.
        max_steps: episode step limit; ``None`` selects an automatic limit
            of ``max(64, 4 * (W + H) * layer_count)`` steps.
        via_penalty: A* cost (in cell units) and reward penalty of a via.
        step_penalty: reward penalty per step.
        progress_coef: shaping coefficient of the distance decrease.
        success_bonus: terminal reward for reaching a target pad.
        collision_penalty: reward penalty for invalid moves.
        clearance_cells: halo radius (in cells) blocked around foreign pads
            and around the board border.
        seed: optional seed for the environment RNG (reserved for
            stochastic extensions; routing itself is fully deterministic).
    """

    grid_mm: float = 0.25
    max_steps: Optional[int] = None
    via_penalty: float = 15.0
    step_penalty: float = 0.02
    progress_coef: float = 8.0
    success_bonus: float = 100.0
    collision_penalty: float = 2.0
    clearance_cells: int = 1
    seed: Optional[int] = None


@dataclass
class _Pad:
    """Internal pad representation (mm coordinates)."""

    component_ref: str
    pad_name: str
    x_mm: float
    y_mm: float
    layer: int
    width_mm: float
    height_mm: float


@dataclass
class _Net:
    """Internal net representation."""

    name: str
    net_class: str
    pads: List[_Pad]
    min_track_width_mm: float
    clearance_mm: float


@dataclass
class _Episode:
    """Mutable state of the current routing episode."""

    net_index: int = -1
    x: int = 0
    y: int = 0
    layer: int = 0
    steps: int = 0
    vias: int = 0
    start: Optional[Cell] = None
    targets: List[Cell] = field(default_factory=list)
    path: List[Cell] = field(default_factory=list)
    visited: Set[Cell] = field(default_factory=set)
    done: bool = False
    success: bool = False


class PCBRouteEnv:
    """Gym-like single-net PCB routing environment + deterministic A* router."""

    def __init__(
        self,
        board: dict,
        nets: List[dict],
        config: Optional[EnvConfig] = None,
    ) -> None:
        """Build the grid environment from plain-Python descriptions.

        Args:
            board: dict with ``width_mm``, ``height_mm``, ``layer_count``,
                ``grid_resolution_mm`` (optional) and ``layer_names``.
            nets: list of dicts with ``name``, ``net_class``,
                ``pads`` (list of dicts with ``component_ref``, ``pad_name``,
                ``position`` ``{x, y}``, ``layer``, ``width_mm``,
                ``height_mm``), ``min_track_width_mm`` and ``clearance_mm``.
            config: optional :class:`EnvConfig` override.
        """
        self.cfg = config if config is not None else EnvConfig()
        self.rng = random.Random(self.cfg.seed)

        self.width_mm = float(board.get("width_mm", 100.0) or 100.0)
        self.height_mm = float(board.get("height_mm", 100.0) or 100.0)
        self.layer_count = max(1, int(board.get("layer_count", 2) or 2))
        names = board.get("layer_names")
        self.layer_names: List[str] = list(names) if names else [
            f"L{i}" for i in range(self.layer_count)
        ]
        if len(self.layer_names) < self.layer_count:
            self.layer_names += [f"L{i}" for i in range(len(self.layer_names), self.layer_count)]

        res = float(board.get("grid_resolution_mm") or 0.0)
        if res <= 0.0:
            res = float(self.cfg.grid_mm)
        self.res: float = max(res, 1e-6)

        self.grid_w: int = max(3, int(round(self.width_mm / self.res)) + 1)
        self.grid_h: int = max(3, int(round(self.height_mm / self.res)) + 1)

        self.action_space = ActionSpace()
        self.reward_shaper = RewardShaper(
            RewardConfig(
                via_penalty=self.cfg.via_penalty,
                step_penalty=self.cfg.step_penalty,
                progress_coef=self.cfg.progress_coef,
                success_bonus=self.cfg.success_bonus,
                collision_penalty=self.cfg.collision_penalty,
            )
        )

        self._nets: List[_Net] = [self._parse_net(n) for n in (nets or [])]
        self._routes_cells: Dict[str, Set[Cell]] = {}  # per-net routed cells
        self._net_routes: Dict[str, dict] = {}         # per-net route dict output
        self._episode = _Episode()
        self.max_steps: int = self._resolve_max_steps()

        self._static: Optional[np.ndarray] = None  # lazily built blocking grid
        self._build_static_blocking()

    # ------------------------------------------------------------------ setup

    def _parse_net(self, spec: dict) -> _Net:
        """Convert a net description dict into the internal representation."""
        pads: List[_Pad] = []
        for pad in spec.get("pads") or []:
            pos = pad.get("position") or {}
            layer = int(pad.get("layer", 0) or 0)
            layer = min(max(layer, 0), self.layer_count - 1)
            pads.append(
                _Pad(
                    component_ref=str(pad.get("component_ref", "") or ""),
                    pad_name=str(pad.get("pad_name", "") or ""),
                    x_mm=float(pos.get("x", 0.0) or 0.0),
                    y_mm=float(pos.get("y", 0.0) or 0.0),
                    layer=layer,
                    width_mm=float(pad.get("width_mm", 0.3) or 0.3),
                    height_mm=float(pad.get("height_mm", 0.3) or 0.3),
                )
            )
        return _Net(
            name=str(spec.get("name", f"net{len(pads)}") or f"net{len(pads)}"),
            net_class=str(spec.get("net_class", "default") or "default"),
            pads=pads,
            min_track_width_mm=float(spec.get("min_track_width_mm", 0.25) or 0.25),
            clearance_mm=float(spec.get("clearance_mm", 0.2) or 0.2),
        )

    def _resolve_max_steps(self) -> int:
        """Compute the episode step limit."""
        if self.cfg.max_steps is not None and self.cfg.max_steps > 0:
            return int(self.cfg.max_steps)
        return max(64, 4 * (self.grid_w + self.grid_h) * self.layer_count)

    def _build_static_blocking(self) -> None:
        """Build the static blocking grid: border margin + all pad halos."""
        layers, height, width = self.layer_count, self.grid_h, self.grid_w
        margin = max(0, int(self.cfg.clearance_cells))
        for attempt_margin in (margin, 0):
            grid = np.zeros((layers, height, width), dtype=bool)
            if attempt_margin > 0:
                grid[:, :attempt_margin, :] = True
                grid[:, height - attempt_margin:, :] = True
                grid[:, :, :attempt_margin] = True
                grid[:, :, width - attempt_margin:] = True
            for net in self._nets:
                for pad in net.pads:
                    gx, gy = self.to_cell(pad.x_mm, pad.y_mm)
                    for dy in range(-attempt_margin, attempt_margin + 1):
                        for dx in range(-attempt_margin, attempt_margin + 1):
                            x, y = gx + dx, gy + dy
                            if 0 <= x < width and 0 <= y < height:
                                grid[pad.layer, y, x] = True
            if bool((~grid).any()) or attempt_margin == 0:
                self._static = grid
                return
        self._static = grid

    # ------------------------------------------------------- coordinate utils

    def to_cell(self, x_mm: float, y_mm: float) -> Tuple[int, int]:
        """Convert millimetre coordinates to integer grid cell indices."""
        gx = min(max(int(round(float(x_mm) / self.res)), 0), self.grid_w - 1)
        gy = min(max(int(round(float(y_mm) / self.res)), 0), self.grid_h - 1)
        return gx, gy

    def to_mm(self, gx: int, gy: int) -> Tuple[float, float]:
        """Convert grid cell indices to millimetre coordinates."""
        return float(gx) * self.res, float(gy) * self.res

    def _pad_cell(self, pad: _Pad) -> Cell:
        """Grid cell of a pad (on the pad's own layer)."""
        gx, gy = self.to_cell(pad.x_mm, pad.y_mm)
        return (gx, gy, pad.layer)

    @staticmethod
    def _pad_dist(a: Cell, b: Cell) -> int:
        """Manhattan distance between cells, counting a layer change as 1."""
        return abs(a[0] - b[0]) + abs(a[1] - b[1]) + (1 if a[2] != b[2] else 0)

    # ------------------------------------------------------------ properties

    @property
    def n_nets(self) -> int:
        """Number of nets known by the environment."""
        return len(self._nets)

    @property
    def net_names(self) -> List[str]:
        """Names of the nets, in declaration order."""
        return [net.name for net in self._nets]

    @property
    def grid_shape(self) -> Tuple[int, int, int]:
        """Grid dimensions as ``(H, W, layer_count)``."""
        return self.grid_h, self.grid_w, self.layer_count

    @property
    def observation_shape(self) -> Tuple[int, int, int]:
        """Observation shape ``(channels, H, W)`` with ``channels = layer_count + 3``."""
        return (self.layer_count + 3, self.grid_h, self.grid_w)

    @property
    def episode_path(self) -> List[Cell]:
        """Cell path walked during the current (or last) episode."""
        return list(self._episode.path)

    # ------------------------------------------------------- masks & routing

    def _free_mask(self, net_index: int) -> np.ndarray:
        """Boolean free-cell mask ``(L, H, W)`` for the given net.

        Static blocking (border + all pad halos) minus the current net's own
        pads and their halo, minus routes of the current net, plus routes of
        the other nets as obstacles.
        """
        layers, height, width = self.layer_count, self.grid_h, self.grid_w
        free = ~self._static  # copy through negation
        net = self._nets[net_index]
        margin = max(0, int(self.cfg.clearance_cells))
        # Unblock own pads and their clearance halo (own net may route there).
        for pad in net.pads:
            gx, gy = self.to_cell(pad.x_mm, pad.y_mm)
            for dy in range(-margin, margin + 1):
                for dx in range(-margin, margin + 1):
                    x, y = gx + dx, gy + dy
                    if 0 <= x < width and 0 <= y < height:
                        free[pad.layer, y, x] = True
        # Block routes belonging to other nets.
        for name, cells in self._routes_cells.items():
            if name == net.name:
                continue
            for (x, y, layer) in cells:
                if 0 <= x < width and 0 <= y < height and 0 <= layer < layers:
                    free[layer, y, x] = False
        return free

    def _observation(self) -> np.ndarray:
        """Build the multi-channel float32 observation for the current state."""
        channels = self.layer_count + 3
        obs = np.zeros((channels, self.grid_h, self.grid_w), dtype=np.float32)
        ep = self._episode
        if 0 <= ep.net_index < len(self._nets):
            free = self._free_mask(ep.net_index)
            obs[: self.layer_count] = ~free
        if ep.start is not None:
            obs[self.layer_count, ep.start[1], ep.start[0]] = 1.0
        for (tx, ty, _tl) in ep.targets:
            obs[self.layer_count + 1, ty, tx] = 1.0
        obs[self.layer_count + 2, :, :] = ep.layer / max(1, self.layer_count - 1)
        return obs

    def _nearest_target_distance(self) -> int:
        """Manhattan distance (cells) to the nearest remaining target."""
        ep = self._episode
        if not ep.targets:
            return 0
        here = (ep.x, ep.y, ep.layer)
        return min(self._pad_dist(here, t) for t in ep.targets)

    # ----------------------------------------------------------------- episode

    def _pick_start(self, cells: Sequence[Cell]) -> Cell:
        """Pick the pad cell nearest to the centroid of all pad cells."""
        cx = sum(c[0] for c in cells) / float(len(cells))
        cy = sum(c[1] for c in cells) / float(len(cells))
        return min(cells, key=lambda c: (abs(c[0] - cx) + abs(c[1] - cy), c))

    def reset(self, net_index: int) -> np.ndarray:
        """Start a new episode routing ``nets[net_index]``.

        Args:
            net_index: index of the net to route.

        Returns:
            The initial observation (``float32``, shape ``observation_shape``).

        Raises:
            IndexError: if ``net_index`` is out of range.
        """
        if not 0 <= net_index < len(self._nets):
            raise IndexError(f"net_index {net_index} out of range [0, {len(self._nets)})")
        net = self._nets[net_index]
        cells: List[Cell] = []
        seen: Set[Cell] = set()
        for pad in net.pads:
            cell = self._pad_cell(pad)
            if cell not in seen:
                seen.add(cell)
                cells.append(cell)

        start = self._pick_start(cells) if cells else None
        ep = self._episode
        ep.net_index = net_index
        ep.steps = 0
        ep.vias = 0
        ep.start = start
        ep.targets = [c for c in cells if c != start]
        ep.path = [start] if start else []
        ep.visited = {start} if start else set()
        ep.done = False
        ep.success = False
        if start is not None:
            ep.x, ep.y, ep.layer = start
        return self._observation()

    def step(self, action: int) -> Tuple[np.ndarray, float, bool, bool, dict]:
        """Apply one action.

        Args:
            action: index in ``[0, 12)`` from :class:`ActionSpace`.

        Returns:
            ``(obs, reward, terminated, truncated, info)``. ``terminated`` is
            True on success (a target pad cell was reached) and ``truncated``
            on the step budget exhaustion. Invalid moves are collisions: the
            agent stays in place with a negative reward.

        Raises:
            IndexError: if ``action`` is out of range.
        """
        if not self.action_space.contains(action):
            raise IndexError(f"Action {action} out of range [0, {self.action_space.n})")
        ep = self._episode
        info = self._info()
        if ep.done:  # idempotent post-terminal step
            return self._observation(), 0.0, ep.success, False, info

        dx, dy, dlayer = self.action_space.decode(int(action))
        shaper = self.reward_shaper
        reward = shaper.step_cost()
        ep.steps += 1

        prev_dist = self._nearest_target_distance()

        if dlayer != 0:
            new_layer = ep.layer + dlayer
            valid_layer = 0 <= new_layer < self.layer_count
            cell_free = (
                valid_layer
                and bool(self._free_mask(ep.net_index)[new_layer, ep.y, ep.x])
            )
            if not valid_layer or not cell_free:
                reward += shaper.collision()
            else:
                ep.layer = new_layer
                ep.vias += 1
                reward += shaper.via_cost()
                cell = (ep.x, ep.y, ep.layer)
                ep.visited.add(cell)
                ep.path.append(cell)
        else:
            nx, ny = ep.x + dx, ep.y + dy
            in_bounds = 0 <= nx < self.grid_w and 0 <= ny < self.grid_h
            free = self._free_mask(ep.net_index) if in_bounds else None
            if not in_bounds or not bool(free[ep.layer, ny, nx]):
                reward += shaper.collision()
            else:
                ep.x, ep.y = nx, ny
                cell = (ep.x, ep.y, ep.layer)
                ep.visited.add(cell)
                ep.path.append(cell)
                new_dist = self._nearest_target_distance()
                reward += shaper.progress(prev_dist, new_dist)
                if cell in ep.targets:
                    ep.targets.remove(cell)
                    ep.done = True
                    ep.success = True
                    reward += shaper.success()
                    reward += shaper.terminal_length_cost(len(ep.path))

        if not ep.done and ep.steps >= self.max_steps:
            ep.done = True
            ep.success = False
        truncated = ep.done and not ep.success
        return self._observation(), float(reward), ep.success, truncated, self._info()

    def _info(self) -> dict:
        """Info dict accompanying :meth:`step`."""
        ep = self._episode
        dist_cells = self._nearest_target_distance()
        return {
            "net": self.net_names[ep.net_index] if 0 <= ep.net_index < len(self._nets) else "",
            "success": ep.success,
            "steps": ep.steps,
            "vias": ep.vias,
            "cells_visited": len(ep.visited),
            "path_len": len(ep.path),
            "dist_cells": dist_cells,
            "dist_mm": dist_cells * self.res,
            "position": (ep.x, ep.y, ep.layer),
            "done": ep.done,
        }

    # ------------------------------------------------------------- route sets

    def set_routes(self, routes: object) -> None:
        """Register already-routed tracks as obstacles.

        Accepted inputs (normalised internally):

        * a list of route entries;
        * a dict ``{"routes": [entry, ...]}``.

        Entry formats:

        * ``{"net": str, "layer": int, "points": [{"x": .., "y": ..}, ...]}``
        * ``{"net": str, "segments": [{"a": {"x","y","layer"}, "b": {...}},
          "vias": [{"x","y","from_layer","to_layer", ...}]}]``
          (the output format of :meth:`astar_route`).

        Cells of a route never block the route's own net (a net can always be
        re-routed through / extended from its own previous geometry).
        """
        for entry in self._iter_route_entries(routes):
            name = str(entry.get("net", "") or "")
            if not name:
                continue
            cells = self._route_entry_cells(entry)
            bucket = self._routes_cells.setdefault(name, set())
            bucket.update(cells)

    def clear_net_route(self, net_name: str) -> None:
        """Forget the registered route of one net (rip-up helper)."""
        self._routes_cells.pop(net_name, None)
        self._net_routes.pop(net_name, None)

    def clear_routes(self) -> None:
        """Forget every registered route."""
        self._routes_cells.clear()
        self._net_routes.clear()

    @staticmethod
    def _iter_route_entries(routes: object) -> Iterator[dict]:
        """Yield normalised route entries from the accepted input shapes."""
        if isinstance(routes, dict):
            entries = routes.get("routes", routes if "net" in routes else [])
        else:
            entries = routes or []
        if not isinstance(entries, (list, tuple)):
            return
        for entry in entries:
            if isinstance(entry, dict):
                yield entry

    def _route_entry_cells(self, entry: dict) -> Set[Cell]:
        """Extract blocked cells from one route entry."""
        cells: Set[Cell] = set()
        segments = entry.get("segments")
        if isinstance(segments, (list, tuple)) and segments:
            for seg in segments:
                a = seg.get("a") or {}
                b = seg.get("b") or {}
                la = min(max(int(a.get("layer", 0) or 0), 0), self.layer_count - 1)
                lb = min(max(int(b.get("layer", la) or la), 0), self.layer_count - 1)
                ax, ay = self.to_cell(float(a.get("x", 0.0) or 0.0), float(a.get("y", 0.0) or 0.0))
                bx, by = self.to_cell(float(b.get("x", 0.0) or 0.0), float(b.get("y", 0.0) or 0.0))
                for (x, y) in self._raster_cells((ax, ay), (bx, by)):
                    cells.add((x, y, la))
                if lb != la:
                    cells.add((bx, by, la))
                    cells.add((bx, by, lb))
            for via in entry.get("vias") or []:
                vx, vy = self.to_cell(float(via.get("x", 0.0) or 0.0), float(via.get("y", 0.0) or 0.0))
                l1 = min(max(int(via.get("from_layer", 0) or 0), 0), self.layer_count - 1)
                l2 = min(max(int(via.get("to_layer", l1) or l1), 0), self.layer_count - 1)
                cells.add((vx, vy, l1))
                cells.add((vx, vy, l2))
            return cells

        points = entry.get("points")
        if isinstance(points, (list, tuple)) and points:
            layer = min(max(int(entry.get("layer", 0) or 0), 0), self.layer_count - 1)
            cell_pts: List[Tuple[int, int]] = [
                self.to_cell(float(p.get("x", 0.0) or 0.0), float(p.get("y", 0.0) or 0.0))
                for p in points
            ]
            for i in range(len(cell_pts) - 1):
                for (x, y) in self._raster_cells(cell_pts[i], cell_pts[i + 1]):
                    cells.add((x, y, layer))
            if len(cell_pts) == 1:
                cells.add((cell_pts[0][0], cell_pts[0][1], layer))
        return cells

    @staticmethod
    def _raster_cells(c0: Tuple[int, int], c1: Tuple[int, int]) -> List[Tuple[int, int]]:
        """Integer straight line between two cells (diagonal-safe raster)."""
        x0, y0 = c0
        x1, y1 = c1
        n = max(abs(x1 - x0), abs(y1 - y0))
        if n == 0:
            return [(x0, y0)]
        out: List[Tuple[int, int]] = []
        for i in range(n + 1):
            t = i / n
            out.append((int(round(x0 + (x1 - x0) * t)), int(round(y0 + (y1 - y0) * t))))
        return out

    # ---------------------------------------------------------------- A* router

    def astar_route(self, net_index: int) -> dict:
        """Deterministically route one net with 4-connected multi-leg A*.

        The net's pads are chained nearest-terminal-first: each leg runs an
        A* search (unit move cost, ``via_penalty`` per layer change, manhattan
        heuristic) from an already-connected pad to the nearest unconnected
        pad. Collinear runs are merged into segments.

        Args:
            net_index: index of the net to route.

        Returns:
            Route dict ``{"net", "segments", "vias", "length_mm",
            "completed"}`` where ``completed`` is True only when every pad
            was connected. Unreachable targets yield ``completed=False``
            with the (possibly empty) geometry of the legs that succeeded.
        """
        if not 0 <= net_index < len(self._nets):
            raise IndexError(f"net_index {net_index} out of range [0, {len(self._nets)})")
        net = self._nets[net_index]
        self.clear_net_route(net.name)

        pad_cells: List[Cell] = []
        seen: Set[Cell] = set()
        for pad in net.pads:
            cell = self._pad_cell(pad)
            if cell not in seen:
                seen.add(cell)
                pad_cells.append(cell)

        empty = {"net": net.name, "segments": [], "vias": [], "length_mm": 0.0, "completed": False}
        if not pad_cells:
            return empty
        if len(pad_cells) == 1:
            route = {"net": net.name, "segments": [], "vias": [], "length_mm": 0.0, "completed": True}
            self._net_routes[net.name] = route
            return route

        start = self._pick_start(pad_cells)
        connected: Set[Cell] = {start}
        remaining: List[Cell] = [c for c in pad_cells if c != start]
        segments: List[dict] = []
        vias: List[dict] = []
        all_cells: Set[Cell] = set()
        completed = True

        while remaining:
            target = min(
                remaining,
                key=lambda t: (min(self._pad_dist(t, c) for c in connected), t),
            )
            source = min(
                connected,
                key=lambda c: (self._pad_dist(c, target), c),
            )
            leg = self._astar(net_index, source, target)
            if leg is None:
                completed = False
                break
            leg_cells = list(leg)
            leg_segments, leg_vias = self._cells_to_geometry(leg_cells, net.min_track_width_mm)
            segments.extend(leg_segments)
            vias.extend(leg_vias)
            bucket = self._routes_cells.setdefault(net.name, set())
            bucket.update(leg_cells)
            all_cells.update(leg_cells)
            connected.add(target)
            remaining.remove(target)

        route = {
            "net": net.name,
            "segments": segments,
            "vias": vias,
            "length_mm": self._segments_length_mm(segments),
            "completed": completed,
        }
        self._net_routes[net.name] = route
        return route

    def route_all(self, strategy: str = "astar") -> List[dict]:
        """Route every net, shortest first (progressive congestion).

        Nets are ordered by their minimal pad-to-pad manhattan span (short
        nets first, the classic greedy heuristic) and every successful
        result is registered as an obstacle for the subsequent nets, which
        makes this the convenience entry point used by the gRPC service and
        the benchmark.

        Args:
            strategy: only ``"astar"`` is currently supported.

        Returns:
            The list of route dicts, in the order the nets were routed.
        """
        if strategy != "astar":
            raise ValueError(f"Unsupported routing strategy: {strategy!r} (expected 'astar')")
        order = sorted(
            range(len(self._nets)),
            key=lambda i: (self._net_span_cells(i), self._nets[i].name),
        )
        return [self.astar_route(i) for i in order]

    def _net_span_cells(self, net_index: int) -> float:
        """Minimal pairwise manhattan span (cells) of a net's pads."""
        cells = [self._pad_cell(pad) for pad in self._nets[net_index].pads]
        if len(cells) < 2:
            return 0.0
        best = math.inf
        for i in range(len(cells)):
            for j in range(i + 1, len(cells)):
                best = min(best, self._pad_dist(cells[i], cells[j]))
        return best

    def _astar(self, net_index: int, source: Cell, goal: Cell) -> Optional[List[Cell]]:
        """Point-to-point A* on the free-cell grid (4-connected + vias).

        Costs: 1 per orthogonal move, ``via_penalty`` per layer change.
        Heuristic: manhattan distance in x/y (admissible).

        Returns:
            The list of cells from source to goal (inclusive), or ``None``
            when the goal is unreachable.
        """
        if source == goal:
            return [source]
        layers, height, width = self.layer_count, self.grid_h, self.grid_w
        free = self._free_mask(net_index)
        gx, gy, glayer = goal
        free[glayer, gy, gx] = True  # the goal pad is always routable
        via_cost = float(self.cfg.via_penalty)

        def heuristic(cell: Cell) -> float:
            return abs(cell[0] - gx) + abs(cell[1] - gy)

        counter = 0
        open_heap: List[Tuple[float, float, int, Cell]] = [(heuristic(source), 0.0, counter, source)]
        g_score: Dict[Cell, float] = {source: 0.0}
        parent: Dict[Cell, Cell] = {}
        closed: Set[Cell] = set()

        while open_heap:
            _f, g, _c, cell = heapq.heappop(open_heap)
            if cell in closed:
                continue
            closed.add(cell)
            if cell == goal:
                path = [cell]
                while path[-1] != source:
                    path.append(parent[path[-1]])
                path.reverse()
                return path
            x, y, layer = cell
            neighbours: List[Tuple[Cell, float]] = []
            for dx, dy in _MOVES_4:
                nx, ny = x + dx, y + dy
                if 0 <= nx < width and 0 <= ny < height and bool(free[layer, ny, nx]):
                    neighbours.append(((nx, ny, layer), 1.0))
            for dlayer in (1, -1):
                nl = layer + dlayer
                if 0 <= nl < layers and bool(free[nl, y, x]):
                    neighbours.append(((x, y, nl), via_cost))
            for neighbour, cost in neighbours:
                ng = g + cost
                if ng < g_score.get(neighbour, math.inf):
                    g_score[neighbour] = ng
                    parent[neighbour] = cell
                    counter += 1
                    heapq.heappush(
                        open_heap, (ng + heuristic(neighbour), ng, counter, neighbour)
                    )
        return None

    # ------------------------------------------------------------- conversions

    def path_to_route(
        self, net_index: int, cells: Sequence[Cell], register: bool = True
    ) -> dict:
        """Convert an episode cell path into the standard route dict.

        Used to export RL rollouts. ``completed`` is True only when the path
        covers every distinct pad cell of the net (single-target rollouts of
        nets with more than two pads are reported as partial). When
        ``register`` is True the geometry is registered as an obstacle for
        the subsequent nets, mirroring :meth:`astar_route`.
        """
        net = self._nets[net_index]
        path = list(cells)
        if not path:
            return {"net": net.name, "segments": [], "vias": [], "length_mm": 0.0, "completed": False}
        segments, vias = self._cells_to_geometry(path, net.min_track_width_mm)
        pad_cells = {self._pad_cell(pad) for pad in net.pads}
        completed = pad_cells.issubset(set(path)) if pad_cells else False
        route = {
            "net": net.name,
            "segments": segments,
            "vias": vias,
            "length_mm": self._segments_length_mm(segments),
            "completed": completed,
        }
        if register:
            bucket = self._routes_cells.setdefault(net.name, set())
            bucket.update(path)
            self._net_routes[net.name] = route
        return route

    def _cells_to_geometry(
        self, cells: Sequence[Cell], width_mm: float
    ) -> Tuple[List[dict], List[dict]]:
        """Merge a cell path into collinear segments + via descriptors."""
        segments: List[dict] = []
        vias: List[dict] = []
        if not cells:
            return segments, vias

        def emit(a: Cell, b: Cell) -> None:
            if a == b:
                return
            ax_mm, ay_mm = self.to_mm(a[0], a[1])
            bx_mm, by_mm = self.to_mm(b[0], b[1])
            segments.append(
                {
                    "a": {"x": ax_mm, "y": ay_mm, "layer": a[2]},
                    "b": {"x": bx_mm, "y": by_mm, "layer": b[2]},
                    "width_mm": float(width_mm),
                }
            )

        seg_start = cells[0]
        prev = cells[0]
        direction: Optional[Tuple[int, int]] = None
        for cell in cells[1:]:
            if cell[0] == prev[0] and cell[1] == prev[1] and cell[2] != prev[2]:
                # Layer change on the spot: close the segment, emit the via.
                emit(seg_start, prev)
                seg_start = cell
                direction = None
                x_mm, y_mm = self.to_mm(cell[0], cell[1])
                vias.append(
                    {
                        "x": x_mm,
                        "y": y_mm,
                        "from_layer": prev[2],
                        "to_layer": cell[2],
                        "diameter_mm": round(max(2.0 * width_mm, 0.6), 4),
                        "drill_mm": round(max(width_mm, 0.3), 4),
                    }
                )
            else:
                step = (cell[0] - prev[0], cell[1] - prev[1])
                if direction is not None and step != direction:
                    emit(seg_start, prev)
                    seg_start = prev
                direction = step
            prev = cell
        emit(seg_start, prev)
        return segments, vias

    @staticmethod
    def _segments_length_mm(segments: Sequence[dict]) -> float:
        """Total Euclidean length of a segment list (mm)."""
        total = 0.0
        for seg in segments:
            a, b = seg.get("a") or {}, seg.get("b") or {}
            total += math.hypot(
                float(b.get("x", 0.0)) - float(a.get("x", 0.0)),
                float(b.get("y", 0.0)) - float(a.get("y", 0.0)),
            )
        return round(total, 4)

    # ------------------------------------------------------------------ debug

    def render(self, layer: int = 0) -> str:
        """ASCII rendering of one layer (debug helper).

        Legend: ``S`` start, ``T`` target, ``*`` agent, ``#`` obstacle,
        ``o`` foreign pad halo, ``.`` free cell.
        """
        ep = self._episode
        free = self._free_mask(ep.net_index) if 0 <= ep.net_index < len(self._nets) else None
        rows: List[str] = []
        for y in range(self.grid_h):
            row_chars: List[str] = []
            for x in range(self.grid_w):
                cell = (x, y, layer)
                if ep.start == cell:
                    row_chars.append("S")
                elif any(t[:2] == (x, y) for t in ep.targets):
                    row_chars.append("T")
                elif (ep.x, ep.y, ep.layer) == cell:
                    row_chars.append("*")
                elif free is not None and not bool(free[layer, y, x]):
                    row_chars.append("#")
                else:
                    row_chars.append(".")
            rows.append("".join(row_chars))
        return "\n".join(rows)

    def __repr__(self) -> str:  # pragma: no cover - debug helper
        return (
            f"PCBRouteEnv(grid={self.grid_w}x{self.grid_h}x{self.layer_count}, "
            f"res={self.res}mm, nets={self.n_nets})"
        )
