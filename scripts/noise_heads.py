#!/usr/bin/env python3
"""Injecte un bruit calibré dans les têtes de logits d'un checkpoint.

Pathologie Task 24 : un BC sur-convergé (entropie ~1e-8) est un puits pour
le PPO — le gradient de l'entropie est exactement nul sur une distribution
one-hot, donc le bonus d'entropie ne redémarre JAMAIS l'exploration. On
bruite les têtes qui produisent les logits (policy_head, local_head,
target_head — le tronc de features reste intact) jusqu'à atteindre une
entropie cible mesurée sur de vraies observations.

Usage : python3 noise_heads.py <in.pt> <out.pt> [entropie_cible=0.6]
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import numpy as np  # noqa: E402
import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import make_realistic_board, load_ppo_agent  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402
from src.agents.ppo_agent import PPOAgent, PPOConfig  # noqa: E402

HEADS = ("policy_head", "local_head", "target_head")


def mean_entropy(agent, obs_batch: torch.Tensor) -> float:
    with torch.no_grad():
        logits, _ = agent.net(obs_batch)
        dist = torch.distributions.Categorical(logits=logits)
        return float(dist.entropy().mean())


def main() -> int:
    src = sys.argv[1] if len(sys.argv) > 1 else \
        str(ROOT / "training/router/model_4l_c2_tp1.pt")
    dst = sys.argv[2] if len(sys.argv) > 2 else \
        str(ROOT / "training/router/model_4l_c2_noised.pt")
    target_h = float(sys.argv[3]) if len(sys.argv) > 3 else 0.6

    agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, arch="compass2")
    if not agent.load(src):
        raise FileNotFoundError(src)
    sd = agent.net.state_dict()
    head_keys = [k for k in sd if any(k.startswith(h + ".") for h in HEADS)]
    print("têtes bruitées :", sorted({k.split('.')[0] for k in head_keys}))

    # Batch d'observations réelles (4 nets d'une carte d'éval, état de départ).
    board, nets = make_realistic_board(46000, layer_count=4)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=46000))
    obs_list = [env.reset(i) for i in range(min(8, env.n_nets))]
    # formes hétérogènes -> on prend la première obs, répétée (batch 8)
    obs0 = np.asarray(obs_list[0], dtype=np.float32)
    batch = torch.as_tensor(obs0).unsqueeze(0).repeat(8, 1, 1, 1)
    h0 = mean_entropy(agent, batch)
    print(f"entropie initiale : {h0:.4f}")

    gen = torch.Generator().manual_seed(1234)
    sigma = 0.1
    for attempt in range(8):
        noisy = {k: v.clone() for k, v in sd.items()}
        for k in head_keys:
            noise = torch.normal(0.0, sigma, size=noisy[k].shape, generator=gen)
            noisy[k] = noisy[k] + noise
        agent.net.load_state_dict(noisy)
        h = mean_entropy(agent, batch)
        print(f"  sigma={sigma:.2f} -> entropie {h:.4f}")
        if h >= target_h * 0.7:
            break
        sigma *= 1.8
    agent.save(dst)
    print(f"checkpoint bruité -> {dst} (entropie {h:.3f}, cible {target_h})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
