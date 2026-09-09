#!/usr/bin/env python3
"""Adoucit les logits d'un checkpoint par scaling des têtes (température).

Un BC sur-convergé (entropie ~0 sur les états multi-actions) est un puits
pour le PPO : le gradient de l'entropie est exactement nul sur une
distribution quasi one-hot. On multiplie poids ET biais des têtes de
logits (policy_head, local_head, target_head) par alpha < 1 — les têtes
sont linéaires, donc les logits sont exactement multipliés par alpha :
l'ORDRE des actions est préservé (le comportement glouton ne change pas)
mais la confiance fond et l'entropie remonte.

Calibration : entropie mesurée sur des états de CORRIDOR (rollouts depuis
les pads), où plusieurs actions sont légales — mesurer sur les pads de
départ est biaisé (souvent 1 seule action légale = entropie structurelle).

Usage : python3 soften_heads.py <in.pt> <out.pt> [entropie_cible=0.8]
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import numpy as np  # noqa: E402
import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import make_realistic_board  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402
from src.agents.ppo_agent import PPOAgent, PPOConfig  # noqa: E402

HEADS = ("policy_head", "local_head", "target_head")


def corridor_obs_batch(env: PCBRouteEnv, agent, n: int = 8) -> torch.Tensor:
    """Collecte des états de corridor : rollouts gloutons depuis les pads."""
    obs_list = []
    for net_index in range(min(6, env.n_nets)):
        obs = env.reset(net_index)
        for step in range(40):
            obs_list.append(np.asarray(obs, dtype=np.float32))
            action = agent.select_action(obs, greedy=True)
            obs, _r, term, trunc, _i = env.step(action)
            if term or trunc:
                break
    take = obs_list[:: max(1, len(obs_list) // n)][:n]
    while len(take) < n:
        take.append(obs_list[-1])
    return torch.as_tensor(np.stack(take))


def mean_entropy(agent, batch: torch.Tensor) -> float:
    with torch.no_grad():
        logits, _ = agent.net(batch)
        dist = torch.distributions.Categorical(logits=logits)
        return float(dist.entropy().mean())


def main() -> int:
    src = sys.argv[1] if len(sys.argv) > 1 else \
        str(ROOT / "training/router/model_4l_c2_tp1.pt")
    dst = sys.argv[2] if len(sys.argv) > 2 else \
        str(ROOT / "training/router/model_4l_c2_soft.pt")
    target_h = float(sys.argv[3]) if len(sys.argv) > 3 else 0.8

    agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, arch="compass2")
    if not agent.load(src):
        raise FileNotFoundError(src)
    sd = agent.net.state_dict()
    head_keys = [k for k in sd if any(k.startswith(h + ".") for h in HEADS)]

    board, nets = make_realistic_board(46000, layer_count=4)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=46000))
    batch = corridor_obs_batch(env, agent)
    h0 = mean_entropy(agent, batch)
    print(f"entropie corridor initiale : {h0:.4f} ({len(head_keys)} tenseurs de tête)")

    alpha = 1.0
    best = None
    for attempt in range(9):
        alpha *= 0.5
        scaled = {k: (v * alpha if k in head_keys else v)
                  for k, v in sd.items()}
        agent.net.load_state_dict(scaled)
        h = mean_entropy(agent, batch)
        print(f"  alpha={alpha:.3f} -> entropie corridor {h:.4f}")
        best = (alpha, h, scaled)
        if h >= target_h:
            break
    agent.net.load_state_dict(best[2])
    agent.save(dst)
    print(f"checkpoint adouci -> {dst} (alpha={best[0]:.3f}, "
          f"entropie {best[1]:.3f}, cible {target_h})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
