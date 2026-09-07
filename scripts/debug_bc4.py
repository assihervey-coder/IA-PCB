#!/usr/bin/env python3
"""Experience D : curriculum res 0.5mm (rapide) -> generalisation hors train."""

from __future__ import annotations

import sys
import time
from pathlib import Path

AI_ENGINE_ROOT = Path(__file__).resolve().parents[1] / "yahriacad" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE_ROOT))
sys.path.insert(0, str(AI_ENGINE_ROOT.parents[0] / "shared" / "gen" / "python"))

import numpy as np  # noqa: E402

from evaluation.bench import make_synthetic_board  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402
from training.router.imitation import (  # noqa: E402
    _greedy_rollout,
    collect_demos,
    save_demos,
    train_bc,
)


def main() -> int:
    t0 = time.monotonic()
    demos = []
    n_boards = 24
    for k in range(n_boards):
        board, nets = make_synthetic_board(2000 + k)
        board["grid_resolution_mm"] = 0.5  # grilles 4x plus petites
        demos.extend(collect_demos(board, nets, probes=30))
    dt = time.monotonic() - t0
    steps_total = sum(len(d["actions"]) for d in demos)
    print(f"collecte: {len(demos)} demos / {steps_total} pas en {dt:.1f}s", flush=True)

    root = AI_ENGINE_ROOT / "training/router"
    save_demos(demos, root / "demos_cur.npz")
    t0 = time.monotonic()
    stats = train_bc(str(root / "demos_cur.npz"), str(root / "model_cur.pt"),
                     epochs=8, batch_size=96, lr=3e-4)
    print(f"D train accuracy {stats['accuracy']:.3f} en {time.monotonic() - t0:.1f}s",
          flush=True)

    # generalisation : carte tenue hors entrainement, pleine resolution 0.25
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(root / "model_cur.pt"))
    for res, seed in ((0.25, 99999), (0.5, 88888)):
        board, nets = make_synthetic_board(seed)
        board["grid_resolution_mm"] = res
        env_star = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
        star = {env_star.net_names[i]: env_star.astar_route(i)
                for i in range(env_star.n_nets)}
        env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
        ok = 0
        ratios = []
        t0 = time.monotonic()
        for i in range(env.n_nets):
            route = _greedy_rollout(env, i, agent)
            name = env.net_names[i]
            if route is not None and route.get("completed"):
                ok += 1
                s = star.get(name, {}).get("length_mm", 0.0)
                if s > 0:
                    ratios.append(route["length_mm"] / s)
        dt = time.monotonic() - t0
        print(f"D held-out res={res}: {ok}/{env.n_nets} nets, "
              f"len rel. {np.mean(ratios) if ratios else float('nan'):.3f}, "
              f"{dt:.1f}s", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
