#!/usr/bin/env python3
"""Passe DAgger 4 couches : états on-policy de la politique COURANTE étiquetés par A*.

Rejoue la recette BC v6 (v5w + DAgger#2 ×8 → 50 %) en 4 couches, avec la leçon
du Task 19 intégrée : la collecte DOIT partir de la meilleure politique
disponible (un DAgger depuis un checkpoint faible produit des états hors
distribution et effondre l'entraînement — 0.099 constaté).

  1. roule la politique BC passée en argument sur les cartes réalistes
     d'ENTRAÎNEMENT (seeds 44000..44023 ; l'éval reste sur 46000+ non vues) ;
  2. étiquette le premier pas correct A* depuis chaque cellule visitée
     (y compris les états d'erreur — c'est tout l'intérêt DAgger) ;
  3. sauve les démonstrations DAgger dans un npz fusionnable.

Exécution par LOTS (le sandbox coupe les appels longs, on ne doit jamais
perdre les cartes déjà traitées) : un lot = un appel = un fichier npz indépendant.

Usage : python3 dagger_round_4l.py <out.npz> <model.pt> <première_cartee> <n_cartes> [rollout_cap]
Ex.   : python3 dagger_round_4l.py training/router/demos_dag2_r0.npz \
            training/router/model_bc_4l.pt 44000 3 300
"""
import sys
import time
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402
torch.set_num_threads(2)  # cgroup restreint : 13,6 ms/forward mesuré avec 2 threads

from evaluation.bench import make_realistic_board  # noqa: E402
from training.router.imitation import collect_dagger, save_demos  # noqa: E402


def main() -> int:
    out = Path(sys.argv[1] if len(sys.argv) > 1 else ROOT / "training/router/demos_dag2_4l.npz")
    model = sys.argv[2] if len(sys.argv) > 2 else str(ROOT / "training/router/model_bc_4l.pt")
    first = int(sys.argv[3]) if len(sys.argv) > 3 else 44000
    count = int(sys.argv[4]) if len(sys.argv) > 4 else 3
    rollout_cap = int(sys.argv[5]) if len(sys.argv) > 5 else 300

    demos: list[dict] = []
    out.parent.mkdir(parents=True, exist_ok=True)
    t0 = time.monotonic()
    print(f"dagger: lot cartes {first}..{first + count - 1} cap={rollout_cap}",
          flush=True)  # trace AVANT la collecte : si le process est tué, on le sait
    for k in range(count):
        seed = first + k
        board, nets = make_realistic_board(seed, layer_count=4)
        got = collect_dagger(model, board, nets, clearance_cells=1,
                             max_states_per_net=200, rollout_cap=rollout_cap)
        demos.extend(got)
        save_demos(demos, out)  # sauvegarde incrémentale au sein du lot
        print(f"dagger: carte {seed} -> +{len(got)} demos "
              f"(lot {len(demos)}, {time.monotonic() - t0:.0f}s, sauvegardé)",
              flush=True)
    print(f"dagger: {len(demos)} demonstrations -> {out} "
          f"({time.monotonic() - t0:.0f}s)", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
