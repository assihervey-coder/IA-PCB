#!/usr/bin/env python3
"""tracemalloc sur un mini-flux PPO réel : collect 64 → update.

Objectif : trouver QUI alloue les ~700MB-1GB transitoires (t=6s du crash).
"""
import sys
import tracemalloc

sys.path.insert(0, ".")

import torch  # noqa: E402
import numpy as np  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402
from src.agents.ppo_agent import PPOConfig  # noqa: E402

tracemalloc.start(25)

ag = load_ppo_agent("training/router/model_4l_c2w_bc3.pt", in_channels=8)
b, n = make_realistic_board(47000, layer_count=4)
env = PCBRouteEnv(b, n, EnvConfig(clearance_cells=1, seed=47))
print(f"apres setup: {tracemalloc.get_traced_memory()[1]/1e6:.0f}MB tracé", flush=True)

# --- collect 64 ---
buffer_obs, buffer_act, buffer_lp, buffer_val = [], [], [], []
net_index = 0
obs = env.reset(net_index)
for step in range(64):
    a, lp, v = ag.act_with_info(obs)
    nobs, r, term, trunc, _ = env.step(a)
    buffer_obs.append(obs)
    buffer_act.append(a)
    buffer_lp.append(lp)
    buffer_val.append(v)
    obs = nobs
    if term or trunc:
        net_index = (net_index + 1) % env.n_nets
        obs = env.reset(net_index)
cur, peak = tracemalloc.get_traced_memory()
print(f"apres collect: cur={cur/1e6:.0f}MB peak={peak/1e6:.0f}MB", flush=True)

# --- update (mimique agent.update) ---
batch = {
    "obs": np.asarray(buffer_obs, dtype=np.float32),
    "actions": np.asarray(buffer_act, dtype=np.int64),
    "logprobs": np.asarray(buffer_lp, dtype=np.float32),
    "returns": np.ones(64, dtype=np.float32) * 50.0,
    "advantages": np.random.randn(64).astype(np.float32),
}
cur, peak = tracemalloc.get_traced_memory()
print(f"apres asarray: cur={cur/1e6:.0f}MB peak={peak/1e6:.0f}MB", flush=True)

stats = ag.update(batch)
cur, peak = tracemalloc.get_traced_memory()
print(f"apres update: cur={cur/1e6:.0f}MB peak={peak/1e6:.0f}MB", flush=True)

snap = tracemalloc.take_snapshot()
print("\n=== TOP 12 allocations (par traceback) ===", flush=True)
for stat in snap.statistics("lineno")[:12]:
    print(f"{stat.size/1e6:8.1f}MB  {stat.count:6d} blocks  {stat}", flush=True)
