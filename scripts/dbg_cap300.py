#!/usr/bin/env python3
"""Comparaison par net : cap 300 fixe vs cap adaptatif (debug Task 26).

Usage : dbg_cap300.py <model> <seed> <phase 1|2>
  phase 1 = cap 300 fixe (sémantique legacy), écrit /tmp/dbg_legacy.json
  phase 2 = cap adaptatif par net, écrit /tmp/dbg_adaptive.json
  phase 3 = comparaison des deux JSON
"""
import json
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))
sys.path.insert(0, "/home/z/my-project/scripts")

import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402
from eval_adaptive import net_dist, adaptive_rollout  # noqa: E402

model = sys.argv[1]
seed = int(sys.argv[2])
phase = sys.argv[3]
layers = 4

if phase == "1":
    agent = load_ppo_agent(model, in_channels=layers + 4)
    board, nets = make_realistic_board(seed, layer_count=layers)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    env.max_steps = 300
    legacy = {}
    for net_index in range(env.n_nets):
        obs = env.reset(net_index)
        info = {}
        for _ in range(env.max_steps):
            a = agent.select_action(obs, greedy=True)
            obs, _r, term, trunc, info = env.step(a)
            if term or trunc:
                break
        legacy[net_index] = bool(info.get("success"))
        print(f"net {net_index}: {legacy[net_index]}", flush=True)
    json.dump(legacy, open("/tmp/dbg_legacy.json", "w"))
    print("phase 1 OK", flush=True)
elif phase == "2":
    agent = load_ppo_agent(model, in_channels=layers + 4)
    board, nets = make_realistic_board(seed, layer_count=layers)
    env2 = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    ada = {}
    for net_index in range(env2.n_nets):
        dist = net_dist(env2, net_index)
        env2.max_steps = max(300, dist * 3 + 50)
        r = adaptive_rollout(env2, net_index, agent)
        ada[net_index] = {"done": r["done"], "steps": r["steps"],
                          "ok300": r["done"] and r["steps"] <= 300}
        print(f"net {net_index}: dist={dist} done={r['done']} steps={r['steps']}", flush=True)
    json.dump(ada, open("/tmp/dbg_adaptive.json", "w"))
    print("phase 2 OK", flush=True)
else:
    legacy = {int(k): v for k, v in json.load(open("/tmp/dbg_legacy.json")).items()}
    ada = {int(k): v for k, v in json.load(open("/tmp/dbg_adaptive.json")).items()}
    diffs = [k for k in sorted(legacy) if legacy[k] != ada[k]["ok300"]]
    print(f"legacy ok={sum(legacy.values())}, adaptatif ok300={sum(1 for v in ada.values() if v['ok300'])}")
    print(f"nets divergents: {diffs}")
    for k in diffs:
        print(f"  net {k}: legacy={legacy[k]} adaptatif={ada[k]}")
