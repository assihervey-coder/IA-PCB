#!/usr/bin/env python3
"""Imitation learning (behavioral cloning) du routeur : le RL apprend de A*.

L'expert deterministe :meth:`PCBRouteEnv.astar_route` joue chaque net sur la
carte ; chaque trajectoire (observation -> action experte) est enregistree
puis distillee dans le meme reseau acteur-critique que le PPO. Le checkpoint
produit est directement chargeable par ``src/service.py`` (meme format que
``train.py``), ce qui permet de mesurer l'ecart A* vs RL(BC) dans l'arene.

Pourquoi l'imitation ? Le PPO explore depuis zero : sur une vraie carte, sa
politique errante consomme des milliers de pas par net (infereince CPU hors
budget). En clonant A*, la politique suit des quasi-chemins optimaux des le
premier essai : le nombre de pas d'inference chute d'un ordre de grandeur et
le score se rapproche de l'expert.

Pipeline :
    1. dumper les entrees exactes d'une requete de routage reelle :
       YAHRIACAD_DUMP_ROUTE_INPUT=<dir> ai-server  (voir src/service.py)
    2. collecter les demonstrations :
       python3 imitation.py generate --input route_input.json \
           --out demos.npz --synthetic 8
    3. entrainer (cross-entropy sur les actions expertes) :
       python3 imitation.py train --demos demos.npz --out model_bc.pt
    4. evaluer hors-ligne puis en ligne (reload + arene) :
       python3 imitation.py eval --model model_bc.pt --input route_input.json
"""

from __future__ import annotations

import argparse
import json
import os
import random
import sys
import time
from pathlib import Path

import numpy as np

AI_ENGINE_ROOT = Path(__file__).resolve().parents[2]
REPO_ROOT = AI_ENGINE_ROOT.parents[0]

for _path in (str(AI_ENGINE_ROOT), str(REPO_ROOT / "shared" / "gen" / "python")):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from src.environment.action_space import LAYER_MODES, MOVES, N_MODES  # noqa: E402
from src.environment.pcb_env import (  # noqa: E402
    Cell,
    EnvConfig,
    PCBRouteEnv,
    position_plane,
)

# Actions de deplacement pur (dlayer = 0) : move_index * 3 + 0.
_MOVE_ACTION: dict[tuple[int, int], int] = {
    move: move_idx * N_MODES + LAYER_MODES.index(0) for move_idx, move in enumerate(MOVES)
}
# Actions de via pur : le composant move est ignore par env.step des que
# dlayer != 0 ; on encode donc les vias sur l'action "up + via-up/down".
_VIA_UP_ACTION = 0 * N_MODES + LAYER_MODES.index(1)
_VIA_DOWN_ACTION = 0 * N_MODES + LAYER_MODES.index(-1)


def encode_expert_action(dx: int, dy: int, dlayer: int) -> int | None:
    """Encode un pas A* ``(dx, dy, dlayer)`` en index d'action de l'env.

    Les voisins A* sont soit un deplacement 4-connexe pur (``dlayer == 0``),
    soit un changement de couche pur (``dx == dy == 0``) ; tout autre delta
    n'a pas d'action correspondante et renvoie ``None``.
    """
    if dlayer != 0:
        if dx != 0 or dy != 0:
            return None
        return _VIA_UP_ACTION if dlayer > 0 else _VIA_DOWN_ACTION
    return _MOVE_ACTION.get((dx, dy))


# ---------------------------------------------------------------------------
# Collecte de demonstrations
# ---------------------------------------------------------------------------


def _route_order(env: PCBRouteEnv) -> list[int]:
    """Ordre de routage de :meth:`route_all` (nets courts d'abord)."""
    return sorted(
        range(env.n_nets),
        key=lambda i: (env._net_span_cells(i), env._nets[i].name),
    )


def _label_legal(env: PCBRouteEnv, net_index: int, source: Cell,
                 dx: int, dy: int, dl: int) -> bool:
    """Vrai si le pas etiqueté est LEGAL vu de l'observation.

    L'A* force-libere la cellule but (``free[glayer, gy, gx] = True``) pour
    traverser les cas degeneres de pads empiles dont la cellule est dejà
    occupée par la route d'un autre net ; une telle etiquette enseigne au
    reseau une action que le masque d'actions interdit (loss infinie au
    training, action inatteignable en production — le repli A* la couvre).
    On filtre donc les etiquettes dont la cellule d'arrivee est bloquee
    dans le masque libre de l'environnement.
    """
    free = env._free_mask(net_index)
    tx, ty, tl = source[0] + dx, source[1] + dy, source[2] + dl
    if not (0 <= tl < env.layer_count and 0 <= tx < env.grid_w
            and 0 <= ty < env.grid_h):
        return False
    return bool(free[tl, ty, tx])


def _pack_planes(obs0, layer_count: int):
    """Pack les canaux statiques de l'observation (obstacles+source+cibles)."""
    static = obs0[: layer_count + 2] > 0.5
    return np.packbits(np.ascontiguousarray(static))


