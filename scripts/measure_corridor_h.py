#!/usr/bin/env python3
"""Mesure l'entropie de corridor d'un checkpoint (sans le modifier).

Réutilise la calibration de soften_heads.py : entropie moyenne sur des
états de CORRIDOR (rollouts gloutons depuis les pads), où plusieurs
actions sont légales — mesurer sur les pads de départ est biaisé
(souvent 1 seule action légale = entropie structurelle).

Usage : python3 measure_corridor_h.py <model.pt> [layers=4] [seed=46000]
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402

sys.path.insert(0, "/home/z/my-project/scripts")
from soften_heads import corridor_obs_batch, mean_entropy  # noqa: E402


def main() -> int:
    model = sys.argv[1]
    layers = int(sys.argv[2]) if len(sys.argv) > 2 else 4
    seed = int(sys.argv[3]) if len(sys.argv) > 3 else 46000

    agent = load_ppo_agent(model, in_channels=layers + 4)
    board, nets = make_realistic_board(seed, layer_count=layers)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    batch = corridor_obs_batch(env, agent, n=16)
    h = mean_entropy(agent, batch)
    print(f"H_corridor={h:.4f} ({Path(model).name}, layers={layers}, seed={seed})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
