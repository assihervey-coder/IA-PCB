#!/usr/bin/env python3
"""DAgger par téléportation 4L (Task 24) — ciblé sur les zones d'échec prouvées.

Diagnostic Task 24 (diag_prefix.py / trace_homing.py) :
  1. VIOPHOBIE — la politique, même posée PILE sur l'xy de la cible sur la
     mauvaise couche, ne pose jamais le via (GND : d=1 -> dérive à 172) ;
  2. CARREFOURS — au premier mauvais choix hors corridor (toutes directions
     libres, cible visible 23 cells au nord), la politique entre dans un
     attracteur dégénéré « tout droit » (SDA) et ne revient jamais.

Ce collecteur ne compte PAS sur les rollouts de la politique (qui passent
l'essentiel de leur budget dans les queues dégénérées) : il TÉLÉPORTE
l'agent le long du corridor A* de chaque net long et dense les états :

  * corridor (toutes les 2 cells)       -> étiquette = pas A* suivant (gratuit) ;
  * zone via (±8 cells avant chaque via) -> densifiée : c'est l'état
    « décision de via » que le BC sous-apprend (vias rares en démos) ;
  * zone cible (25 dernières cells)      -> homing final ;
  * carrefours (±3 cells autour de chaque changement de direction) ;
  * fenêtres de DÉVIATION : depuis les états critiques, la politique agit
    W=2 pas (distribution DE l'agent, esprit DAgger) et chaque état visité
    est étiqueté par l'expert A* depuis cet état (récupération).

Contexte identique aux démos/production : routes A* des nets précédents
enregistrées dans l'ordre route_all (nets courts d'abord), propres nets non
bloquants pour eux-mêmes. Format npz identique (mergeable, packbits).

Usage : python3 dagger_teleport_4l.py <out.npz> <model.pt> <première_cartes> <n_cartes> [min_leg]
Ex.   : python3 dagger_teleport_4l.py training/router/demos_tp_r0.npz \
            training/router/model_bc_4l.pt 44000 1 50
"""
import sys
import time
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import numpy as np  # noqa: E402
import torch  # noqa: E402

torch.set_num_threads(2)

from evaluation.bench import make_realistic_board  # noqa: E402
from src.agents.ppo_agent import PPOAgent, PPOConfig  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402
from training.router.imitation import (  # noqa: E402
    _label_legal,
    _pack_planes,
    _route_order,
    encode_expert_action,
    load_demos,
    save_demos,
)


def load_existing(path: Path) -> list[dict]:
    """Recharge un npz de démos en liste de dicts (reprise de collecte)."""
    if not path.is_file():
        return []
    data = load_demos(path)
    meta, planes = data["meta"], data["planes"]
    steps, actions, nets = data["steps"], data["actions"], data["nets"]
    demos: list[dict] = []
    for row in range(len(meta)):
        h, w, layers, step_off, n, plane_off, plane_bytes = (
            int(v) for v in meta[row]
        )
        bits = np.unpackbits(planes[plane_off: plane_off + plane_bytes])
        static = bits[: (layers + 2) * h * w].reshape(layers + 2, h, w)
        demos.append({
            "net": str(nets[row]), "layers": layers, "h": h, "w": w,
            "planes": np.packbits(np.ascontiguousarray(static)),
            "steps": [tuple(int(v) for v in r)
                      for r in steps[step_off: step_off + n]],
            "actions": [int(a) for a in actions[step_off: step_off + n]],
        })
    return demos

MIN_LEG = 50        # nets longs seulement (zone du cliff)
STRIDE = 2          # corridor : un état tous les STRIDE cells
VIA_RADIUS = 8      # ± cells autour de chaque décision de via
TARGET_ZONE = 25    # dernières cells avant la cible
JUNCT_RADIUS = 3    # ± cells autour de chaque carrefour
DEV_EVERY = 16      # fenêtre de déviation aussi tous les DEV_EVERY cells
DEV_STEPS = 2       # pas de politique par fenêtre de déviation
TIME_GUARD = 500    # secondes : arrêter la collecte du lot au-delà