def collect_demos(
    board: dict,
    nets: list[dict],
    clearance_cells: int = 1,
    seed: int = 0,
    max_nets: int = 0,
    probes: int = 0,
    skip_decoys: bool = False,
) -> list[dict]:
    """Collecte une demonstration (trajectoire experte) par net routable.

    Les nets sont routes dans l'ordre de :meth:`route_all` : chaque route A*
    est enregistree comme obstacle pour les suivants, donc chaque
    demonstration voit exactement le contexte de congestion sequentiel du
    routeur de production. Pour chaque net, la premiere jambe de A*
    (depart = pad central, cible = pad le plus proche) est rejouee pas a pas
    dans l'environnement : l'observation affichee avant chaque action est
    celle qu'un rollout glouton de production recueillerais.

    Args:
        probes: pour chaque net, nombre de cellules libres aleatoires
            supplementaires etiquetees par le premier pas de leur propre
            chemin A* (le champ de reprise de l'expert). Sans elles, le BC
            ne voit que les etats de la trajectoire experte et une seule
            deviation hors-chemin compromet le rollout (erreurs composees).

    Returns:
        Liste de dicts ``{"net", "layers", "h", "w", "planes", "steps",
        "actions"}`` ou ``planes`` est la representation packbits des canaux
        statiques et ``steps[i] = (x, y, layer)`` l'etat AVANT ``actions[i]``.
        Les sondes forment une entree supplementaire nommee ``<net>#probe``.
    """
    rng = random.Random(seed if seed else 12345)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=clearance_cells, seed=seed))
    order = _route_order(env)
    if skip_decoys:
        # Route tout (les leurres congestionnent la carte comme en production)
        # mais ne prélève les démonstrations que sur les nets non-leurres :
        # l'ordre « courts d'abord » plaçait les leurres en tête, et la
        # troncature max_nets ne gardait qu'eux => 0 démonstration.
        demo_order = [i for i in order if env._nets[i].net_class != "decoy"]
        if max_nets and max_nets > 0:
            demo_order = demo_order[:max_nets]
    elif max_nets and max_nets > 0:
        demo_order = order[:max_nets]
    else:
        demo_order = order
    demo_set = set(demo_order)

    demos: list[dict] = []
    for net_index in order:
        route = env.astar_route(net_index)  # enregistre les obstacles progressifs
        if not route.get("completed"):
            continue
        if net_index not in demo_set:
            continue  # net leurre (ou au-delà du plafond) : routé mais pas de démo
        obs0 = env.reset(net_index)
        ep = env._episode
        if ep.start is None or not ep.targets:
            continue
        targets_all = list(ep.targets)  # intact : la marche retire le but atteint
        goal = min(targets_all, key=lambda t: (env._pad_dist(ep.start, t), t))
        path: list[Cell] | None = env._astar(net_index, ep.start, goal)
        if not path or len(path) < 2:
            continue
        planes = _pack_planes(obs0, env.layer_count)
        base = {
            "layers": env.layer_count,
            "h": env.grid_h,
            "w": env.grid_w,
            "planes": planes,
        }

        steps: list[tuple[int, int, int]] = []
        actions: list[int] = []
        success = False
        info: dict = {}
        for k in range(len(path) - 1):
            x1, y1, l1 = path[k]
            x2, y2, l2 = path[k + 1]
            action = encode_expert_action(x2 - x1, y2 - y1, l2 - l1)
            if action is None:  # jamais produit par A* ; garde-fou
                break
            if not _label_legal(env, net_index, (x1, y1, l1),
                                x2 - x1, y2 - y1, l2 - l1):
                break  # but conteste (pad empile) : arrete la demo ici
            steps.append((ep.x, ep.y, ep.layer))
            actions.append(action)
            _obs, _reward, terminated, truncated, info = env.step(action)
            if truncated:
                break
            if terminated:
                success = True
                break
        if not success or not actions:
            continue
        entry = {"net": env._nets[net_index].name, **base, "steps": steps,
                 "actions": actions}
        demos.append(entry)

        if probes > 0:
            free = env._free_mask(net_index)
            cells = np.argwhere(free)  # (N, 3) = (layer, y, x)
            if len(cells):
                chosen = rng.sample(range(len(cells)), min(probes, len(cells)))
                probe_steps: list[tuple[int, int, int]] = []
                probe_actions: list[int] = []
                for row in chosen:
                    layer, y, x = (int(v) for v in cells[row])
                    source = (x, y, layer)
                    goal_p = min(targets_all, key=lambda t: (env._pad_dist(source, t), t))
                    probe_path = env._astar(net_index, source, goal_p)
                    if not probe_path or len(probe_path) < 2:
                        continue
                    dx = probe_path[1][0] - source[0]
                    dy = probe_path[1][1] - source[1]
                    dl = probe_path[1][2] - source[2]
                    action = encode_expert_action(dx, dy, dl)
                    if action is None:
                        continue
                    if not _label_legal(env, net_index, source, dx, dy, dl):
                        continue
                    # Sur-echantillonnage des cas difficiles : quand l'expert
                    # evite un obstacle (action != direction naive vers la
                    # cible), l'etat est repete 3 fois pour contrebalancer la
                    # dominance des pas en ligne droite — sinon la politique
                    # apprend la direction et fonce dans les murs.
                    dxg = goal_p[0] - source[0]
                    dyg = goal_p[1] - source[1]
                    repeats = 1
                    if dl == 0 and (dxg or dyg):
                        if abs(dxg) >= abs(dyg) and dxg != 0:
                            naive = (1 if dxg > 0 else -1, 0)
                        else:
                            naive = (0, 1 if dyg > 0 else -1)
                        repeats = 3 if (dx, dy) != naive else 1
                    for _ in range(repeats):
                        probe_steps.append(source)
                        probe_actions.append(action)
                if probe_actions:
                    demos.append(
                        {"net": f"{env._nets[net_index].name}#probe", **base,
                         "steps": probe_steps, "actions": probe_actions}
                    )
    return demos


