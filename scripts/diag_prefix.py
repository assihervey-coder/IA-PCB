#!/usr/bin/env python3
"""Diagnostic « préfixe A* + suffixe politique » (Task 24).

Question : sur les nets longs (le cliff : 0 % one-shot au-delà de 60 cells),
à partir de QUEL point du corridor A* la politique sait-elle finir ?

Protocole par net (carte réaliste 4L, seed NON vue, cap 300 pas) :
  1. rollout glouton naturel (comme eval_bc_4l) + profil de distance à la
     cible (dist min atteinte, pas auquel elle est atteinte, dist finale) ;
  2. sur les nets en échec dont la première jambe A* fait >= 60 pas :
     rejouer un PRÉFIXE du chemin A* (fraction p du corridor, via des
     env.step officiels — pas, vias et budget consommés cohérents), puis
     confier le suffixe à la politique gloutonne sur un env neuf (carte
     propre). Fractions testées : 0.5, 0.75, 0.9.

Lecture des résultats :
  - échec même à p=0.9 -> homing terminal cassé ;
  - réussite à 0.75-0.9 mais échec à 0.5 -> la TRAVERSÉE du milieu est le
    problème (levier DAgger ciblé corridors / curriculum distance) ;
  - réussite dès 0.25 -> erreur composée précoce (fragilité générale).

Usage : python3 diag_prefix.py [model.pt] [seed] [layers]
Sortie : JSON stdout, résumé lisible stderr. ~3-5 min par carte.
"""
import json
import sys
import time
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import load_ppo_agent, make_realistic_board  # noqa: E402
from src.environment.action_space import LAYER_MODES, MOVES  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402

ROLLOUT_CAP = 300
PREFIX_FRACTIONS = (0.5, 0.75, 0.9)
MIN_LEG = 60          # seule la zone du cliff est diagnostiquée
MAX_PREFIX_NETS = 40  # budget : sous-échantillonnage déterministe au-delà


def action_for_step(dx: int, dy: int, dlayer: int) -> int:
    """Index d'action pour un pas A* (mouvement OU via, jamais les deux)."""
    if dlayer != 0:
        mode = LAYER_MODES.index(dlayer)
        return 0 * 3 + mode  # move (0,0) + mode via
    move = MOVES.index((dx, dy))
    return move * 3 + 0


def greedy_with_profile(env: PCBRouteEnv, net_index: int, agent) -> dict:
    """Rollout glouton instrumenté : résultat + profil de distance."""
    obs = env.reset(net_index)
    min_dist, min_step = None, 0
    steps = 0
    info = {}
    for _ in range(env.max_steps):
        action = agent.select_action(obs, greedy=True)
        obs, _r, terminated, truncated, info = env.step(action)
        steps += 1
        d = env._nearest_target_distance()
        if min_dist is None or d < min_dist:
            min_dist, min_step = d, steps
        if terminated or truncated:
            break
    return {
        "success": bool(info.get("success")),
        "steps": steps,
        "min_dist": int(min_dist) if min_dist is not None else 0,
        "min_step": int(min_step),
        "final_dist": int(env._nearest_target_distance()),
    }


def replay_prefix(env: PCBRouteEnv, leg: list, k: int) -> bool:
    """Rejoue leg[:k+1] (cellules) via env.step ; True si fidèle."""
    for (x0, y0, l0), (x1, y1, l1) in zip(leg[:k], leg[1:k + 1]):
        action = action_for_step(x1 - x0, y1 - y0, l1 - l0)
        _obs, _r, term, trunc, _i = env.step(action)
        if term or trunc:  # le préfixe ne doit jamais finir l'épisode
            return False
    ep = env._episode
    return (ep.x, ep.y, ep.layer) == leg[k]


