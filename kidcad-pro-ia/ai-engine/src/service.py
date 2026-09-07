"""gRPC AI router service (KidCAD-Pro-IA ai-engine).

Implements ``kidcad.pcb.v1.AIRouterService`` (see ``shared/types/pcb.proto``):

* ``GetHealth``     : liveness + configuration report;
* ``PlanPlacement`` : simulated-annealing placement (deterministic seed);
* ``RouteBoard``    : server-streaming routing, one ``ProgressEvent`` per net
  (RL greedy rollout when a trained model is loaded, deterministic A*
  fallback otherwise);
* ``OptimizeRoutes``: rip-up & reroute of the longest nets.

The module NEVER imports torch at import time: torch is only touched when a
trained checkpoint actually exists on disk, so the service works end-to-end
with the pure-Python A* fallback.
"""

from __future__ import annotations

import logging
import math
import os
import random
import sys
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple

# --------------------------------------------------------------------------
# sys.path bootstrap: generated gRPC stubs live in shared/gen/python, sibling
# packages (src.environment, evaluation) live under the ai-engine root.
# --------------------------------------------------------------------------
_THIS_FILE = Path(__file__).resolve()
AI_ENGINE_ROOT = _THIS_FILE.parents[1]
REPO_ROOT = _THIS_FILE.parents[2]
SHARED_GEN_DIR = REPO_ROOT / "shared" / "gen" / "python"
for _path in (str(SHARED_GEN_DIR), str(AI_ENGINE_ROOT)):
    if _path and os.path.isdir(_path) and _path not in sys.path:
        sys.path.insert(0, _path)

import grpc  # noqa: E402
import pcb_pb2 as pb  # noqa: E402
import pcb_pb2_grpc as pb_grpc  # noqa: E402

try:  # package import (src.service) or direct import (service)
    from .environment.pcb_env import EnvConfig, PCBRouteEnv
except ImportError:  # pragma: no cover - direct script import
    from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # type: ignore

__all__ = ["AIRouterServicer", "AI_ENGINE_ROOT", "REPO_ROOT"]

_DEFAULT_CLEARANCE_MM = 0.2
_BORDER_MARGIN_MM = 0.25


# ==========================================================================
# Conversion helpers (proto <-> plain dicts). All helpers are total: they
# tolerate missing/defaulted proto fields and never raise on None.
# ==========================================================================


def _board_to_dict(board: pb.BoardSpec) -> dict:
    """Convert a ``BoardSpec`` proto into the env board dict."""
    return {
        "width_mm": float(board.width_mm or 0.0) or 100.0,
        "height_mm": float(board.height_mm or 0.0) or 100.0,
        "layer_count": max(1, int(board.layer_count or 2)),
        "grid_resolution_mm": float(board.grid_resolution_mm or 0.0) or None,
        "layer_names": [str(name) for name in board.layer_names],
    }


def _nets_to_dicts(nets: Iterable[pb.NetSpec]) -> List[dict]:
    """Convert ``NetSpec`` protos into env net dicts (absolute pad positions)."""
    out: List[dict] = []
    for net in nets:
        pads = [
            {
                "component_ref": str(pad.component_ref or ""),
                "pad_name": str(pad.pad_name or ""),
                "position": {"x": float(pad.position.x), "y": float(pad.position.y)},
                "layer": int(pad.layer),
                "width_mm": float(pad.width_mm or 0.3),
                "height_mm": float(pad.height_mm or 0.3),
            }
            for pad in net.pads
        ]
        out.append(
            {
                "name": str(net.name or f"net{len(out)}"),
                "net_class": str(net.net_class or "default"),
                "pads": pads,
                "min_track_width_mm": float(net.min_track_width_mm or 0.25),
                "clearance_mm": float(net.clearance_mm or _DEFAULT_CLEARANCE_MM),
            }
        )
    return out