def collect_dagger(
    model_path: str,
    board: dict,
    nets: list[dict],
    clearance_cells: int = 1,
    seed: int = 0,
    max_states_per_net: int = 200,
    rollout_cap: int = 400,
) -> list[dict]:
    """Passe DAgger : etats visites par la politique BC, etiquetes par A*.

    Le BC seul ne voit que les etats de l'expert : des qu'il devie, il
    explore des etats jamais etiquetes et les erreurs se composent. Ici la
    politique BC roule chaque net (comme en production), puis l'expert A*
    etiquete le PREMIER pas correct depuis chaque cellule visitee — y
    compris les etats d'erreur et d'hesitation. Les routes enregistrees
    pour les nets suivants reproduisent le comportement de production
    (route BC si succes, sinon repli A*).
    """
    _require_torch()
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=clearance_cells, seed=seed))
    # Les rollouts BC en echec peuvent errer jusqu'a max_steps : on plafonne
    # la collecte (les etats d'erreur utiles sont dans les premiers pas).
    env.max_steps = min(int(env.max_steps), max(32, int(rollout_cap)))
    in_channels = env.observation_shape[0]
    agent = PPOAgent(PPOConfig(), in_channels=in_channels, n_actions=12, device="cpu")
    if not agent.load(model_path):
        raise FileNotFoundError(f"checkpoint illisible : {model_path}")

    demos: list[dict] = []
    for net_index in _route_order(env):
        route_bc = _greedy_rollout(env, net_index, agent)
        visited = list(env.episode_path)
        if route_bc is None or not route_bc.get("completed"):
            route = env.astar_route(net_index)  # repli de production
        else:
            route = route_bc  # deja enregistre par path_to_route(register=True)
        if not route.get("completed"):
            continue
        obs0 = env.reset(net_index)
        ep = env._episode
        if ep.start is None:
            continue
        targets_all = list(ep.targets)
        if not targets_all:
            continue
        planes = _pack_planes(obs0, env.layer_count)
        base = {"layers": env.layer_count, "h": env.grid_h, "w": env.grid_w,
                "planes": planes}

        seen: set[Cell] = set()
        steps: list[tuple[int, int, int]] = []
        actions: list[int] = []
        for cell in visited:
            if cell in seen or len(steps) >= max_states_per_net:
                continue
            seen.add(cell)
            goal = min(targets_all, key=lambda t: (env._pad_dist(cell, t), t))
            if cell == goal:
                continue
            path = env._astar(net_index, cell, goal)
            if not path or len(path) < 2:
                continue
            dx = path[1][0] - cell[0]
            dy = path[1][1] - cell[1]
            dl = path[1][2] - cell[2]
            action = encode_expert_action(dx, dy, dl)
            if action is None:
                continue
            if not _label_legal(env, net_index, cell, dx, dy, dl):
                continue  # cellule bloquee dans l'obs (pad empile) : pas d'etiquette
            steps.append(cell)
            actions.append(action)
        if steps:
            demos.append({"net": f"{env._nets[net_index].name}#dagger", **base,
                          "steps": steps, "actions": actions})
    return demos


def save_demos(demos: list[dict], path: str | Path) -> None:
    """Serialise les demonstrations en ``.npz`` sans pickle."""
    meta_rows: list[tuple] = []
    planes_parts: list = []
    steps_rows: list[tuple] = []
    actions_rows: list[int] = []
    names: list[str] = []
    plane_off = 0
    step_off = 0
    for demo in demos:
        planes = demo["planes"]
        meta_rows.append(
            (
                demo["h"],
                demo["w"],
                demo["layers"],
                step_off,
                len(demo["actions"]),
                plane_off,
                int(planes.nbytes),
            )
        )
        planes_parts.append(np.asarray(planes, dtype=np.uint8).ravel())
        steps_rows.extend(tuple(int(v) for v in step) for step in demo["steps"])
        actions_rows.extend(int(a) for a in demo["actions"])
        names.append(str(demo["net"])[:63])
        plane_off += int(planes.nbytes)
        step_off += len(demo["actions"])
    out = Path(path)
    out.parent.mkdir(parents=True, exist_ok=True)
    np.savez_compressed(
        out,
        meta=np.asarray(meta_rows, dtype=np.int64).reshape(-1, 7),
        planes=np.concatenate(planes_parts) if planes_parts else np.zeros(0, dtype=np.uint8),
        steps=np.asarray(steps_rows, dtype=np.int32).reshape(-1, 3),
        actions=np.asarray(actions_rows, dtype=np.int8),
        nets=np.asarray(names, dtype="<U64"),
    )


def load_demos(path: str | Path) -> dict:
    """Recharge un ``.npz`` de demonstrations (numpy pur, sans pickle)."""
    with np.load(str(path), allow_pickle=False) as data:
        return {key: data[key] for key in data.files}


