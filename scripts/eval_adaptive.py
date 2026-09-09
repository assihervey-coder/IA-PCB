#!/usr/bin/env python3
"""Éval adaptative : distingue « trop long » de « perdu » (Task 26).

Protocole : UN rollout glouton par net au cap étendu cap = max(300, dist*3+50)
(dist = borne inférieure de la longueur de route, Manhattan pads les plus
proches, changement de couche = 1). La classification vient du nombre de pas :
  - ok    : complété en <= 300 pas          (identique au protocole historique)
  - long  : complété en <= cap étendu       (l'échec cap 300 était un artifact)
  - perdu : non complété même au cap étendu ; on journalise le ratio de la
            distance minimale à la cible atteinte (proche 1.0 = n'a jamais
            approché ; faible = a approché puis dérivé).

Usage : python3 eval_adaptive.py <model.pt> [layers] [seed ...]
Sortie : JSON stdout, résumé stderr. Ne modifie JAMAIS les fichiers.
"""
import json
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402

BASE_CAP = 300


def net_dist(env: PCBRouteEnv, net_index: int) -> int:
    """Borne inférieure de la route : min Manhattan(pads), dz = 1."""
    net = env._nets[net_index]
    cells = [env._pad_cell(p) for p in net.pads]
    best = 1 << 30
    for i, a in enumerate(cells):
        for b in cells[i + 1:]:
            d = abs(a[0] - b[0]) + abs(a[1] - b[1]) + (1 if a[2] != b[2] else 0)
            best = min(best, d)
    return best if best < (1 << 30) else 0


def adaptive_rollout(env: PCBRouteEnv, net_index: int, agent) -> dict:
    """Rollout glouton avec journalisation distance minimale à la cible."""
    obs = env.reset(net_index)
    init_dist = env._nearest_target_distance()
    min_dist = init_dist
    steps = 0
    info: dict = {}
    for steps in range(1, env.max_steps + 1):
        action = agent.select_action(obs, greedy=True)
        obs, _r, terminated, truncated, info = env.step(action)
        min_dist = min(min_dist, env._nearest_target_distance())
        if terminated or truncated:
            break
    # Sémantique legacy EXACTE : completed = TOUS les pads couverts, et la
    # géométrie réussie est enregistrée comme obstacle pour les nets suivants
    # (couplage séquentiel, comme eval_bc_4l.py via ppo_rollout).
    full = False
    if info.get("success"):
        route = env.path_to_route(net_index, env.episode_path)
        full = bool(route.get("completed"))
    return {"done": full, "steps": steps if full else -1, "min_dist": min_dist,
            "init_dist": init_dist}


def main() -> int:
    argv = [a for a in sys.argv[1:]]
    model = argv[0] if argv else str(ROOT / "training/router/model_bc_4l.pt")
    layers = int(argv[1]) if len(argv) > 1 else 4
    seeds = [int(s) for s in argv[2:]] or [46000, 46001]

    # Task 26 : auto-détection congestion (checkpoint layer_count+5 canaux)
    payload = torch.load(model, map_location="cpu", weights_only=False)
    in_ch = int(payload.get("in_channels") or (layers + 4))
    congestion = in_ch > layers + 4
    agent = load_ppo_agent(model, in_channels=in_ch)
    if congestion:
        print(f"[eval-adaptive] modèle congestion détecté ({in_ch} canaux)",
              file=sys.stderr, flush=True)
    report = {"model": Path(model).name, "layers": layers,
              "congestion": congestion, "boards": [],
              "nets": 0, "ok": 0, "long": 0, "lost": 0}
    for seed in seeds:
        board, nets = make_realistic_board(seed, layer_count=layers)
        env = PCBRouteEnv(board, nets,
                          EnvConfig(clearance_cells=1, seed=seed,
                                    congestion_channel=congestion))
        rows = []
        for net_index in range(env.n_nets):
            dist = net_dist(env, net_index)
            cap = max(BASE_CAP, dist * 3 + 50)
            env.max_steps = cap
            r = adaptive_rollout(env, net_index, agent)
            if r["done"] and r["steps"] <= BASE_CAP:
                outcome = "ok"
            elif r["done"]:
                outcome = "long"
            else:
                outcome = "lost"
            # NB : le couplage séquentiel (register) rend la comparaison par
            # ordre des nets STRICTEMENT identique au protocole historique.
            ratio = round(r["min_dist"] / max(1, r["init_dist"]), 3)
            rows.append({"net": net_index, "dist": dist, "cap": cap,
                         "outcome": outcome, "steps": r["steps"],
                         "min_dist_ratio": ratio})
            report["nets"] += 1
            report[outcome] += 1
        report["boards"].append({"seed": seed, "rows": rows})
        ok = sum(1 for x in rows if x["outcome"] == "ok")
        lo = sum(1 for x in rows if x["outcome"] == "long")
        ls = sum(1 for x in rows if x["outcome"] == "lost")
        print(f"seed {seed}: ok={ok}/{len(rows)} long=+{lo} perdu={ls} "
              f"(one-shot {100.0*ok/len(rows):.1f} %, accessible "
              f"{100.0*(ok+lo)/len(rows):.1f} %)", file=sys.stderr, flush=True)
    report["one_shot_pct"] = round(100.0 * report["ok"] / max(1, report["nets"]), 1)
    report["accessible_pct"] = round(100.0 * (report["ok"] + report["long"]) / max(1, report["nets"]), 1)
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