def _components_from_proto(components: Iterable[pb.ComponentSpec]) -> List[dict]:
    """Convert ``ComponentSpec`` protos into plain dicts."""
    out: List[dict] = []
    for comp in components:
        bbox = comp.bbox_mm
        out.append(
            {
                "ref": str(comp.ref or f"C{len(out)}"),
                "footprint": str(comp.footprint or ""),
                "position": {"x": float(comp.position.x), "y": float(comp.position.y)},
                "rotation_deg": float(comp.rotation_deg),
                "fixed": bool(comp.fixed),
                "bbox": {
                    "min_x": float(bbox.min_x),
                    "min_y": float(bbox.min_y),
                    "max_x": float(bbox.max_x),
                    "max_y": float(bbox.max_y),
                },
                "height_mm": float(comp.height_mm),
            }
        )
    return out


def _component_to_proto(comp: dict) -> pb.ComponentSpec:
    """Convert a plain component dict into a ``ComponentSpec`` proto."""
    position = comp.get("position") or {}
    bbox = comp.get("bbox") or {}
    proto = pb.ComponentSpec(
        ref=str(comp.get("ref", "") or ""),
        footprint=str(comp.get("footprint", "") or ""),
        rotation_deg=float(comp.get("rotation_deg", 0.0) or 0.0),
        fixed=bool(comp.get("fixed", False)),
        height_mm=float(comp.get("height_mm", 0.0) or 0.0),
    )
    proto.position.x = float(position.get("x", 0.0) or 0.0)
    proto.position.y = float(position.get("y", 0.0) or 0.0)
    proto.bbox_mm.min_x = float(bbox.get("min_x", 0.0) or 0.0)
    proto.bbox_mm.min_y = float(bbox.get("min_y", 0.0) or 0.0)
    proto.bbox_mm.max_x = float(bbox.get("max_x", 0.0) or 0.0)
    proto.bbox_mm.max_y = float(bbox.get("max_y", 0.0) or 0.0)
    return proto


def _route_to_proto(route: dict) -> pb.RouteNetResult:
    """Convert an env route dict into a ``RouteNetResult`` proto."""
    result = pb.RouteNetResult(
        net=str(route.get("net", "") or ""),
        length_mm=float(route.get("length_mm", 0.0) or 0.0),
        completed=bool(route.get("completed", False)),
        drc_violations=int(route.get("drc_violations", 0) or 0),
    )
    for seg in route.get("segments") or []:
        a = seg.get("a") or {}
        b = seg.get("b") or {}
        seg_msg = result.segments.add()
        seg_msg.a.position.x = float(a.get("x", 0.0) or 0.0)
        seg_msg.a.position.y = float(a.get("y", 0.0) or 0.0)
        seg_msg.a.layer = int(a.get("layer", 0) or 0)
        seg_msg.b.position.x = float(b.get("x", 0.0) or 0.0)
        seg_msg.b.position.y = float(b.get("y", 0.0) or 0.0)
        seg_msg.b.layer = int(b.get("layer", 0) or 0)
        seg_msg.width_mm = float(seg.get("width_mm", 0.25) or 0.25)
    for via in route.get("vias") or []:
        via_msg = result.vias.add()
        via_msg.position.x = float(via.get("x", 0.0) or 0.0)
        via_msg.position.y = float(via.get("y", 0.0) or 0.0)
        via_msg.from_layer = int(via.get("from_layer", 0) or 0)
        via_msg.to_layer = int(via.get("to_layer", 0) or 0)
        via_msg.diameter_mm = float(via.get("diameter_mm", 0.6) or 0.6)
        via_msg.drill_mm = float(via.get("drill_mm", 0.3) or 0.3)
    return result


def _route_result_to_dict(result: pb.RouteNetResult) -> dict:
    """Convert a ``RouteNetResult`` proto into the env route dict format."""
    return {
        "net": str(result.net or ""),
        "segments": [
            {
                "a": {
                    "x": float(seg.a.position.x),
                    "y": float(seg.a.position.y),
                    "layer": int(seg.a.layer),
                },
                "b": {
                    "x": float(seg.b.position.x),
                    "y": float(seg.b.position.y),
                    "layer": int(seg.b.layer),
                },
                "width_mm": float(seg.width_mm or 0.25),
            }
            for seg in result.segments
        ],
        "vias": [
            {
                "x": float(via.position.x),
                "y": float(via.position.y),
                "from_layer": int(via.from_layer),
                "to_layer": int(via.to_layer),
                "diameter_mm": float(via.diameter_mm or 0.6),
                "drill_mm": float(via.drill_mm or 0.3),
            }
            for via in result.vias
        ],
        "length_mm": float(result.length_mm or 0.0),
        "completed": bool(result.completed),
        "drc_violations": int(result.drc_violations or 0),
    }


