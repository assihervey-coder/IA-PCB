#!/usr/bin/env python3
"""Diagnostic BC : accord politique vs expert + rollouts pas a pas."""

from __future__ import annotations

import sys
from pathlib import Path

AI_ENGINE_ROOT = Path(__file__).resolve().parents[1] / "yahriacad" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE_ROOT))
sys.path.insert(0, str(AI_ENGINE_ROOT.parents[0] / "shared" / "gen" / "python"))

import numpy as np  # noqa: E402

from evaluation.bench import make_synthetic_board  # noqa: E402
from src.environment.action_space import ActionSpace  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402
from training.router.imitation import (  # noqa: E402
    _greedy_rollout,
    _route_order,
    collect_demos,
    train_bc,
)


def main() -> int:
    board, nets = make_synthetic_board(42002)
    demos = collect_demos(board, nets)
    steps_total = sum(len(d["actions"]) for d in demos)
    print(f"demos: {len(demos)} nets / {steps_total} pas", flush=True)

    out = Path(AI_ENGINE_ROOT) / "training/router/model_debug_bc.pt"
    from training.router.imitation import save_demos

    save_demos(demos, Path(AI_ENGINE_ROOT) / "training/router/demos_debug.npz")
    stats = train_bc(
        str(Path(AI_ENGINE_ROOT) / "training/router/demos_debug.npz"),
        str(out),
        epochs=6,
        batch_size=16,
        lr=1e-3,
    )

    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(out))
    sp = ActionSpace()

    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    for net_index in _route_order(env)[:4]:
        demo_steps = None
        for d in demos:
            if d["net"] == env.net_names[net_index]:
                demo_steps = d
        route = _greedy_rollout(env, net_index, agent)
        if route is None:
            print(f"net {env.net_names[net_index]}: ECHEC rollout", flush=True)
            # detail : rejouer et comparer a l'expert
            obs = env.reset(net_index)
            ep = env._episode
            for step in range(12):
                action = agent.select_action(obs, greedy=True)
                expert = demo_steps["actions"][step] if demo_steps and step < len(demo_steps["actions"]) else -1
                mark = "OK" if action == expert else f"XX(expert={sp.name(expert)})"
                print(
                    f"   pas {step}: politique={sp.name(action)} {mark} "
                    f"pos={ep.x},{ep.y},{ep.layer}",
                    flush=True,
                )
                obs, _r, term, trunc, _i = env.step(action)
                if term or trunc:
                    break
        else:
            print(
                f"net {env.net_names[net_index]}: OK "
                f"{route['length_mm']:.1f} mm, {len(route['vias'])} vias",
                flush=True,
            )
    print("accuracy finale:", stats["accuracy"], flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
