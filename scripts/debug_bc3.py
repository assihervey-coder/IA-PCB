#!/usr/bin/env python3
"""Experience C : curriculum multi-cartes -> generalisation a une carte tenue hors train."""

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
    n_boards = 40
    for k in range(n_boards):
        board, nets = make_synthetic_board(1000 + k)
        demos.extend(collect_demos(board, nets))
    print(f"collecte: {len(demos)} demos en {time.monotonic() - t0:.1f}s", flush=True)
    steps_total = sum(len(d["actions"]) for d in demos)
    print(f"pas totaux: {steps_total}", flush=True)

    root = AI_ENGINE_ROOT / "training/router"
    save_demos(demos, root / "demos_cur.npz")
    stats = train_bc(str(root / "demos_cur.npz"), str(root / "model_cur.pt"),
                     epochs=10, batch_size=64, lr=3e-4)
    print("C train accuracy:", stats["accuracy"], flush=True)

    # generalisation : carte tenue hors entrainement (seed 99999)
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(root / "model_cur.pt"))
    board, nets = make_synthetic_board(99999)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    ok = 0
    len_ratio = []
    env_star = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    star = {env_star.net_names[i]: env_star.astar_route(i) for i in range(env_star.n_nets)}
    for i in range(env.n_nets):
        route = _greedy_rollout(env, i, agent)
        name = env.net_names[i]
        if route is not None and route.get("completed"):
            ok += 1
            s = star.get(name, {}).get("length_mm", 0.0)
            if s > 0:
                len_ratio.append(route["length_mm"] / s)
    print(f"C held-out: {ok}/{env.n_nets} nets complets, "
          f"longueur relative moy {np.mean(len_ratio) if len_ratio else float('nan'):.3f}",
          flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
