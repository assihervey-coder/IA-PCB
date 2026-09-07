#!/usr/bin/env python3
"""Experiences BC : (A) sur-apprentissage d'une demo, (B) entrainement long."""

from __future__ import annotations

import sys
from pathlib import Path

AI_ENGINE_ROOT = Path(__file__).resolve().parents[1] / "yahriacad" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE_ROOT))
sys.path.insert(0, str(AI_ENGINE_ROOT.parents[0] / "shared" / "gen" / "python"))

from evaluation.bench import make_synthetic_board  # noqa: E402
from training.router.imitation import (  # noqa: E402
    collect_demos,
    save_demos,
    train_bc,
)


def main() -> int:
    board, nets = make_synthetic_board(42002)
    demos = collect_demos(board, nets)
    root = AI_ENGINE_ROOT / "training/router"

    # --- A : une seule demo, 120 epochs -> le reseau peut-il memoriser ?
    one = [d for d in demos if len(d["actions"]) >= 40][:1] or demos[:1]
    save_demos(one, root / "demos_one.npz")
    stats_a = train_bc(str(root / "demos_one.npz"), str(root / "model_one.pt"),
                       epochs=120, batch_size=16, lr=3e-4, val_frac=0.2)
    print("A (1 demo):", stats_a["accuracy"], stats_a["class_accuracy"], flush=True)

    # --- B : toutes les demos, 40 epochs, lr 3e-4
    save_demos(demos, root / "demos_full.npz")
    stats_b = train_bc(str(root / "demos_full.npz"), str(root / "model_full.pt"),
                       epochs=40, batch_size=32, lr=3e-4)
    print("B (tout):", stats_b["accuracy"], stats_b["class_accuracy"], flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
