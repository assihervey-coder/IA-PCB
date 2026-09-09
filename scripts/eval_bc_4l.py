#!/usr/bin/env python3
"""Évaluation one-shot d'un checkpoint BC 4 couches sur cartes NON vues.

Protocole (fixé au Task 20, réutilisable pour toute itération) :
  - cartes réalistes layer_count=4, seeds 46000+ (jamais vus à l'entraînement :
    les jeux d'entraînement sont 42000..42029 synthétique et 44000..44023
    réaliste) ;
  - leg1 one-shot : chaque net est routé par un rollout glouton de la
    politique, SANS repli A* — le pourcentage de complétion mesure la
    qualité intrinsèque de la politique ;
  - plafond de rollout 300 pas (une route qui réussit dépasse rarement
    300 pas ; sans plafond un net en échec brûle max(64, 4*(W+H)*couches)
    pas à 13,6 ms/forward et l'éval devient infranchissable — mesuré) ;
  - métriques secondaires : vias et longueur des routes complétées.

Usage : python3 eval_bc_4l.py <model.pt> [layers] [seed ...]
        layers = 4 (défaut, canaux = layers+4) ; ex. 2L : eval_bc_4l.py model.pt 2 46000
Conseil : UNE seed par invocation (~3-4 min/carte au premier plan).
Sortie : JSON sur stdout (parseable), résumé lisible sur stderr.
"""
import json
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

from evaluation.bench import make_realistic_board, load_ppo_agent, ppo_rollout  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402

ROLLOUT_CAP = 300

import torch  # noqa: E402
torch.set_num_threads(2)  # cgroup restreint : l'oversubscription ralentit les petits convnets


def main() -> int:
    model = sys.argv[1] if len(sys.argv) > 1 else str(ROOT / "training/router/model_bc_4l.pt")
    layers = int(sys.argv[2]) if len(sys.argv) > 2 else 4
    seeds = [int(s) for s in sys.argv[3:]] or [46000, 46001, 46002]

    # Auto-détection des canaux depuis le checkpoint (Task 26 : modèles
    # congestion layer_count+5) ; fallback layer_count+4 (historique).
    payload = torch.load(model, map_location="cpu", weights_only=False)
    in_ch = int(payload.get("in_channels") or (layers + 4))
    congestion = in_ch > layers + 4
    agent = load_ppo_agent(model, in_channels=in_ch)
    if congestion:
        print(f"[eval] modèle congestion détecté ({in_ch} canaux)", file=sys.stderr, flush=True)
    report = {"model": model, "layers": layers, "congestion": congestion,
              "boards": [], "total_nets": 0, "total_one_shot": 0}
    for seed in seeds:
        board, nets = make_realistic_board(seed, layer_count=layers)
        env = PCBRouteEnv(board, nets,
                          EnvConfig(clearance_cells=1, seed=seed,
                                    congestion_channel=congestion))
        env.max_steps = min(int(env.max_steps), ROLLOUT_CAP)
        one_shot = 0
        vias, lengths = [], []
        for net_index in range(env.n_nets):
            route = ppo_rollout(env, net_index, agent)
            if route.get("completed"):
                one_shot += 1
                vias.append(len(route.get("vias") or []))
                lengths.append(route.get("length_mm", 0.0))
        pct = 100.0 * one_shot / max(1, env.n_nets)
        report["boards"].append({
            "seed": seed,
            "nets": env.n_nets,
            "one_shot": one_shot,
            "pct": round(pct, 1),
            "mean_vias": round(sum(vias) / len(vias), 2) if vias else 0.0,
            "mean_length_mm": round(sum(lengths) / len(lengths), 2) if lengths else 0.0,
        })
        report["total_nets"] += env.n_nets
        report["total_one_shot"] += one_shot
        print(f"seed {seed}: {one_shot}/{env.n_nets} nets one-shot ({pct:.1f} %)",
              file=sys.stderr, flush=True)
    report["global_pct"] = round(100.0 * report["total_one_shot"] / max(1, report["total_nets"]), 1)
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