def _flat_segments(routes: Sequence[dict]) -> List[Tuple[str, int, float, float, float, float]]:
    """Flatten route dicts into ``(net, layer, x1, y1, x2, y2)`` tuples."""
    out: List[Tuple[str, int, float, float, float, float]] = []
    for route in routes:
        net = str(route.get("net", "") or "")
        for seg in route.get("segments") or []:
            a = seg.get("a") or {}
            b = seg.get("b") or {}
            out.append(
                (
                    net,
                    int(a.get("layer", 0) or 0),
                    float(a.get("x", 0.0) or 0.0),
                    float(a.get("y", 0.0) or 0.0),
                    float(b.get("x", 0.0) or 0.0),
                    float(b.get("y", 0.0) or 0.0),
                )
            )
    return out


def _drc_violations_for_net(route: dict, prior_routes: Sequence[dict], clearance_mm: float) -> int:
    """Approximate clearance violations of ``route`` against earlier routes.

    Counts same-layer segment pairs of different nets closer than
    ``clearance_mm`` plus the corresponding via/segment and via/via checks.
    """
    try:
        from evaluation.metrics import point_segment_distance, segment_distance
    except Exception:  # pragma: no cover - metrics always importable here
        return 0

    count = 0
    eps = 1e-9
    own = str(route.get("net", "") or "")
    own_segments = _flat_segments([route])
    other_segments = _flat_segments(list(prior_routes))

    for net_a, layer_a, ax, ay, bx, by in own_segments:
        for net_b, layer_b, cx, cy, dx, dy in other_segments:
            if net_a == net_b or layer_a != layer_b:
                continue
            if segment_distance((ax, ay), (bx, by), (cx, cy), (dx, dy)) < clearance_mm - eps:
                count += 1

    own_vias = [
        (own, float(v.get("x", 0.0) or 0.0), float(v.get("y", 0.0) or 0.0),
         {int(v.get("from_layer", 0) or 0), int(v.get("to_layer", 0) or 0)})
        for v in route.get("vias") or []
    ]
    other_vias = [
        (str(r.get("net", "") or ""), float(v.get("x", 0.0) or 0.0), float(v.get("y", 0.0) or 0.0),
         {int(v.get("from_layer", 0) or 0), int(v.get("to_layer", 0) or 0)})
        for r in prior_routes
        for v in (r.get("vias") or [])
    ]
    for net_a, vx, vy, layers_a in own_vias:
        for net_b, ox, oy, layers_b in other_vias:
            if net_a == net_b or not (layers_a & layers_b):
                continue
            if math.hypot(vx - ox, vy - oy) < clearance_mm - eps:
                count += 1
        for net_b, layer_b, cx, cy, dx, dy in other_segments:
            if net_a == net_b or layer_b not in layers_a:
                continue
            if point_segment_distance((vx, vy), (cx, cy), (dx, dy)) < clearance_mm - eps:
                count += 1
    return count


def _estimate_net_length(net: dict) -> float:
    """Cheap routability estimate: manhattan span of the net's pads."""
    pads = net.get("pads") or []
    if not pads:
        return 0.0
    xs = [float((p.get("position") or {}).get("x", 0.0) or 0.0) for p in pads]
    ys = [float((p.get("position") or {}).get("y", 0.0) or 0.0) for p in pads]
    return (max(xs) - min(xs)) + (max(ys) - min(ys))


def _job_id_from_context(context) -> str:
    """Read an optional ``job_id`` gRPC metadata key (empty by default)."""
    try:
        for key, value in context.invocation_metadata() or ():
            if key.lower() in ("job_id", "job-id", "x-job-id"):
                return str(value)
    except Exception:
        pass
    return ""


# ==========================================================================
# Placement: simulated annealing (deterministic)
# ==========================================================================