def main() -> int:
    model = sys.argv[1] if len(sys.argv) > 1 else \
        str(ROOT / "training/router/model_bc_4l.pt")
    seed = int(sys.argv[2]) if len(sys.argv) > 2 else 46000
    layers = int(sys.argv[3]) if len(sys.argv) > 3 else 4

    agent = load_ppo_agent(model, in_channels=layers + 4)
    board, nets = make_realistic_board(seed, layer_count=layers)

    t0 = time.monotonic()
    report = {"model": model, "seed": seed, "layers": layers, "nets": []}
    n_one_shot = n_cliff = 0
    frac_success = {p: 0 for p in PREFIX_FRACTIONS}
    frac_total = {p: 0 for p in PREFIX_FRACTIONS}
    prefix_net_count = 0

    # Un env par net garantit des conditions identiques (carte propre,
    # aucune route enregistrée) entre one-shot naturel et tests préfixe.
    for net_index in range(len(nets)):
        env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
        env.max_steps = ROLLOUT_CAP
        natural = greedy_with_profile(env, net_index, agent)

        # Première jambe A* (source = pad de départ de l'env, cible = pad
        # restant le plus proche — mêmes règles que astar_route).
        env2 = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
        env2.reset(net_index)
        ep = env2._episode
        source = (ep.x, ep.y, ep.layer)
        targets = [c for c in ep.targets]
        if not targets:
            continue
        target = min(targets, key=lambda t: env2._pad_dist(source, t))
        span = env2._pad_dist(source, target)
        leg = env2._astar(net_index, source, target)
        leg_len = (len(leg) - 1) if leg else -1

        row = {
            "net": env2.net_names[net_index],
            "span": int(span),
            "leg_len": int(leg_len),
            "one_shot": natural["success"],
            "min_dist": natural["min_dist"],
            "min_step": natural["min_step"],
            "final_dist": natural["final_dist"],
            "prefix": {},
        }
        if natural["success"]:
            n_one_shot += 1
        elif leg is not None and leg_len >= MIN_LEG:
            n_cliff += 1
            if prefix_net_count < MAX_PREFIX_NETS and n_cliff % 2 == 1:
                prefix_net_count += 1
                for p in PREFIX_FRACTIONS:
                    k = int(round(p * leg_len))
                    if k < 1 or k >= len(leg):
                        continue
                    envp = PCBRouteEnv(
                        board, nets, EnvConfig(clearance_cells=1, seed=seed)
                    )
                    envp.max_steps = ROLLOUT_CAP
                    envp.reset(net_index)
                    if not replay_prefix(envp, leg, k):
                        row["prefix"][p] = {"error": "replay_mismatch"}
                        continue
                    # Suffixe glouton avec le budget restant.
                    obs = envp._observation()
                    info = {}
                    ok = False
                    for _ in range(envp.max_steps - k):
                        action = agent.select_action(obs, greedy=True)
                        obs, _r, term, trunc, info = envp.step(action)
                        if term or trunc:
                            ok = bool(info.get("success"))
                            break
                    row["prefix"][p] = {
                        "success": ok,
                        "start_dist": int(envp._nearest_target_distance()) if not ok else 0,
                        "suffix_steps": int(envp._episode.steps) - k,
                    }
                    frac_total[p] += 1
                    frac_success[p] += int(ok)
        report["nets"].append(row)
        if net_index % 20 == 0:
            print(f"[{net_index}/{len(nets)}] one-shot {n_one_shot} "
                  f"cliff-zone {n_cliff} ({time.monotonic() - t0:.0f}s)",
                  file=sys.stderr, flush=True)

    report["summary"] = {
        "nets_total": len(nets),
        "one_shot": n_one_shot,
        "one_shot_pct": round(100.0 * n_one_shot / max(1, len(nets)), 1),
        "cliff_zone_failed": n_cliff,
        "prefix_nets_tested": prefix_net_count,
        "prefix_success": {
            str(p): {
                "success": frac_success[p],
                "total": frac_total[p],
                "pct": round(100.0 * frac_success[p] / frac_total[p], 1)
                if frac_total[p] else None,
            }
            for p in PREFIX_FRACTIONS
        },
        "elapsed_s": round(time.monotonic() - t0, 1),
    }
    print(json.dumps(report, indent=2))
    s = report["summary"]
    print(f"\n=== seed {seed} : one-shot {n_one_shot}/{len(nets)} "
          f"({s['one_shot_pct']} %) | cliff-zone (jambe A* >= {MIN_LEG}) : "
          f"{n_cliff} échecs ===", file=sys.stderr)
    for p in PREFIX_FRACTIONS:
        tot = frac_total[p]
        if tot:
            print(f"  suffixe depuis préfixe A* p={p:.2f} : "
                  f"{frac_success[p]}/{tot} ({100.0 * frac_success[p] / tot:.1f} %)",
                  file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
