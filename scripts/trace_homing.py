#!/usr/bin/env python3
"""Micro-trace : suffixe glouton depuis un préfixe A* d'un net en échec.

Affiche pas à pas : distance à la cible, action choisie, direction « bonne »
(signe du delta vers la cible). Objectif : voir QUAND la politique quitte le
corridor et si elle s'éloigne d'une cible visible.

Usage : python3 trace_homing.py [model.pt] [seed] [net_name] [fraction]
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.action_space import MOVE_NAMES, MODE_NAMES  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402

sys.path.insert(0, "/home/z/my-project/scripts")
from diag_prefix import action_for_step, replay_prefix  # noqa: E402

ROLLOUT_CAP = 300


def main() -> int:
    model = sys.argv[1] if len(sys.argv) > 1 else \
        str(ROOT / "training/router/model_bc_4l.pt")
    seed = int(sys.argv[2]) if len(sys.argv) > 2 else 46000
    net_name = sys.argv[3] if len(sys.argv) > 3 else "GND"
    fraction = float(sys.argv[4]) if len(sys.argv) > 4 else 0.9
    layers = 4

    agent = load_ppo_agent(model, in_channels=layers + 4)
    board, nets = make_realistic_board(seed, layer_count=layers)
    net_index = next(i for i, n in enumerate(nets) if n["name"] == net_name)

    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    env.reset(net_index)
    ep = env._episode
    source = (ep.x, ep.y, ep.layer)
    target = min(ep.targets, key=lambda t: env._pad_dist(source, t))
    leg = env._astar(net_index, source, target)
    if leg is None:
        print("A* introuvable pour ce net")
        return 1
    k = int(round(fraction * (len(leg) - 1)))

    print(f"net={net_name} span={env._pad_dist(source, target)} "
          f"leg={len(leg) - 1} prefix k={k} "
          f"pos={leg[k]} target={target}")
    env2 = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    env2.max_steps = ROLLOUT_CAP
    env2.reset(net_index)
    if not replay_prefix(env2, leg, k):
        print("REPLAY MISMATCH")
        return 1

    print(f"{'pas':>4} {'pos':>14} {'dist':>5} {'action':>14} {'vers cible?':>11}")
    obs = env2._observation()
    for step in range(ROLLOUT_CAP - k):
        ep = env2._episode
        pos = (ep.x, ep.y, ep.layer)
        d_before = env2._nearest_target_distance()
        action = agent.select_action(obs, greedy=True)
        dx, dy, dl = env2.action_space.decode(action)
        # direction « bonne » : rapproche-t-elle de la cible la plus proche ?
        tmin = min(ep.targets, key=lambda t: env2._pad_dist(pos, t))
        good = (abs(pos[0] + dx - tmin[0]) + abs(pos[1] + dy - tmin[1])
                < abs(pos[0] - tmin[0]) + abs(pos[1] - tmin[1]))
        obs, _r, term, trunc, _i = env2.step(action)
        d_after = env2._nearest_target_distance()
        name = (f"{MOVE_NAMES[action // 3]}+{MODE_NAMES[action % 3]}")
        flag = "OK" if good else "BAD"
        print(f"{step:>4} ({ep.x:>3},{ep.y:>3},l{ep.layer}) {d_after:>5} "
              f"{name:>14} {flag:>11} d:{d_before}->{d_after}")
        if term:
            print("SUCCÈS")
            break
        if trunc:
            print("TRONCATURE (échec)")
            break
    return 0


if __name__ == "__main__":
    sys.exit(main())