def _rect_overlap_area(
    pos_a: Sequence[float], half_a: Sequence[float],
    pos_b: Sequence[float], half_b: Sequence[float],
) -> float:
    """Overlap area of two axis-aligned rectangles (0 when disjoint)."""
    dx = min(pos_a[0] + half_a[0], pos_b[0] + half_b[0]) - max(
        pos_a[0] - half_a[0], pos_b[0] - half_b[0]
    )
    dy = min(pos_a[1] + half_a[1], pos_b[1] + half_b[1]) - max(
        pos_a[1] - half_a[1], pos_b[1] - half_b[1]
    )
    return max(0.0, dx) * max(0.0, dy)


def _clamp_position(
    x: float, y: float, half_w: float, half_h: float, width: float, height: float
) -> Tuple[float, float]:
    """Clamp a component center so its bbox stays inside the board."""
    margin = _BORDER_MARGIN_MM
    min_x, max_x = margin + half_w, width - margin - half_w
    min_y, max_y = margin + half_h, height - margin - half_h
    if min_x > max_x:
        min_x = max_x = width / 2.0
    if min_y > max_y:
        min_y = max_y = height / 2.0
    return min(max(x, min_x), max_x), min(max(y, min_y), max_y)


def _simulated_annealing_placement(
    components: List[dict],
    board: dict,
    rng: random.Random,
    iterations: int = 1500,
    time_budget_s: float = 2.0,
    overlap_weight: float = 5.0,
) -> Tuple[List[Tuple[float, float]], float, float]:
    """Deterministic simulated annealing placement.

    Note: the v1 ``PlacementRequest`` proto carries no netlist, so the
    wirelength term is a complete-graph HPWL proxy (star decomposition around
    the centroid of the initial positions); the overlap term penalises
    footprint collisions. Both are minimised incrementally (only the moved
    component's contributions are recomputed per iteration).

    Args:
        components: plain dicts (see ``_components_from_proto``).
        board: board dict (``width_mm`` / ``height_mm``).
        rng: seeded ``random.Random`` (determinism guarantee).
        iterations: annealing iteration cap.
        time_budget_s: wall-clock budget (checked every 64 iterations).
        overlap_weight: cost weight of one mm^2 of footprint overlap.

    Returns:
        ``(positions, final_cost, wirelength_proxy_mm)``.
    """
    width = float(board.get("width_mm", 100.0) or 100.0)
    height = float(board.get("height_mm", 100.0) or 100.0)
    n = len(components)
    if n == 0:
        return [], 0.0, 0.0

    halves: List[Tuple[float, float]] = []
    for comp in components:
        bbox = comp.get("bbox") or {}
        hw = max((float(bbox.get("max_x", 0.0)) - float(bbox.get("min_x", 0.0))) / 2.0, 0.0)
        hh = max((float(bbox.get("max_y", 0.0)) - float(bbox.get("min_y", 0.0))) / 2.0, 0.0)
        if hw <= 0.0 or hh <= 0.0:  # degenerate footprint: small default body
            hw = max(hw, 0.5)
            hh = max(hh, 0.5)
        halves.append((hw, hh))

    fixed = [bool(c.get("fixed", False)) for c in components]
    positions: List[List[float]] = []
    for comp in components:
        pos = comp.get("position") or {}
        positions.append([float(pos.get("x", 0.0) or 0.0), float(pos.get("y", 0.0) or 0.0)])

    movable = [i for i, is_fixed in enumerate(fixed) if not is_fixed]
    if movable and all(
        positions[i][0] == 0.0 and positions[i][1] == 0.0 for i in movable
    ):
        # Unplaced input: deterministic grid packing (rows), footprint-aware.
        ordered = sorted(movable, key=lambda i: components[i]["ref"])
        gap = 1.0
        cursor_x, cursor_y = 1.0, 1.0
        row_height = 0.0
        for i in ordered:
            hw, hh = halves[i]
            if cursor_x + hw > width - 1.0 and cursor_x > 1.0:
                cursor_x = 1.0
                cursor_y += row_height + gap
                row_height = 0.0
            positions[i] = [cursor_x + hw, cursor_y + hh]
            cursor_x += 2.0 * hw + gap
            row_height = max(row_height, 2.0 * hh)

    for i in range(n):
        positions[i][0], positions[i][1] = _clamp_position(
            positions[i][0], positions[i][1], halves[i][0], halves[i][1], width, height
        )

    anchor_x = sum(p[0] for p in positions) / n
    anchor_y = sum(p[1] for p in positions) / n

    def contributions(index: int, pos: Sequence[float]) -> float:
        """Cost contributions of component ``index`` at ``pos``."""
        wire = math.hypot(pos[0] - anchor_x, pos[1] - anchor_y)
        overlap = 0.0
        for j in range(n):
            if j == index:
                continue
            overlap += _rect_overlap_area(pos, halves[index], positions[j], halves[j])
        return wire + overlap_weight * overlap

    # True composite cost: star wirelength + weighted pairwise overlap.
    wire_total = sum(math.hypot(p[0] - anchor_x, p[1] - anchor_y) for p in positions)
    overlap_total = sum(
        _rect_overlap_area(positions[i], halves[i], positions[j], halves[j])
        for i in range(n)
        for j in range(i + 1, n)
    )
    total_cost = wire_total + overlap_weight * overlap_total
    best_cost = total_cost
    best_positions = [(p[0], p[1]) for p in positions]

    step_max = max(0.5, 0.05 * min(width, height))
    step_min = 0.05
    t_start = max(0.25, 0.05 * total_cost)
    t_end = 0.01
    started = time.monotonic()
    span = max(1, iterations - 1)

    for it in range(iterations):
        if it % 64 == 0 and (time.monotonic() - started) > time_budget_s:
            break
        temperature = t_start * ((t_end / t_start) ** (it / span))
        if not movable:
            break
        index = movable[rng.randrange(len(movable))]
        progress = it / span
        step = step_max + (step_min - step_max) * progress
        old_pos = (positions[index][0], positions[index][1])
        proposed = _clamp_position(
            old_pos[0] + rng.gauss(0.0, step),
            old_pos[1] + rng.gauss(0.0, step),
            halves[index][0],
            halves[index][1],
            width,
            height,
        )
        old_contrib = contributions(index, old_pos)
        new_contrib = contributions(index, proposed)
        delta = new_contrib - old_contrib
        if delta <= 0.0 or rng.random() < math.exp(-delta / max(temperature, 1e-9)):
            positions[index][0], positions[index][1] = proposed
            total_cost += delta
            if total_cost < best_cost:
                best_cost = total_cost
                best_positions = [(p[0], p[1]) for p in positions]

    wire_proxy = sum(
        math.hypot(p[0] - anchor_x, p[1] - anchor_y) for p in best_positions
    )
    return best_positions, best_cost, wire_proxy