def merge_demos(paths: list[str | Path], out_path: str | Path) -> int:
    """Concatene plusieurs ``.npz`` de demonstrations en un seul.

    Permet de fractionner la collecte (cartes realistes lentes) sur
    plusieurs sessions tout en gardant un unique jeu d'entrainement.
    """
    demos: list[dict] = []
    total = 0
    for path in paths:
        data = load_demos(path)
        meta, planes = data["meta"], data["planes"]
        steps, actions, nets = data["steps"], data["actions"], data["nets"]
        for row in range(len(meta)):
            h, w, layers, step_off, n, plane_off, plane_bytes = (
                int(v) for v in meta[row]
            )
            bits = np.unpackbits(planes[plane_off : plane_off + plane_bytes])
            static = bits[: (layers + 2) * h * w].reshape(layers + 2, h, w)
            demos.append(
                {
                    "net": str(nets[row]),
                    "layers": layers,
                    "h": h,
                    "w": w,
                    "planes": np.packbits(np.ascontiguousarray(static)),
                    "steps": [tuple(int(v) for v in r) for r in steps[step_off : step_off + n]],
                    "actions": [int(a) for a in actions[step_off : step_off + n]],
                }
            )
            total += n
    save_demos(demos, out_path)
    print(
        f"merge: {len(demos)} demonstrations / {total} pas depuis "
        f"{[str(p) for p in paths]} -> {out_path}",
        flush=True,
    )
    return total


# ---------------------------------------------------------------------------
# Jeu de donnees torch
# ---------------------------------------------------------------------------


def _require_torch():
    try:
        import torch  # noqa: F401
    except Exception:
        sys.exit(
            "torch est requis pour l'entrainement BC (la collecte n'en a PAS besoin).\n"
            "  pip install torch --index-url https://download.pytorch.org/whl/cpu"
        )
    return torch


def _reconstruct_obs(batch_meta, planes, steps, demo_id: int, step_row: int):
    """Reconstruit l'observation float32 ``(C, H, W)`` d'un pas de demo.

    Canaux statiques (obstacles, source, cibles) depuis les plans packbits,
    indicateur de couche et position de l'agent depuis l'etat du pas.
    """
    h, w, layers, _step_off, _n, plane_off, plane_bytes = (
        int(v) for v in batch_meta[demo_id]
    )
    bits = np.unpackbits(planes[plane_off : plane_off + plane_bytes])
    static = bits[: (layers + 2) * h * w].reshape(layers + 2, h, w)
    x, y, layer = (int(v) for v in steps[step_row])
    obs = np.zeros((layers + 4, h, w), dtype=np.float32)
    obs[:layers] = static[:layers]
    obs[layers] = static[layers]
    obs[layers + 1] = static[layers + 1]
    obs[layers + 2, :, :] = layer / max(1, layers - 1)
    obs[layers + 3] = position_plane(h, w, x, y)
    return obs


def build_torch_dataset(demos_data: dict):
    """Construit un ``Dataset`` torch ``(obs, action)`` depuis un npz charge."""
    _require_torch()  # verifie torch avant d'importer Dataset
    from torch.utils.data import Dataset

    meta = demos_data["meta"]
    planes = demos_data["planes"]
    steps = demos_data["steps"]
    actions = demos_data["actions"]

    index: list[tuple[int, int]] = []
    for demo_id, row in enumerate(meta):
        offset, count = int(row[3]), int(row[4])
        for k in range(count):
            index.append((demo_id, offset + k))

    class _BCDataset(Dataset):
        def __init__(self) -> None:
            self.index = index

        def __len__(self) -> int:
            return len(self.index)

        def __getitem__(self, idx: int):
            demo_id, step_row = self.index[idx]
            obs = _reconstruct_obs(meta, planes, steps, demo_id, step_row)
            return obs, int(actions[step_row])

    return _BCDataset()


# ---------------------------------------------------------------------------
# Entrainement (cross-entropy sur les actions expertes)
# ---------------------------------------------------------------------------


