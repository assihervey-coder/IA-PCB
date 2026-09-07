#!/usr/bin/env python3
"""Experience E : convs dilatees + curriculum 48 cartes + sondes 60."""

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
    for k in range(32):
        board, nets = make_synthetic_board(3000 + k)
        board["grid_resolution_mm"] = 0.5
        demos.extend(collect_demos(board, nets, probes=40))
    steps_total = sum(len(d["actions"]) for d in demos)
    print(f"collecte: {len(demos)} demos / {steps_total} pas en "
          f"{time.monotonic() - t0:.1f}s", flush=True)

    root = AI_ENGINE_ROOT / "training/router"
    save_demos(demos, root / "demos_cur.npz")
    stats = train_bc(str(root / "demos_cur.npz"), str(root / "model_cur.pt"),
                     epochs=8, batch_size=128, lr=3e-4, time_budget_s=500.0)
    print(f"E train accuracy {stats['accuracy']:.3f}", flush=True)

    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(root / "model_cur.pt"))
    for res, seed in ((0.5, 88888), (0.25, 99999), (0.25, 77777)):
        board, nets = make_synthetic_board(seed)
        board["grid_resolution_mm"] = res
        env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
        ok = 0
        ratios = []
        t0 = time.monotonic()
        for i in range(env.n_nets):
            route = _greedy_rollout(env, i, agent)
            if route is not None and route.get("completed"):
                ok += 1
                ratios.append(route["length_mm"] /
                              max(1e-6, route["length_mm"]))  # placeholder
        dt = time.monotonic() - t0
        print(f"E held-out res={res} seed={seed}: {ok}/{env.n_nets} nets "
              f"complets en {dt:.1f}s", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
