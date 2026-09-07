#!/usr/bin/env python3
"""Environment smoke test (torch-free, runs in ~1 second).

Checks the :class:`~src.environment.pcb_env.PCBRouteEnv` core loop:

1. build a 20 x 15 mm 2-layer board with 2 nets (2 pads each);
2. ``reset(0)`` then 5 printed ``step(...)`` transitions;
3. ``astar_route`` on both nets must succeed with a positive length.

Exit code 0 = PASS, 1 = FAIL.
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import numpy as np  # noqa: E402
from src.environment.action_space import ActionSpace  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402


def build_board() -> tuple:
    """20 x 15 mm 2-layer board with two 2-pad nets."""
    board = {
        "width_mm": 20.0,
        "height_mm": 15.0,
        "layer_count": 2,
        "grid_resolution_mm": 0.25,
        "layer_names": ["F.Cu", "B.Cu"],
    }
    nets = [
        {
            "name": "NET1",
            "net_class": "default",
            "pads": [
                {
                    "component_ref": "U1",
                    "pad_name": "1",
                    "position": {"x": 2.0, "y": 2.0},
                    "layer": 0,
                    "width_mm": 0.6,
                    "height_mm": 0.6,
                },
                {
                    "component_ref": "U2",
                    "pad_name": "1",
                    "position": {"x": 17.0, "y": 12.0},
                    "layer": 0,
                    "width_mm": 0.6,
                    "height_mm": 0.6,
                },
            ],
            "min_track_width_mm": 0.25,
            "clearance_mm": 0.2,
        },
        {
            "name": "NET2",
            "net_class": "default",
            "pads": [
                {
                    "component_ref": "U3",
                    "pad_name": "1",
                    "position": {"x": 2.0, "y": 12.0},
                    "layer": 1,
                    "width_mm": 0.6,
                    "height_mm": 0.6,
                },
                {
                    "component_ref": "U4",
                    "pad_name": "1",
                    "position": {"x": 17.0, "y": 2.0},
                    "layer": 1,
                    "width_mm": 0.6,
                    "height_mm": 0.6,
                },
            ],
            "min_track_width_mm": 0.25,
            "clearance_mm": 0.2,
        },
    ]
    return board, nets


def main() -> int:
    """Run every smoke check; print progress and return an exit code."""
    print("[smoke] PCBRouteEnv - building 20x15mm 2-layer board, 2 nets")
    board, nets = build_board()
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    assert env.observation_shape == (5, 61, 81), f"unexpected shape: {env.observation_shape}"

    print(f"[smoke] observation shape = {env.observation_shape}, max_steps = {env.max_steps}")
    obs = env.reset(0)
    assert obs.dtype == np.float32 and obs.shape == env.observation_shape

    space = ActionSpace()
    rng_step = [3, 3, 3, 11, 6]  # deterministic sequence: right x3, via-up, down
    for i, action in enumerate(rng_step, start=1):
        obs, reward, terminated, truncated, info = env.step(action)
        print(
            f"  step {i}: action={action:>2} ({space.name(action):<12})"
            f" reward={reward:+8.3f} pos={info['position']}"
            f" dist={info['dist_mm']:.2f}mm visited={info['cells_visited']}"
            f" term={terminated} trunc={truncated}"
        )
        assert isinstance(reward, float)

    print("[smoke] astar_route(0) + astar_route(1)")
    route0 = env.astar_route(0)
    route1 = env.astar_route(1)
    for route in (route0, route1):
        print(
            f"  net={route['net']:<6} length={route['length_mm']:.2f} mm"
            f" segments={len(route['segments'])} vias={len(route['vias'])}"
            f" completed={route['completed']}"
        )
        assert route["completed"] is True, f"net {route['net']} not completed"
        assert route["length_mm"] > 0.0, f"net {route['net']} has zero length"

    # Registered routes must act as obstacles: re-route NET1 with NET2 present.
    route0_again = env.astar_route(0)
    assert route0_again["completed"] is True

    # route_all must return one result per net.
    routes = env.route_all("astar")
    assert len(routes) == 2 and all(r["completed"] for r in routes)

    print("[smoke] PASS - environment + A* router OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