def train_bc(
    demos_path: str,
    out_path: str,
    epochs: int = 3,
    batch_size: int = 32,
    lr: float = 1e-3,
    val_frac: float = 0.05,
    seed: int = 0,
    init_from: str | None = None,
    time_budget_s: float = 0.0,
    arch: str = "base",
    via_weight: float | None = None,
) -> dict:
    """Entraine le clone comportemental et sauve un checkpoint compatible PPO.

    Le reseau est exactement celui du PPO (:class:`PPOAgent`) : le checkpoint
    produit se charge dans le service sans aucune difference de format, ce
    qui permet de comparer PPO pur et BC dans l'arene a modelisation egale.

    Returns:
        Dict de stats ``{"samples", "epochs_run", "accuracy", "class_accuracy"}``.
    """
    torch = _require_torch()
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    torch.set_num_threads(max(1, os.cpu_count() or 1))
    torch.manual_seed(seed)

    data = load_demos(demos_path)
    meta = data["meta"]
    layers = int(meta[0][2]) + 4  # canaux d'observation = couches + 4
    dataset = build_torch_dataset(data)

    # Split train/validation PAR demo (pas par pas : evite la fuite).
    rng = random.Random(seed)
    demo_ids = list(range(len(meta)))
    rng.shuffle(demo_ids)
    n_val = max(1, int(len(demo_ids) * val_frac)) if len(demo_ids) > 1 else 0
    val_ids = set(demo_ids[:n_val])
    train_idx = [i for i, (d, _s) in enumerate(dataset.index) if d not in val_ids]
    val_idx = [i for i, (d, _s) in enumerate(dataset.index) if d in val_ids]

    agent = PPOAgent(PPOConfig(), in_channels=layers, n_actions=12, device="cpu",
                     arch=str(arch or "base"))
    if init_from:
        follow = str(arch or "base") == "base"
        if not agent.load(init_from, follow_arch=follow):
            print(f"[warn] checkpoint initial illisible : {init_from}", flush=True)
        else:
            print(f"[init-from] {init_from} (archi={agent.arch}, "
                  f"{'strict/suivi' if follow else 'partiel/transfert'})", flush=True)
    agent.net.train()

    optimizer = torch.optim.Adam(agent.net.parameters(), lr=lr)
    # Cross-entropy PONDEREE par classe : sur les vraies cartes, ~7 % des pas
    # experts sont des vias mais les deplacements dominent le jeu de donnees
    # (>90 %) ; sans ponderation, la politique n'apprend JAMAIS a poser de
    # via (accuracy via ~0.00-0.08) et diverge au premier changement de
    # couche. Poids inverse-sqrt des frequences, normalises a moyenne 1.
    counts = np.bincount(
        [int(data["actions"][dataset.index[i][1]]) for i in train_idx],
        minlength=12,
    ).astype(np.float64)
    weights = 1.0 / np.sqrt(np.maximum(counts, 1.0))
    weights = weights / weights.mean()
    if via_weight is not None and via_weight > 0:
        # Task 25 : MULTIPLICATEUR des classes via au-dessus des ratios
        # inverse-sqrt (moyenne calculee sur les classes AVEC echantillons :
        # la CE ponderee est invariante par echelle globale, seuls les
        # RATIOS comptent — les 6 classes fantomes (compte 0) ne changeaient
        # rien a la perte, juste aux chiffres imprimes). F=1 reproduit les
        # ratios historiques (via:mouvement ~x4-5, via-acc 0.00-0.08) ;
        # F=2-3 double/triple l'emphase via sans basculer l'argmax de Bayes
        # sur via aux etats generiques (p_via ~2,6 % x F vs masse des
        # mouvements) — mesure : F=35 en poids final => politique "via
        # partout" (via-acc 0.76, mouvements 0.00) des la passe 1.
        real = counts > 0
        base = 1.0 / np.sqrt(np.maximum(counts, 1.0))
        base = base / base[real].mean()
        weights = np.where(real, base, 1.0)
        weights[_VIA_UP_ACTION] *= via_weight
        weights[_VIA_DOWN_ACTION] *= via_weight
    loss_fn = torch.nn.CrossEntropyLoss(
        weight=torch.as_tensor(weights, dtype=torch.float32)
    )
    print(
        "bc: poids de classe "
        f"{ {ActionLabel(k): round(float(v), 2) for k, v in enumerate(weights) if counts[k]} }",
        flush=True,
    )

    started = time.monotonic()
    epochs_run = 0

    def batches(idx_list: list[int], shuffle: bool):
        order = list(idx_list)
        if shuffle:
            rng.shuffle(order)
        # Grouper par forme d'observation : le jeu melange de vraies cartes
        # (grilles grandes) et du curriculum synthetique (grilles petites),
        # et np.stack exige des formes identiques au sein d'un batch.
        groups: dict[tuple[int, int], list[int]] = {}
        for i in order:
            demo_id, _step_row = dataset.index[i]
            shape = (int(meta[demo_id][0]), int(meta[demo_id][1]))
            groups.setdefault(shape, []).append(i)
        shapes = list(groups)
        if shuffle:
            rng.shuffle(shapes)
        for shape in shapes:
            chunk_list = groups[shape]
            if shuffle:
                rng.shuffle(chunk_list)
            for start in range(0, len(chunk_list), batch_size):
                chunk = chunk_list[start : start + batch_size]
                obs_batch = np.stack(
                    [_reconstruct_obs(meta, data["planes"], data["steps"],
                                      dataset.index[i][0], dataset.index[i][1])
                     for i in chunk]
                )
                act_batch = np.asarray(
                    [int(data["actions"][dataset.index[i][1]]) for i in chunk]
                )
                yield (
                    torch.as_tensor(obs_batch, dtype=torch.float32),
                    torch.as_tensor(act_batch, dtype=torch.int64),
                )

    def accuracy(idx_list: list[int]) -> tuple[float, dict[int, float]]:
        agent.net.eval()
        correct = 0
        total = 0
        per_class: dict[int, list[int]] = {}
        with torch.no_grad():
            for obs_batch, act_batch in batches(idx_list, shuffle=False):
                logits, _value = agent.net(obs_batch)
                preds = logits.argmax(dim=-1)
                matches = (preds == act_batch).tolist()
                for gold, ok in zip(act_batch.tolist(), matches, strict=True):
                    total += 1
                    correct += int(ok)
                    slot = per_class.setdefault(gold, [0, 0])
                    slot[0] += int(ok)
                    slot[1] += 1
        agent.net.train()
        class_acc = {k: v[0] / v[1] for k, v in sorted(per_class.items()) if v[1]}
        return (correct / total if total else 0.0), class_acc

    print(
        f"bc: {len(train_idx)} pas d'entrainement / {len(val_idx)} validation "
        f"({len(meta)} demonstrations, {layers} canaux)",
        flush=True,
    )
    for epoch in range(1, max(1, epochs) + 1):
        running = 0.0
        count = 0
        for obs_batch, act_batch in batches(train_idx, shuffle=True):
            logits, _value = agent.net(obs_batch)
            loss = loss_fn(logits, act_batch)
            if not torch.isfinite(loss):
                # Etiquette contree residuelle (action experte masquee) :
                # batch indisponible, on passe sans casser la moyenne.
                optimizer.zero_grad()
                continue
            optimizer.zero_grad()
            loss.backward()
            torch.nn.utils.clip_grad_norm_(agent.net.parameters(), 5.0)
            optimizer.step()
            running += float(loss.item())
            count += 1
            if count % 25 == 0:
                print(f"bc: epoch {epoch} update {count} loss {running / count:.4f}", flush=True)
            if time_budget_s > 0 and time.monotonic() - started > time_budget_s:
                print(
                    f"bc: budget temps atteint ({time_budget_s:.0f}s) -> arret apres "
                    f"{epochs_run + 1} epoch(s)",
                    flush=True,
                )
                break
        epochs_run = epoch
        over_budget = time_budget_s > 0 and time.monotonic() - started > time_budget_s
        val_acc, _ = accuracy(val_idx) if val_idx and not over_budget else (float("nan"), {})
        print(
            f"bc: epoch {epoch}/{epochs} loss {running / max(1, count):.4f} "
            f"val_accuracy {val_acc:.3f}",
            flush=True,
        )
        if over_budget:
            break

    train_acc, class_acc = accuracy(train_idx) if len(train_idx) <= 4000 else accuracy(
        rng.sample(sorted(train_idx), 4000)
    )
    out = Path(out_path)
    out.parent.mkdir(parents=True, exist_ok=True)
    if not agent.save(str(out)):
        raise RuntimeError(f"echec de la sauvegarde du checkpoint {out}")
    print(
        f"bc: checkpoint {out} - accuracy train {train_acc:.3f} "
        f"({len(train_idx)} pas, {epochs_run} epoch(s))",
        flush=True,
    )
    pretty = {ActionLabel(k): f"{v:.2f}" for k, v in class_acc.items()}
    print(f"bc: accuracy par action {pretty}", flush=True)
    return {
        "samples": len(train_idx),
        "epochs_run": epochs_run,
        "accuracy": train_acc,
        "class_accuracy": class_acc,
    }


