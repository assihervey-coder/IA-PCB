#!/usr/bin/env python3
"""Trace comportementale : que fait la politique sur un net MOYEN (60-120 cells) ?"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402
torch.set_num_threads(2)
from evaluation.bench import make_realistic_board, load_ppo_agent  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402

CAP = 300
agent = load_ppo_agent(str(ROOT / "training/router/model_bc_4l.pt"), in_channels=8)

board, nets = make_realistic_board(46000, layer_count=4)
env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=46000))
env.max_steps = CAP

picked = 0
for net_index in range(env.n_nets):
    env.reset(net_index)
    start = env._episode.start
    targets = env._episode.targets
    dist = min(abs(start[0] - t[0]) + abs(start[1] - t[1]) for t in targets)
    if 60 <= dist <= 120:
        picked += 1
        obs = env._observation()
        path = [(env._episode.x, env._episode.y, env._episode.layer)]
        for _ in range(CAP):
            a = agent.select_action(obs, greedy=True)
            obs, _r, term, trunc, _i = env.step(a)
            ep = env._episode
            path.append((ep.x, ep.y, ep.layer))
            if term:
                break
            if trunc:
                break
        uniq = set(path)
        # progression : distance minimale a la cible au fil du rollout
        best = min(abs(px - t[0]) + abs(py - t[1])
                   for (px, py, _l) in path for t in targets)
        near_target = sum(1 for (px, py, _l) in path
                          if min(abs(px - t[0]) + abs(py - t[1]) for t in targets) <= 2)
        vias = sum(1 for i in range(1, len(path)) if path[i][2] != path[i - 1][2])
        print(f"net {net_index}: dist={dist} term={term} trunc={trunc} "
              f"pas={len(path) - 1} cellules_uniques={len(uniq)} "
              f"dist_min_cible={best} pas_proche_cible={near_target} vias={vias}")
        if picked >= 3:
            break