def collect_teleport_dagger(model_path: str, board: dict, nets: list[dict],
                            clearance_cells: int = 1, seed: int = 0,
                            min_leg: int = MIN_LEG) -> list[dict]:
    """Collecte téléportée pour une carte. Retourne une démo par net traité."""
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=clearance_cells,
                                             seed=seed))
    agent = PPOAgent(PPOConfig(), in_channels=env.observation_shape[0],
                     n_actions=12, device="cpu")
    if not agent.load(model_path):
        raise FileNotFoundError(f"checkpoint illisible : {model_path}")

    demos: list[dict] = []
    for net_index in _route_order(env):
        # 1) Route A* enregistrée (contexte séquentiel de production) ;
        #    les décoys et courts nets ne produisent pas d'états.
        route = env.astar_route(net_index)
        if not route.get("completed"):
            continue
        if env._nets[net_index].name.startswith("DEC"):
            continue

        # 2) Première jambe (source -> pad restant le plus proche) : c'est
        #    le trajet que l'épisode doit finir.
        obs0 = env.reset(net_index)
        ep = env._episode
        if ep.start is None:
            continue
        source = (ep.x, ep.y, ep.layer)
        targets_all = list(ep.targets)
        if not targets_all:
            continue
        target = min(targets_all, key=lambda t: (env._pad_dist(source, t), t))
        leg = env._astar(net_index, source, target)
        if not leg or len(leg) - 1 < min_leg:
            continue
        L = len(leg)

        # 3) Index critiques : vias, carrefours, zone cible, pas réguliers.
        via_idx = {j - 1 for j in range(1, L) if leg[j][2] != leg[j - 1][2]}
        junct_idx = set()
        for j in range(1, L - 1):
            d1 = (leg[j][0] - leg[j - 1][0], leg[j][1] - leg[j - 1][1])
            d2 = (leg[j + 1][0] - leg[j][0], leg[j + 1][1] - leg[j][1])
            if d1 != d2:
                junct_idx.add(j)
        zone: set[int] = set()
        for j in via_idx:
            zone.update(range(max(0, j - VIA_RADIUS), min(L - 1, j + VIA_RADIUS) + 1))
        for j in junct_idx:
            zone.update(range(max(0, j - JUNCT_RADIUS), min(L - 1, j + JUNCT_RADIUS) + 1))
        zone.update(range(max(0, L - 1 - TARGET_ZONE), L - 1))
        zone.update(range(0, L - 1, DEV_EVERY))
        corridor = set(range(0, L - 1, STRIDE))
        idxs = sorted((corridor | zone) & set(range(L - 1)))  # jamais la cible
        critical = sorted((via_idx | junct_idx
                           | set(range(0, L - 1, DEV_EVERY))) & set(idxs))

        # 4) Étiquettes de corridor (gratuites : cellule suivante du leg).
        steps: list[tuple[int, int, int]] = []
        actions: list[int] = []
        labeled: set[tuple[int, int, int]] = set()
        for j in idxs:
            cell, nxt = leg[j], leg[j + 1]
            if cell in labeled:
                continue
            dx, dy, dl = (nxt[0] - cell[0], nxt[1] - cell[1], nxt[2] - cell[2])
            action = encode_expert_action(dx, dy, dl)
            if action is None or not _label_legal(env, net_index, cell, dx, dy, dl):
                continue
            steps.append(tuple(int(v) for v in cell))
            actions.append(int(action))
            labeled.add(cell)

        # 5) Fenêtres de déviation étiquetées A* (cache par cellule).
        label_cache: dict[tuple[int, int, int], int | None] = {}

        def expert_action(cell: tuple[int, int, int]) -> int | None:
            if cell in label_cache:
                return label_cache[cell]
            goal = min(targets_all, key=lambda t: (env._pad_dist(cell, t), t))
            action = None
            if cell != goal:
                path = env._astar(net_index, cell, goal)
                if path and len(path) >= 2:
                    dx = path[1][0] - cell[0]
                    dy = path[1][1] - cell[1]
                    dl = path[1][2] - cell[2]
                    cand = encode_expert_action(dx, dy, dl)
                    if cand is not None and _label_legal(env, net_index, cell,
                                                         dx, dy, dl):
                        action = cand
            label_cache[cell] = action
            return action

        for j in critical:
            env.reset(net_index)
            ep = env._episode
            ep.x, ep.y, ep.layer = (int(v) for v in leg[j])
            obs = env._observation()
            for _w in range(DEV_STEPS):
                cell = (ep.x, ep.y, ep.layer)
                if cell in labeled:
                    break  # déjà couvert par le corridor
                action_expert = expert_action(cell)
                if action_expert is None:
                    break  # hors de portée : on arrête la fenêtre
                steps.append(tuple(int(v) for v in cell))
                actions.append(int(action_expert))
                labeled.add(cell)
                act = agent.select_action(obs, greedy=True)
                obs, _r, term, trunc, _i = env.step(act)
                if term or trunc:
                    break

        if steps:
            demos.append({"net": f"{env._nets[net_index].name}#tp",
                          "layers": env.layer_count, "h": env.grid_h,
                          "w": env.grid_w, "planes": _pack_planes(obs0, env.layer_count),
                          "steps": steps, "actions": actions})
    return demos


def main() -> int:
    out = Path(sys.argv[1] if len(sys.argv) > 1 else
               ROOT / "training/router/demos_tp_r0.npz")
    model = sys.argv[2] if len(sys.argv) > 2 else \
        str(ROOT / "training/router/model_bc_4l.pt")
    first = int(sys.argv[3]) if len(sys.argv) > 3 else 44000
    count = int(sys.argv[4]) if len(sys.argv) > 4 else 1
    min_leg = int(sys.argv[5]) if len(sys.argv) > 5 else MIN_LEG

    demos = load_existing(out)  # reprise : les lots précédents sont conservés
    out.parent.mkdir(parents=True, exist_ok=True)
    t0 = time.monotonic()
    print(f"teleport-dagger: cartes {first}..{first + count - 1} "
          f"min_leg={min_leg} W={DEV_STEPS} (reprise: {len(demos)} démos)",
          flush=True)
    for k in range(count):
        if time.monotonic() - t0 > TIME_GUARD:
            print(f"teleport-dagger: garde temps atteinte, stop après "
                  f"{k} carte(s)", flush=True)
            break
        seed = first + k
        board, nets = make_realistic_board(seed, layer_count=4)
        got = collect_teleport_dagger(model, board, nets, clearance_cells=1,
                                      seed=seed, min_leg=min_leg)
        states = sum(len(d["actions"]) for d in got)
        demos.extend(got)
        save_demos(demos, out)
        print(f"teleport-dagger: carte {seed} -> +{len(got)} démos / "
              f"+{states} états (lot {len(demos)}, "
              f"{time.monotonic() - t0:.0f}s, sauvegardé)", flush=True)
    print(f"teleport-dagger: {len(demos)} démos -> {out} "
          f"({time.monotonic() - t0:.0f}s)", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