def ActionLabel(action: int) -> str:  # noqa: N802 - libelle lisible pour le log
    from src.environment.action_space import ActionSpace

    return ActionSpace().name(action)


# ---------------------------------------------------------------------------
# Evaluation hors-ligne : A* vs BC sur la meme carte
# ---------------------------------------------------------------------------


def _greedy_rollout(env: PCBRouteEnv, net_index: int, agent) -> dict | None:
    """Rollout glouton, copie conforme de ``service._rl_route``."""
    obs = env.reset(net_index)
    info: dict = {}
    for _ in range(env.max_steps):
        action = agent.select_action(obs, greedy=True)
        obs, _reward, terminated, truncated, info = env.step(action)
        if terminated or truncated:
            break
    if not info.get("success"):
        return None
    return env.path_to_route(net_index, env.episode_path)


def hybrid_rl_route(env: PCBRouteEnv, net_index: int, agent,
                    step_budget: int | None = None) -> dict | None:
    """Strategie hybride : RL pour la premiere jambe, A* pour la suite.

    L'episode RL se termine au PREMIER pad atteint : sur une vraie carte ou
    50/52 nets sont multi-pads (4-52 pads), un rollout mono-jambe ne peut
    STRUCTURELLEMENT pas completer un net — l'eval ``completed`` mesurait un
    plafond d'architecture, pas une competence de politique. Ici la jambe
    RL (depart -> pad le plus proche) est chaine avec les jambes A* vers les
    pads restants, exactement comme :meth:`astar_route` chaine les siennes.

    Returns:
        Route dictionnaire complet (``completed`` True quand tous les pads
        sont connectes), ou ``None`` si la jambe RL elle-meme echoue (appelant
        : repli A* integral).
    """
    obs = env.reset(net_index)
    ep = env._episode
    if ep.start is None or not ep.targets:
        return None
    if step_budget is not None:
        budget = int(step_budget)
    else:
        # Parite production : ~4x la distance manhattan optimale (+96).
        dist = min(env._pad_dist(ep.start, t) for t in ep.targets)
        budget = min(env.max_steps, 4 * dist + 96)
    info: dict = {}
    for _ in range(budget):
        action = agent.select_action(obs, greedy=True)
        obs, _reward, terminated, truncated, info = env.step(action)
        if terminated or truncated:
            break
    if not info.get("success"):
        return None

    net = env._nets[net_index]
    leg1_cells = list(env.episode_path)
    env.path_to_route(net_index, leg1_cells)  # enregistre (cells propres libres)
    connected = {leg1_cells[0], leg1_cells[-1]}
    remaining = [c for c in pad_cells_of(env, net_index) if c not in connected]

    all_cells = list(leg1_cells)
    completed = True
    while remaining:
        target = min(
            remaining,
            key=lambda t: (min(env._pad_dist(t, c) for c in connected), t),
        )
        source = min(connected, key=lambda c: (env._pad_dist(c, target), c))
        leg = env._astar(net_index, source, target)
        if leg is None:
            completed = False
            break
        leg_cells = list(leg)
        bucket = env._routes_cells.setdefault(net.name, set())
        bucket.update(leg_cells)
        all_cells.extend(leg_cells[1:])
        connected.add(target)
        remaining.remove(target)

    # Geometrie fusionnee : jambe RL + jambes A* (runs colineaires fondus).
    segments, vias = env._cells_to_geometry(all_cells, net.min_track_width_mm)
    route = {
        "net": net.name,
        "segments": segments,
        "vias": vias,
        "length_mm": env._segments_length_mm(segments),
        "completed": completed,
    }
    env._net_routes[net.name] = route
    return route


def pad_cells_of(env: PCBRouteEnv, net_index: int) -> list[Cell]:
    """Cells uniques des pads du net (ordre de declaration)."""
    cells: list[Cell] = []
    seen: set[Cell] = set()
    for pad in env._nets[net_index].pads:
        cell = env._pad_cell(pad)
        if cell not in seen:
            seen.add(cell)
            cells.append(cell)
    return cells


