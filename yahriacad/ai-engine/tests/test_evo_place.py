"""Tests d'EvoPlace : déterminisme, convergence, contraintes physiques."""

from __future__ import annotations

import random
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))  # racine ai-engine

from src.evolution.evo_place import (  # noqa: E402
    EvoComponent,
    EvoConfig,
    EvoNet,
    Placement,
    clamp_to_board,
    evolve,
    fitness,
    hpwl,
    overlap_area,
    random_placement,
)


@pytest.fixture()
def small_board():
    comps = [
        EvoComponent("U1", 6.0, 6.0),
        EvoComponent("U2", 4.0, 4.0),
        EvoComponent("R1", 1.6, 0.8),
        EvoComponent("C1", 1.6, 0.8),
    ]
    nets = [
        EvoNet("VCC", ["U1", "U2"]),
        EvoNet("SIG", ["U2", "R1", "C1"]),
    ]
    return comps, nets, 30.0, 20.0


def test_hpwl_half_perimeter():
    from src.evolution.evo_place import Placement  # noqa: F401  (déjà importé)

    net = EvoNet("N", ["A", "B"])
    p = Placement({"A": (0.0, 0.0), "B": (10.0, 6.0)})
    assert hpwl(net, p) == pytest.approx(16.0)


def test_overlap_detected():
    a = EvoComponent("A", 2, 2)
    b = EvoComponent("B", 2, 2)
    overlapped = Placement({"A": (1.0, 1.0), "B": (2.0, 2.0)})
    separated = Placement({"A": (0.0, 0.0), "B": (10.0, 10.0)})
    assert overlap_area(a, b, overlapped) > 0
    assert overlap_area(a, b, separated) == 0


def test_evolution_improves_random_baseline(small_board):
    """Après évolution, la fitness doit battre la moyenne des aléas."""
    comps, nets, w, h = small_board
    rng = random.Random(7)
    baseline = [
        fitness(random_placement(comps, w, h, rng), comps, nets, EvoConfig())
        for _ in range(30)
    ]
    mean_baseline = sum(baseline) / len(baseline)

    best, score, history = evolve(
        comps, nets, w, h, EvoConfig(generations=40, population_size=30)
    )
    assert score < mean_baseline
    assert len(history) == 40
    # monotone non-croissante du meilleur (élitisme)
    bests = [s.best_fitness for s in history]
    assert all(bests[i] >= bests[i + 1] - 1e-9 for i in range(len(bests) - 1))
    assert score == pytest.approx(bests[-1])


def test_evolution_deterministic_with_seed(small_board):
    comps, nets, w, h = small_board
    cfg = EvoConfig(generations=10, population_size=15, seed=123)
    s1 = evolve(comps, nets, w, h, cfg)[1]
    s2 = evolve(comps, nets, w, h, cfg)[1]
    assert s1 == pytest.approx(s2)


def test_placement_stays_on_board(small_board):
    comps, nets, w, h = small_board
    best, _, _ = evolve(comps, nets, w, h, EvoConfig(generations=15, seed=5))
    clamped = clamp_to_board(best, comps, w, h)
    for c in comps:
        x, y = clamped.coords[c.ref]
        assert c.width_mm / 2 - 1e-9 <= x <= w - c.width_mm / 2 + 1e-9
        assert c.height_mm / 2 - 1e-9 <= y <= h - c.height_mm / 2 + 1e-9