# ==========================================================================
# Servicer
# ==========================================================================


def _detect_device(preference: str = "auto") -> str:
    """Resolve the torch device WITHOUT importing torch eagerly."""
    try:
        import torch  # lazy: only to probe CUDA availability
        has_cuda = bool(torch.cuda.is_available())
    except Exception:
        return "cpu"
    if preference == "cuda" and has_cuda:
        return "cuda"
    if preference == "auto" and has_cuda:
        return "cuda"
    return "cpu"


def _load_yaml_config(config_path: Optional[str]) -> dict:
    """Load the training/router YAML config (defensively)."""
    candidates: List[str] = []
    if config_path:
        candidates.append(config_path)
    candidates.append(str(AI_ENGINE_ROOT / "training" / "router" / "config.yaml"))
    for path in candidates:
        if path and os.path.isfile(path):
            try:
                import yaml

                with open(path, "r", encoding="utf-8") as handle:
                    data = yaml.safe_load(handle) or {}
                if isinstance(data, dict):
                    return data
            except Exception:
                continue
    return {}


class AIRouterServicer(pb_grpc.AIRouterServiceServicer):
    """Implementation of ``kidcad.pcb.v1.AIRouterService``."""

    def __init__(self, config_path: Optional[str] = None, logger: Optional[logging.Logger] = None) -> None:
        """Args:
        config_path: optional YAML config override (defaults to
            ``training/router/config.yaml`` inside the ai-engine root).
        logger: optional logger (defaults to ``kidcad.ai``).
        """
        self._log = logger if logger is not None else logging.getLogger("kidcad.ai")
        self.version = "0.1.0"

        config = _load_yaml_config(config_path)
        router_cfg = config.get("router") or {}
        env_cfg = config.get("env") or {}
        placer_cfg = config.get("placer") or {}
        optimizer_cfg = config.get("optimizer") or {}

        model_path = str(router_cfg.get("model_path", "training/router/model_v1.pt") or "")
        self._model_path = self._resolve_model_path(model_path)
        self._device = _detect_device(str(router_cfg.get("device", "auto") or "auto"))
        self._clearance_cells = max(0, int(env_cfg.get("clearance_cells", 1) or 1))
        # Placement seed: derived from the request (number of components) for
        # determinism, unless the YAML config pins an explicit integer seed.
        raw_seed = placer_cfg.get("seed")
        placer_seed = int(raw_seed) if isinstance(raw_seed, (int, float)) else None
        self._placer_cfg = {
            "iterations": max(1, int(placer_cfg.get("iterations", 1500) or 1500)),
            "time_budget_s": max(0.05, float(placer_cfg.get("time_budget_ms", 2000) or 2000) / 1000.0),
            "seed": placer_seed,
        }
        self._optimizer_cfg = {
            "via_cost_mm": float(optimizer_cfg.get("via_cost_mm", 2.0) or 2.0),
            "drc_cost": float(optimizer_cfg.get("drc_cost", 10.0) or 10.0),
            "clearance_mm": float(optimizer_cfg.get("clearance_mm", _DEFAULT_CLEARANCE_MM) or _DEFAULT_CLEARANCE_MM),
        }
        self._agent = self._load_rl_agent()

    # ------------------------------------------------------------- internals

    def _resolve_model_path(self, model_path: str) -> Optional[str]:
        """Resolve the checkpoint path (absolute or ai-engine relative)."""
        if not model_path:
            return None
        candidate = Path(model_path)
        if not candidate.is_absolute():
            candidate = AI_ENGINE_ROOT / candidate
        return str(candidate) if candidate.is_file() else None

    def _load_rl_agent(self):
        """Load the trained PPO router when torch + checkpoint are available.

        Returns the agent or ``None`` (the service then falls back to the
        deterministic A* router; this is the expected mode in production).
        """
        if not self._model_path:
            return None
        try:
            import torch  # lazy: only executed when a checkpoint exists

            try:
                from .agents.ppo_agent import PPOAgent, PPOConfig
            except ImportError:  # pragma: no cover - direct import layout
                from src.agents.ppo_agent import PPOAgent, PPOConfig

            router_cfg = {}
            try:
                import yaml
                config = _load_yaml_config(None)
                router_cfg = config.get("router") or {}
            except Exception:
                pass
            in_channels = max(1, int(router_cfg.get("in_channels", 5) or 5))
            agent = PPOAgent(
                PPOConfig(),
                in_channels=in_channels,
                n_actions=12,
                device="cuda" if torch.cuda.is_available() else "cpu",
            )
            if agent.load(self._model_path):
                self._log.info("RL router loaded from %s", self._model_path)
                return agent
            self._log.warning("RL router checkpoint unreadable: %s", self._model_path)
        except Exception as exc:
            self._log.warning("RL router unavailable (A* fallback active): %s", exc)
        return None

    def _env_config(self) -> EnvConfig:
        """EnvConfig used for every routing request."""
        return EnvConfig(clearance_cells=self._clearance_cells, seed=0)

    def _rl_route(self, env: PCBRouteEnv, net_index: int) -> Optional[dict]:
        """Greedy RL rollout for one net; ``None`` when it fails to connect."""
        agent = self._agent
        if agent is None:
            return None
        try:
            obs = env.reset(net_index)
            info: dict = {}
            for _ in range(env.max_steps):
                action = agent.select_action(obs, greedy=True)
                obs, _reward, terminated, truncated, info = env.step(action)
                if terminated or truncated:
                    break
            if not info.get("success"):
                return None
            return env.path_to_route(net_index, env.episode_path)
        except Exception as exc:
            self._log.warning("RL rollout failed on net %s: %s", env.net_names[net_index], exc)
            return None

    # -------------------------------------------------------------- health

    def GetHealth(self, request, context) -> pb.HealthResponse:
        """Report service health.

        The service answers ``ok`` whenever it is alive; ``model_loaded`` is
        False when no RL checkpoint is available, which is the nominal
        production mode (deterministic A* fallback). ``degraded`` is kept for
        a future partial-failure mode.
        """
        return pb.HealthResponse(
            status="ok",
            version=self.version,
            device=self._device,
            model_loaded=self._agent is not None,
        )

    # ----------------------------------------------------------- placement

    def PlanPlacement(self, request, context) -> pb.PlacementResult:
        """Simulated-annealing placement (deterministic seed).

        Only non-fixed components move; the optimised cost is a
        complete-graph HPWL proxy plus a footprint-overlap penalty.
        """
        try:
            board = _board_to_dict(request.board)
            components = _components_from_proto(request.components)
            # Deterministic seed: derived from the request size, so identical
            # inputs always produce identical placements (config may override).
            seed = self._placer_cfg["seed"]
            if seed is None:
                seed = len(components)
            positions, cost, wire_proxy = _simulated_annealing_placement(
                components,
                board,
                random.Random(seed),
                iterations=self._placer_cfg["iterations"],
                time_budget_s=self._placer_cfg["time_budget_s"],
            )
            result = pb.PlacementResult(
                total_wirelength_mm=round(wire_proxy, 4),
                score=round(cost, 4),
                strategy="rl" if (str(request.strategy or "") == "rl" and self._agent is not None) else "heuristic",
            )
            for comp, (x, y) in zip(components, positions):
                moved = dict(comp)
                moved["position"] = {"x": x, "y": y}
                result.placed.append(_component_to_proto(moved))
            self._log.info(
                "PlanPlacement: %d components, cost=%.2f, wire_proxy=%.2f mm",
                len(components), cost, wire_proxy,
            )
            return result
        except Exception as exc:
            self._log.exception("PlanPlacement failed")
            context.abort(grpc.StatusCode.INTERNAL, f"PlanPlacement failed: {exc}")

    # ------------------------------------------------------------- routing

    def RouteBoard(self, request, context):
        """Server-streaming routing: one ``ProgressEvent`` per net, final done.

        Nets are routed shortest-first (unless ``net_filter`` prescribes the
        order); every completed route becomes an obstacle for the next nets.
        """
        started = time.monotonic()
        try:
            board = _board_to_dict(request.board)
            all_nets = _nets_to_dicts(request.nets)
            job_id = _job_id_from_context(context)
            env = PCBRouteEnv(board, all_nets, self._env_config())

            filters = [str(name) for name in request.net_filter if str(name)]
            index_by_name = {net["name"]: i for i, net in enumerate(all_nets)}
            if filters:
                seen: set = set()
                selected = []
                for name in filters:
                    if name in index_by_name and name not in seen:
                        seen.add(name)
                        selected.append(all_nets[index_by_name[name]])
            else:
                selected = sorted(all_nets, key=lambda net: (_estimate_net_length(net), net["name"]))

            use_rl = (
                str(request.strategy or "") == "rl"
                and self._agent is not None
                and env.observation_shape[0] == self._agent.in_channels
            )
            strategy = "rl" if use_rl else "astar"
            total = len(selected)
            results: List[pb.RouteNetResult] = []
            result_dicts: List[dict] = []

            for k, net in enumerate(selected):
                if not context.is_active():
                    self._log.info("RouteBoard cancelled by the client")
                    return
                net_index = index_by_name[net["name"]]
                route = self._rl_route(env, net_index) if use_rl else None
                if route is None:
                    route = env.astar_route(net_index)
                violations = _drc_violations_for_net(
                    route, result_dicts, self._optimizer_cfg["clearance_mm"]
                )
                route["drc_violations"] = violations
                result = _route_to_proto(route)
                results.append(result)
                result_dicts.append(route)
                percent = 100.0 * (k + 1) / total if total else 100.0
                status = "completed" if result.completed else "UNROUTED"
                message = (
                    f"{net['name']}: {result.length_mm:.1f} mm, "
                    f"{len(result.vias)} via(s) - {status}"
                )
                yield pb.ProgressEvent(
                    job_id=job_id,
                    stage="route",
                    current_net=net["name"],
                    percent=percent,
                    message=message,
                    partial=result,
                    done=False,
                )

            elapsed_ms = (time.monotonic() - started) * 1000.0
            completed = sum(1 for r in results if r.completed)
            yield pb.ProgressEvent(
                job_id=job_id,
                stage="route",
                percent=100.0,
                done=True,
                message=f"{completed}/{total} nets routés en {elapsed_ms:.0f} ms (stratégie={strategy})",
            )
        except Exception as exc:
            self._log.exception("RouteBoard failed")
            yield pb.ProgressEvent(stage="route", done=True, error=str(exc))

    # ----------------------------------------------------------- optimizing

    def OptimizeRoutes(self, request, context):
        """Rip-up & reroute of the longest nets (server-streaming).

        For each net (longest first) the old geometry is ripped up and a
        fresh A* route is computed with all other routes as obstacles; the
        shorter/compliant result is kept.
        """
        started = time.monotonic()
        try:
            board = _board_to_dict(request.board)
            nets = _nets_to_dicts(request.nets)
            routes = [_route_result_to_dict(r) for r in request.routes]
            job_id = _job_id_from_context(context)
            objectives = [str(o) for o in request.objectives if str(o)] or ["length", "vias", "drc"]
            via_cost = self._optimizer_cfg["via_cost_mm"]
            drc_cost = self._optimizer_cfg["drc_cost"]

            def score(route: dict) -> float:
                value = float(route.get("length_mm", 0.0) or 0.0)
                if "vias" in objectives:
                    value += via_cost * len(route.get("vias") or [])
                if "drc" in objectives:
                    value += drc_cost * int(route.get("drc_violations", 0) or 0)
                return value

            env = PCBRouteEnv(board, nets, self._env_config())
            env.set_routes(routes)
            index_by_name = {net["name"]: i for i, net in enumerate(nets)}

            candidates = [
                route
                for route in routes
                if route.get("net") in index_by_name
                and (route.get("segments") or route.get("vias"))
            ]
            candidates.sort(key=lambda route: (-float(route.get("length_mm", 0.0) or 0.0), route.get("net", "")))

            total = len(candidates)
            improved = 0
            saved_mm = 0.0
            for k, old in enumerate(candidates):
                if not context.is_active():
                    self._log.info("OptimizeRoutes cancelled by the client")
                    return
                name = old["net"]
                old_length = float(old.get("length_mm", 0.0) or 0.0)
                env.clear_net_route(name)
                new = env.astar_route(index_by_name[name])
                prior = [r for r in routes if r.get("net") != name]
                if new.get("completed") and score(new) < score(old) - 1e-6:
                    new["drc_violations"] = _drc_violations_for_net(
                        new, prior, self._optimizer_cfg["clearance_mm"]
                    )
                    kept = new
                    improved += 1
                    saved_mm += old_length - float(new.get("length_mm", 0.0) or 0.0)
                    message = f"{name}: {old_length:.1f} -> {float(new.get('length_mm', 0.0)):.1f} mm"
                else:
                    env.clear_net_route(name)
                    env.set_routes([old])
                    kept = old
                    message = f"{name}: kept ({old_length:.1f} mm)"
                # keep the working set coherent for the next iterations
                routes = [r for r in routes if r.get("net") != name] + [kept]
                result = _route_to_proto(kept)
                yield pb.ProgressEvent(
                    job_id=job_id,
                    stage="optimize",
                    current_net=name,
                    percent=100.0 * (k + 1) / total if total else 100.0,
                    message=message,
                    partial=result,
                    done=False,
                )

            elapsed_ms = (time.monotonic() - started) * 1000.0
            yield pb.ProgressEvent(
                job_id=job_id,
                stage="optimize",
                percent=100.0,
                done=True,
                message=(
                    f"{improved}/{total} nets optimisés en {elapsed_ms:.0f} ms, "
                    f"{saved_mm:.1f} mm gagnés"
                ),
            )
        except Exception as exc:
            self._log.exception("OptimizeRoutes failed")
            yield pb.ProgressEvent(stage="optimize", done=True, error=str(exc))

    # ------------------------------------------------------------ properties

    @property
    def device(self) -> str:
        """Torch device in use ("cpu" when torch is absent)."""
        return self._device

    @property
    def model_loaded(self) -> bool:
        """True when the RL router checkpoint is loaded."""
        return self._agent is not None