def evaluate_offline(
    model_path: str,
    board: dict,
    nets: list[dict],
    clearance_cells: int = 1,
    seed: int = 0,
    max_nets: int = 0,
    rollout_cap: int = 0,
    mode: str = "leg1",
) -> dict:
    """Compare A* et BC sur la meme carte avec deux environnements separes.

    Args:
        rollout_cap: plafond de pas pour les rollouts BC (0 = max_steps de
            l'env). Un cap evite des minutes d'errance par net en echec ;
            400 represente deja ~2.6x le plus long chemin expert.
        mode: ``pure`` = rollout glouton integral (completude mono-jambe,
            plafonnee structurellement sur nets multi-pads) ; ``leg1``
            (defaut, fidelite production) = jambe RL puis chainage A* des
            pads restants, ce qui mesure la contribution reelle de la
            politique sur des cartes multi-pads.

    Returns:
        Dict avec, par strategy : completion, longueur, vias, temps ; et les
        totaux ``bc_with_fallback`` correspondant au comportement de
        production (repli A* net par net en cas d'echec BC).
    """
    _require_torch()
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    env_star = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=clearance_cells, seed=seed))
    env_bc = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=clearance_cells, seed=seed))
    if rollout_cap > 0:
        env_bc.max_steps = min(int(env_bc.max_steps), int(rollout_cap))
    in_channels = env_bc.observation_shape[0]
    agent = PPOAgent(PPOConfig(), in_channels=in_channels, n_actions=12, device="cpu")
    if not agent.load(model_path):
        raise FileNotFoundError(f"checkpoint illisible : {model_path}")

    order = _route_order(env_star)
    if max_nets and max_nets > 0:
        order = order[:max_nets]

    rows = []
    for net_index in order:
        t0 = time.monotonic()
        route_star = env_star.astar_route(net_index)
        t_star = time.monotonic() - t0
        t0 = time.monotonic()
        if mode == "leg1":
            route_bc = hybrid_rl_route(env_bc, net_index, agent)
        else:
            route_bc = _greedy_rollout(env_bc, net_index, agent)
        t_bc = time.monotonic() - t0
        if route_bc is None or not route_bc.get("completed"):
            # FIDELITE PRODUCTION : en cas d'echec BC, la route A* de repli
            # est ENREGISTREE sur env_bc (comme le fait service._rl_route et
            # collect_dagger). Sans cela, les nets suivants roulent sur une
            # carte trouee, hors de la distribution d'entrainement — l'eval
            # sous-estimait severement la politique (2/52 au lieu de ~5/10
            # sur les premiers nets).
            env_bc.astar_route(net_index)
        if route_bc is None:
            route_bc = {"net": env_bc.net_names[net_index], "segments": [], "vias": [],
                        "length_mm": 0.0, "completed": False}
        rows.append({"net": env_star.net_names[net_index], "star": route_star,
                     "bc": route_bc, "t_star": t_star, "t_bc": t_bc})

    def _totals(key: str) -> dict:
        completed = sum(1 for r in rows if r[key].get("completed"))
        length = sum(r[key].get("length_mm", 0.0) for r in rows)
        vias = sum(len(r[key].get("vias") or []) for r in rows)
        return {"completed": completed, "length_mm": round(length, 1), "vias": vias}

    fallback_rows = []
    for r in rows:
        best = r["bc"] if r["bc"].get("completed") else r["star"]
        fallback_rows.append(best)
    fb_completed = sum(1 for r in fallback_rows if r.get("completed"))
    fb_length = sum(r.get("length_mm", 0.0) for r in fallback_rows)
    fb_vias = sum(len(r.get("vias") or []) for r in fallback_rows)

    return {
        "nets": len(rows),
        "mode": mode,
        "astar": {**_totals("star"), "time_s": round(sum(r["t_star"] for r in rows), 2)},
        "bc": {**_totals("bc"), "time_s": round(sum(r["t_bc"] for r in rows), 2)},
        "bc_with_fallback": {
            "completed": fb_completed,
            "length_mm": round(fb_length, 1),
            "vias": fb_vias,
        },
        "rows": [
            {
                "net": r["net"],
                "astar_mm": round(r["star"].get("length_mm", 0.0), 1),
                "bc_mm": round(r["bc"].get("length_mm", 0.0), 1),
                "bc_ok": bool(r["bc"].get("completed")),
            }
            for r in rows
        ],
    }


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _load_route_input(path: str) -> tuple[dict, list[dict]]:
    payload = json.loads(Path(path).read_text(encoding="utf-8"))
    return payload["board"], payload["nets"]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Imitation learning (behavioral cloning) du routeur YahriaCad"
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_gen = sub.add_parser("generate", help="collecte les demonstrations de A*")
    p_gen.add_argument("--input", help="route_input.json dumpy par le moteur IA")
    p_gen.add_argument("--out", default="training/router/demos.npz")
    p_gen.add_argument("--synthetic", type=int, default=0,
                       help="ajoute N cartes synthetiques au curriculum")
    p_gen.add_argument("--synthetic-real", type=int, default=0,
                       help="ajoute N cartes REALISTES (0,25 mm, 70-115 x 50-85 mm, "
                            "congestion par leurres) au curriculum")
    p_gen.add_argument("--synthetic-seed", type=int, default=42)
    p_gen.add_argument("--layers", type=int, default=2,
                       help="nombre de couches cuivre des cartes synthetiques "
                            "(2 par defaut ; 4+ => curriculum multi-couches)")
    p_gen.add_argument("--max-nets", type=int, default=0,
                       help="plafonne le nombre de nets par carte (0 = tous)")
    p_gen.add_argument("--probes", type=int, default=0,
                       help="cellules-sondes etiquetees par net (champ de reprise A*)")
    p_gen.add_argument("--probes-real", type=int, default=None,
                       help="sondes pour la carte reelle seule (defaut = --probes)")
    p_gen.add_argument("--clearance", type=int, default=1)

    p_train = sub.add_parser("train", help="entraine le clone comportemental")
    p_train.add_argument("--demos", default="training/router/demos.npz")
    p_train.add_argument("--out", default="training/router/model_bc.pt")
    p_train.add_argument("--epochs", type=int, default=3)
    p_train.add_argument("--batch", type=int, default=32)
    p_train.add_argument("--lr", type=float, default=1e-3)
    p_train.add_argument("--seed", type=int, default=0)
    p_train.add_argument("--init-from", default=None,
                         help="checkpoint de depart (reprise / fine-tune)")
    p_train.add_argument("--arch", choices=["base", "compass", "wide", "compass2"],
                         default="base",
                         help="architecture du reseau : base = CNN historique "
                              "(compat checkpoints existants) ; compass = "
                              "boussole cible (2 canaux dx/dy + tete 1x1) ; "
                              "wide = tronc elargi 128 canaux. Une archi "
                              "different du checkpoint --init-from declenche "
                              "un chargement PARTIEL")
    p_train.add_argument("--time-budget", type=float, default=0.0,
                         help="budget temps en secondes (0 = illimite)")
    p_train.add_argument("--via-weight", type=float, default=None,
                         help="multiplicateur des classes via dans la perte "
                              "(F=1 = ratios historiques inverse-sqrt ; "
                              "Task 25 : 2-3 conseille en passes escaladees ; "
                              "au-dela la CE ponderee fait de via l'argmax "
                              "partout — mesure F=35)")

    p_eval = sub.add_parser("eval", help="compare A* et BC hors-ligne")
    p_eval.add_argument("--model", default="training/router/model_bc.pt")
    p_eval.add_argument("--input", help="route_input.json (carte a evaluer)")
    p_eval.add_argument("--max-nets", type=int, default=0)
    p_eval.add_argument("--rollout-cap", type=int, default=0,
                        help="plafond de pas par rollout BC (0 = max_steps)")
    p_eval.add_argument("--mode", choices=["leg1", "pure"], default="leg1",
                        help="leg1 = jambe RL + chainage A* (fidelite production) ; "
                             "pure = rollout glouton integral")
    p_eval.add_argument("--clearance", type=int, default=1)

    p_merge = sub.add_parser("merge", help="concatene plusieurs npz de demonstrations")
    p_merge.add_argument("--inputs", nargs="+", required=True)
    p_merge.add_argument("--out", required=True)

    args = parser.parse_args(argv)

    if args.command == "generate":
        probes_real = args.probes if args.probes_real is None else args.probes_real
        demos: list[dict] = []
        if args.input:
            board, nets = _load_route_input(args.input)
            got = collect_demos(board, nets, clearance_cells=args.clearance,
                                max_nets=args.max_nets, probes=probes_real)
            print(f"generate: {len(got)} demonstrations depuis {args.input}", flush=True)
            demos.extend(got)
        if args.synthetic > 0:
            from evaluation.bench import make_synthetic_board

            for k in range(args.synthetic):
                board, nets = make_synthetic_board(args.synthetic_seed * 1000 + k,
                                                   layer_count=args.layers)
                got = collect_demos(board, nets, clearance_cells=args.clearance,
                                    max_nets=args.max_nets, probes=args.probes)
                demos.extend(got)
            print(f"generate: +{args.synthetic} cartes synthetiques "
                  f"(total {len(demos)} demonstrations)", flush=True)
        if getattr(args, "synthetic_real", 0) > 0:
            from evaluation.bench import make_realistic_board

            for k in range(args.synthetic_real):
                board, nets = make_realistic_board(args.synthetic_seed * 1000 + k,
                                                   layer_count=args.layers)
                got = collect_demos(board, nets, clearance_cells=args.clearance,
                                    max_nets=args.max_nets, probes=args.probes,
                                    skip_decoys=True)
                demos.extend(got)
            print(f"generate: +{args.synthetic_real} cartes realistes 0,25 mm "
                  f"(total {len(demos)} demonstrations)", flush=True)
        if not demos:
            print("generate: aucune demonstration collectee", flush=True)
            return 1
        steps_total = sum(len(d["actions"]) for d in demos)
        save_demos(demos, args.out)
        print(f"generate: {len(demos)} demonstrations / {steps_total} pas -> {args.out}",
              flush=True)
        return 0

    if args.command == "merge":
        merge_demos(args.inputs, args.out)
        return 0

    if args.command == "train":
        train_bc(
            args.demos,
            args.out,
            epochs=args.epochs,
            batch_size=args.batch,
            lr=args.lr,
            seed=args.seed,
            init_from=args.init_from,
            time_budget_s=args.time_budget,
            arch=args.arch,
            via_weight=args.via_weight,
        )
        return 0

    if args.command == "eval":
        board, nets = _load_route_input(args.input)
        report = evaluate_offline(args.model, board, nets, clearance_cells=args.clearance,
                                  max_nets=args.max_nets, rollout_cap=args.rollout_cap,
                                  mode=args.mode)
        print(json.dumps(report, indent=2, ensure_ascii=False))
        return 0

    return 1


if __name__ == "__main__":
    raise SystemExit(main())
