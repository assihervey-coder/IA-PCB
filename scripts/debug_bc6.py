#!/usr/bin/env python3
"""Experience F : passe DAgger sur le modele BC + re-entrainement."""

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
    collect_dagger,
    collect_demos,
    save_demos,
    train_bc,
)

ROOT = AI_ENGINE_ROOT / "training/router"


def evaluate(agent, res: float, seed: int) -> tuple[int, int, float]:
    board, nets = make_synthetic_board(seed)
    board["grid_resolution_mm"] = res
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    ok = 0
    t0 = time.monotonic()
    for i in range(env.n_nets):
        route = _greedy_rollout(env, i, agent)
        if route is not None and route.get("completed"):
            ok += 1
    return ok, env.n_nets, time.monotonic() - t0


def main() -> int:
    # 1) curriculum de base (meme recette que E)
    demos = []
    for k in range(32):
        board, nets = make_synthetic_board(3000 + k)
        board["grid_resolution_mm"] = 0.5
        demos.extend(collect_demos(board, nets, probes=40))
    base_steps = sum(len(d["actions"]) for d in demos)
    print(f"curriculum: {len(demos)} demos / {base_steps} pas", flush=True)

    # 2) DAgger avec le modele BC de l'experience E sur 12 cartes fraiches
    dagger = []
    for k in range(12):
        board, nets = make_synthetic_board(5000 + k)
        board["grid_resolution_mm"] = 0.5
        dagger.extend(collect_dagger(str(ROOT / "model_cur.pt"), board, nets))
    dag_steps = sum(len(d["actions"]) for d in dagger)
    print(f"dagger: {len(dagger)} demos / {dag_steps} pas", flush=True)

    # 3) fusion + re-entrainement depuis le checkpoint BC
    save_demos(demos + dagger, ROOT / "demos_dag.npz")
    stats = train_bc(str(ROOT / "demos_dag.npz"), str(ROOT / "model_dag.pt"),
                     epochs=6, batch_size=128, lr=3e-4,
                     init_from=str(ROOT / "model_cur.pt"), time_budget_s=430.0)
    print(f"F train accuracy {stats['accuracy']:.3f}", flush=True)

    # 4) evaluation tenue a l'ecart
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(ROOT / "model_dag.pt"))
    for res, seed in ((0.5, 88888), (0.25, 99999), (0.25, 77777)):
        ok, total, dt = evaluate(agent, res, seed)
        print(f"F held-out res={res} seed={seed}: {ok}/{total} nets en {dt:.1f}s",
              flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
