#!/usr/bin/env python3
"""IL suite ① : ronde DAgger sur la vraie carte avec un checkpoint BC.

Les états visités par la politique (succès ET erreurs) sont étiquetés par
le premier pas de l'expert A* — c'est la clé contre les erreurs composées.

Usage :
    python3 scripts/il_dagger.py <model.pt> <out.npz> [max_nets] [rollout_cap]

Contrainte sandbox : à lancer dans une session bash de moins de 600 s ;
fractionner via max_nets si besoin (les npz se fusionnent ensuite).
"""

from __future__ import annotations

import json
import sys
import time
from pathlib import Path

AIROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(AIROOT))
sys.path.insert(0, str(AIROOT.parents[1] / "shared" / "gen" / "python"))

from training.router.imitation import collect_dagger, save_demos  # noqa: E402


def main() -> int:
    model_path = sys.argv[1] if len(sys.argv) > 1 else "training/router/model_bc_v3.pt"
    out_path = sys.argv[2] if len(sys.argv) > 2 else "training/router/demos_dag_v3.npz"
    max_nets = int(sys.argv[3]) if len(sys.argv) > 3 else 0
    rollout_cap = int(sys.argv[4]) if len(sys.argv) > 4 else 250

    payload = json.loads(
        (Path("/tmp/yahriacad-imitation/route_input.json")).read_text(encoding="utf-8")
    )
    board, nets = payload["board"], payload["nets"]

    started = time.monotonic()
    mp = Path(model_path)
    if not mp.is_absolute():
        mp = AIROOT / mp
    op = Path(out_path)
    if not op.is_absolute():
        op = AIROOT / op
    demos = collect_dagger(
        str(mp),
        board,
        nets,
        rollout_cap=rollout_cap,
        max_states_per_net=120,
    )
    if max_nets and max_nets > 0:
        # collect_dagger route tous les nets ; pour fractionner, on ne garde
        # que les N premières démonstrations (ordre = ordre de routage).
        demos = demos[:max_nets]
    save_demos(demos, op)
    steps = sum(len(d["actions"]) for d in demos)
    print(f"dagger: {len(demos)} demos / {steps} pas en {time.monotonic()-started:.0f}s -> {op}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
