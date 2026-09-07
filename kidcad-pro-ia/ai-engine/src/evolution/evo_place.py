"""EvoPlace: genetic-algorithm component placement for PCB boards.

Where ``place.go`` asks the RL engine for a plan, EvoPlace evolves one: a
real GA (population, tournament selection, BLX-alpha crossover, gaussian
mutation, elitism) whose fitness is the classic half-perimeter wire length
(HPWL) penalised by footprint overlaps. Every generation is reported
through a callback so the backend can stream the evolution live
("génération 7 : fitness 312.4 mm") and the CLI can print an animated
best-of-breed history.

Deterministic given the seed; pure stdlib (no numpy) so it runs anywhere
the ai-engine runs.

Usage (smoke)::

    python -m src.evolution.evo_place --demo

"""

from __future__ import annotations

import argparse
import math
import random
from dataclasses import dataclass, field
from typing import Callable, Dict, List, Optional, Sequence, Tuple

# --------------------------------------------------------------------------- #
# Modèle de problème
# --------------------------------------------------------------------------- #

Point = Tuple[float, float]


@dataclass
class EvoComponent:
    """Un composant à placer (empreinte rectangulaire simplifiée)."""

    ref: str
    width_mm: float = 2.0
    height_mm: float = 1.2

    @property
    def area(self) -> float:
        return self.width_mm * self.height_mm


@dataclass
class EvoNet:
    """Un net reliant des références (pour le calcul HPWL)."""

    name: str
    refs: List[str]


@dataclass
class Placement:
    """Un individu : ref -> (x, y) sur la carte."""

    coords: Dict[str, Point] = field(default_factory=dict)

    def copy(self) -> "Placement":
        return Placement(dict(self.coords))


@dataclass
class EvoConfig:
    """Hyper-paramètres du GA (valeurs par défaut raisonnables)."""

    population_size: int = 60
    generations: int = 120
    tournament_k: int = 3
    elite_count: int = 2
    crossover_rate: float = 0.9
    mutation_rate: float = 0.25
    mutation_sigma_mm: float = 3.0
    overlap_penalty_mm: float = 25.0
    seed: int = 42


@dataclass
class GenerationStat:
    """Instantané d'une génération (pour le streaming live)."""

    generation: int
    best_fitness: float
    mean_fitness: float
    best_placement: Placement


# --------------------------------------------------------------------------- #
# Fitness : HPWL + pénalité de recouvrement
# --------------------------------------------------------------------------- #


def hpwl(net: EvoNet, placement: Placement) -> float:
    """Demi-périmètre du rectangle englobant du net (mm)."""
    xs = [placement.coords[r][0] for r in net.refs if r in placement.coords]
    ys = [placement.coords[r][1] for r in net.refs if r in placement.coords]
    if len(xs) < 2:
        return 0.0
    return (max(xs) - min(xs)) + (max(ys) - min(ys))


def overlap_area(a: EvoComponent, b: EvoComponent, placement: Placement) -> float:
    """Aire de recouvrement entre deux composants placés (mm²)."""
    ax, ay = placement.coords[a.ref]
    bx, by = placement.coords[b.ref]
    dx = (a.width_mm + b.width_mm) / 2 - abs(ax - bx)
    dy = (a.height_mm + b.height_mm) / 2 - abs(ay - by)
    if dx > 0 and dy > 0:
        return dx * dy
    return 0.0


def fitness(
    placement: Placement,
    components: Sequence[EvoComponent],
    nets: Sequence[EvoNet],
    cfg: EvoConfig,
) -> float:
    """Fitness à minimiser : HPWL total + pénalités de recouvrement."""
    total = sum(hpwl(n, placement) for n in nets)
    for i, ci in enumerate(components):
        for cj in components[i + 1 :]:
            if overlap_area(ci, cj, placement) > 0:
                total += cfg.overlap_penalty_mm
    return total


# --------------------------------------------------------------------------- #
# Opérateurs génétiques
# --------------------------------------------------------------------------- #


def _grid_shape(n: int, board_w: float, board_h: float) -> Tuple[int, int]:
    """Grille quasi-carrée adaptée à l'emprise de la carte."""
    cols = max(1, int(math.sqrt(n * board_w / max(board_h, 1e-9))))
    rows = max(1, math.ceil(n / cols))
    return cols, rows


