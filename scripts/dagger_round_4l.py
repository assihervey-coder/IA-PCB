#!/usr/bin/env python3
"""Passe DAgger 4 couches : états on-policy du BC étiquetés par A*.

Rejoue la recette BC v6 (v5w + DAgger#2 ×8) en 4 couches :
  1. roule la politique BC courante (model_bc_4l_best.pt) sur les cartes
     réalistes d'entraînement (seeds 44000..44011, layer_count=4) ;
  2. étiquette le premier pas correct A* depuis chaque cellule visitée
     (y compris les états d'erreur — c'est tout l'intérêt DAgger) ;
  3. sauve les démonstrations DAgger dans un npz fusionnable.

Usage : python3 dagger_round_4l.py <out.npz>
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

from evaluation.bench import make_realistic_board  # noqa: E402
from training.router.imitation import collect_dagger, save_demos  # noqa: E402

MODEL = ROOT / "training/router/model_bc_4l_best.pt"


def main() -> int:
    out = Path(sys.argv[1] if len(sys.argv) > 1 else "training/router/demos_dag_4l.npz")
    demos: list[dict] = []
    # Sauvegarde incrémentale : le sandbox coupe les appels longs, on ne doit
    # jamais perdre les cartes déjà traitées.
    out.parent.mkdir(parents=True, exist_ok=True)
    for k in range(8):
        board, nets = make_realistic_board(44000 + k, layer_count=4)
        got = collect_dagger(str(MODEL), board, nets, clearance_cells=1,
                             max_states_per_net=200, rollout_cap=400)
        demos.extend(got)
        save_demos(demos, out)
        print(f"dagger: carte {44000 + k} -> +{len(got)} demos "
              f"(total {len(demos)}, sauvegardé)", flush=True)
    print(f"dagger: {len(demos)} demonstrations -> {out}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