def random_placement(
    components: Sequence[EvoComponent],
    board_w: float,
    board_h: float,
    rng: random.Random,
) -> Placement:
    """Individu initial : dispersion en grille avec jitter aléatoire."""
    p = Placement()
    cols, rows = _grid_shape(len(components), board_w, board_h)
    for i, c in enumerate(components):
        gx = (i % cols + 0.5) * board_w / cols
        gy = (i // cols + 0.5) * board_h / rows
        jx = rng.uniform(-0.5, 0.5) * board_w / cols
        jy = rng.uniform(-0.5, 0.5) * board_h / rows
        p.coords[c.ref] = (
            min(max(gx + jx, c.width_mm / 2), board_w - c.width_mm / 2),
            min(max(gy + jy, c.height_mm / 2), board_h - c.height_mm / 2),
        )
    return p


def tournament_select(
    population: Sequence[Placement],
    scores: Sequence[float],
    k: int,
    rng: random.Random,
) -> Placement:
    """Sélection par tournoi (minimisation)."""
    idx = rng.sample(range(len(population)), min(k, len(population)))
    best = min(idx, key=lambda i: scores[i])
    return population[best]


def blend_crossover(
    parent_a: Placement,
    parent_b: Placement,
    rate: float,
    rng: random.Random,
    alpha: float = 0.35,
) -> Placement:
    """Crossover BLX-alpha, coordonnée par coordonnée."""
    if rng.random() > rate:
        return parent_a.copy()
    child = Placement()
    for ref in parent_a.coords:
        ax, ay = parent_a.coords[ref]
        bx, by = parent_b.coords[ref]
        lo_x = min(ax, bx) - alpha * abs(ax - bx)
        hi_x = max(ax, bx) + alpha * abs(ax - bx)
        lo_y = min(ay, by) - alpha * abs(ay - by)
        hi_y = max(ay, by) + alpha * abs(ay - by)
        child.coords[ref] = (rng.uniform(lo_x, hi_x), rng.uniform(lo_y, hi_y))
    return child


def mutate(
    placement: Placement,
    rate: float,
    sigma: float,
    rng: random.Random,
) -> Placement:
    """Mutation gaussienne coordonnée (contenue par clamp_to_board)."""
    for ref in placement.coords:
        if rng.random() < rate:
            x, y = placement.coords[ref]
            placement.coords[ref] = (x + rng.gauss(0, sigma), y + rng.gauss(0, sigma))
    return placement


def clamp_to_board(
    placement: Placement,
    components: Sequence[EvoComponent],
    board_w: float,
    board_h: float,
) -> Placement:
    """Reprojette les coordonnées dans l'emprise de la carte."""
    for c in components:
        x, y = placement.coords[c.ref]
        placement.coords[c.ref] = (
            min(max(x, c.width_mm / 2), board_w - c.width_mm / 2),
            min(max(y, c.height_mm / 2), board_h - c.height_mm / 2),
        )
    return placement


# --------------------------------------------------------------------------- #
# Boucle d'évolution
# --------------------------------------------------------------------------- #

GenerationCallback = Callable[[GenerationStat], None]


def evolve(
    components: Sequence[EvoComponent],
    nets: Sequence[EvoNet],
    board_w: float,
    board_h: float,
    cfg: Optional[EvoConfig] = None,
    on_generation: Optional[GenerationCallback] = None,
) -> Tuple[Placement, float, List[GenerationStat]]:
    """Fait évoluer un placement et renvoie (meilleur, fitness, historique)."""
    cfg = cfg or EvoConfig()
    rng = random.Random(cfg.seed)

    population = [
        random_placement(components, board_w, board_h, rng)
        for _ in range(cfg.population_size)
    ]

    def score_all(pop: Sequence[Placement]) -> List[float]:
        return [fitness(p, components, nets, cfg) for p in pop]

    scores = score_all(population)
    history: List[GenerationStat] = []
    best_idx = min(range(len(population)), key=lambda i: scores[i])
    best, best_score = population[best_idx].copy(), scores[best_idx]

    for gen in range(cfg.generations):
        # Élitisme : les meilleurs survivent tels quels.
        order = sorted(range(len(population)), key=lambda i: scores[i])
        next_pop = [population[i].copy() for i in order[: cfg.elite_count]]

        while len(next_pop) < cfg.population_size:
            pa = tournament_select(population, scores, cfg.tournament_k, rng)
            pb = tournament_select(population, scores, cfg.tournament_k, rng)
            child = blend_crossover(pa, pb, cfg.crossover_rate, rng)
            mutate(child, cfg.mutation_rate, cfg.mutation_sigma_mm, rng)
            next_pop.append(clamp_to_board(child, components, board_w, board_h))

        population = next_pop
        scores = score_all(population)
        gen_best = min(range(len(population)), key=lambda i: scores[i])
        if scores[gen_best] < best_score:
            best, best_score = population[gen_best].copy(), scores[gen_best]

        stat = GenerationStat(
            generation=gen + 1,
            best_fitness=best_score,
            mean_fitness=sum(scores) / len(scores),
            best_placement=best.copy(),
        )
        history.append(stat)
        if on_generation is not None:
            on_generation(stat)

    return best, best_score, history


# --------------------------------------------------------------------------- #
# Démo CLI (aucune dépendance externe)
# --------------------------------------------------------------------------- #


def _demo() -> None:
    comps = [
        EvoComponent("U1", 6.0, 6.0),
        EvoComponent("U2", 6.0, 4.0),
        EvoComponent("R1", 1.6, 0.8),
        EvoComponent("R2", 1.6, 0.8),
        EvoComponent("C1", 1.6, 0.8),
        EvoComponent("C2", 1.6, 0.8),
        EvoComponent("J1", 8.0, 3.0),
        EvoComponent("D1", 1.6, 0.8),
    ]
    nets = [
        EvoNet("VCC", ["J1", "U1", "U2"]),
        EvoNet("GND", ["J1", "U1", "U2", "C1", "C2"]),
        EvoNet("SIG_A", ["U1", "R1", "R2"]),
        EvoNet("LED", ["R2", "D1"]),
        EvoNet("BYP", ["C1", "U1", "C2", "U2"]),
    ]

    def bar(stat: GenerationStat) -> None:
        filled = min(60, int(stat.best_fitness / 6))
        print(
            f"gen {stat.generation:>3} | {stat.best_fitness:8.1f} mm | "
            f"moy {stat.mean_fitness:8.1f} | {'█' * (60 - filled)}"
        )

    best, score, history = evolve(comps, nets, 40.0, 30.0, on_generation=bar)
    print(f"\nMeilleur placement : {score:.1f} mm de HPWL en {len(history)} générations")
    for ref in sorted(best.coords):
        x, y = best.coords[ref]
        print(f"  {ref:>3} : ({x:6.2f}, {y:6.2f})")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="EvoPlace — placement génétique")
    parser.add_argument("--demo", action="store_true", help="lance la démo animée")
    args = parser.parse_args()
    if args.demo:
        _demo()
    else:
        parser.print_help()
