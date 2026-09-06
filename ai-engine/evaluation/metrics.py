"""Métriques d'évaluation des agents de routage PCB.

Métriques calculées sur un ensemble de grilles / nets :
  - longueur totale routée (cases, convertible en mm via la grille 0.635 mm) ;
  - nombre de vias utilisés ;
  - taux de complétion des nets (nets connectés / nets tentés) ;
  - nombre de violations DRC (clearance, bord — paramétrable).

Utilisé par ``evaluation/bench.py`` et les evals périodiques d'entraînement.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Dict, Iterable, List, Sequence, Tuple

ROUTING_GRID_MM = 0.635  # 25 mil, aligné sur le moteur TS


@dataclass
class DrcThresholds:
    """Seuils DRC (mm) pour l'évaluation, alignés sur les règles par défaut."""

    min_track_width: float = 0.25
    min_clearance: float = 0.2
    min_via_drill: float = 0.35
    min_via_diameter: float = 0.7
    edge_clearance: float = 0.3


@dataclass
class EvalMetrics:
    """Résultat d'une évaluation (agrégable)."""

    routed_length_cells: int = 0
    routed_length_mm: float = 0.0
    total_vias: int = 0
    nets_total: int = 0
    nets_completed: int = 0
    drc_violations: int = 0
    episodes: int = 0
    extra: Dict[str, float] = field(default_factory=dict)

    @property
    def completion_rate(self) -> float:
        """Taux de complétion des nets ∈ [0, 1]."""
        return self.nets_completed / self.nets_total if self.nets_total else 0.0

    def merge(self, other: "EvalMetrics") -> "EvalMetrics":
        """Fusionne deux métriques (somme des compteurs)."""
        self.routed_length_cells += other.routed_length_cells
        self.routed_length_mm += other.routed_length_mm
        self.total_vias += other.total_vias
        self.nets_total += other.nets_total
        self.nets_completed += other.nets_completed
        self.drc_violations += other.drc_violations
        self.episodes += other.episodes
        self.extra.update(other.extra)
        return self


def path_length_cells(path: Sequence[Tuple[int, int, int]]) -> int:
    """Longueur d'un chemin (nombre de pas, en cases de grille)."""
    if len(path) < 2:
        return 0
    return len(path) - 1


def count_vias(path: Sequence[Tuple[int, int, int]]) -> int:
    """Nombre de changements de couche (= vias) le long du chemin."""
    return sum(1 for (a, b) in zip(path, path[1:]) if a[2] != b[2])


def manhattan_lower_bound(start: Tuple[int, int, int], end: Tuple[int, int, int]) -> int:
    """Borne inférieure de longueur (distance de Manhattan + coût de couche)."""
    layer_cost = 1 if start[2] != end[2] else 0
    return abs(start[0] - end[0]) + abs(start[1] - end[1]) + layer_cost


def clearance_violations(
    paths: Iterable[Sequence[Tuple[int, int, int]]],
    grid_shape: Tuple[int, int, int],
    thresholds: DrcThresholds | None = None,
    min_gap_cells: int = 1,
) -> List[str]:
    """Détecte les violations de clearance/bord pour un ensemble de chemins.

    Args:
        paths: chemins routés (cases (y, x, layer)).
        grid_shape: (h, w, layers) de la grille.
        thresholds: seuils DRC en mm (conversion : gap 1 case ≈ 0.635 mm, donc
            toute case adjacente respecte 0.2 mm ; on contrôle ici un gap min
            en cases + la distance au bord).
        min_gap_cells: écart minimal entre deux chemins de nets différents.

    Returns:
        Liste de messages de violation (vide = conforme).
    """
    thresholds = thresholds or DrcThresholds()
    h, w, _ = grid_shape
    violations: List[str] = []
    edge_margin_cells = max(1, int(round(thresholds.edge_clearance / ROUTING_GRID_MM)))

    cell_owner: Dict[Tuple[int, int, int], int] = {}
    for net_idx, path in enumerate(paths):
        for (y, x, layer) in path:
            # Distance au bord
            if y < edge_margin_cells or x < edge_margin_cells \
                    or y >= h - edge_margin_cells or x >= w - edge_margin_cells:
                violations.append(
                    f"net#{net_idx} : cuivre à moins de {thresholds.edge_clearance} mm du bord "
                    f"({y},{x})"
                )
            # Chevauchement direct
            if (y, x, layer) in cell_owner and cell_owner[(y, x, layer)] != net_idx:
                violations.append(f"net#{cell_owner[(y, x, layer)]}×net#{net_idx} : court-circuit ({y},{x})")
            cell_owner[(y, x, layer)] = net_idx

    # Clearance entre nets différents (distance Chebyshev < min_gap_cells)
    cells_by_net: Dict[int, List[Tuple[int, int, int]]] = {}
    for net_idx, path in enumerate(paths):
        cells_by_net[net_idx] = list(path)
    nets = sorted(cells_by_net)
    for i, net_a in enumerate(nets):
        for net_b in nets[i + 1:]:
            for (ya, xa, la) in cells_by_net[net_a]:
                for (yb, xb, lb) in cells_by_net[net_b]:
                    if la != lb:
                        continue
                    gap = max(abs(ya - yb), abs(xa - xb))
                    if 0 < gap < min_gap_cells:
                        violations.append(
                            f"net#{net_a}×net#{net_b} : clearance {gap * ROUTING_GRID_MM:.3f} mm "
                            f"< {thresholds.min_clearance} mm ({xa},{ya})–({xb},{yb})"
                        )
    return violations


def episode_metrics(
    paths: Dict[str, Sequence[Tuple[int, int, int]]],
    connected: Dict[str, bool],
    grid_shape: Tuple[int, int, int],
    thresholds: DrcThresholds | None = None,
) -> EvalMetrics:
    """Agrège les métriques d'un épisode (un ou plusieurs nets).

    Args:
        paths: {net_name: chemin (cases)} des tentatives de routage.
        connected: {net_name: booléen de connexion réussie}.
        grid_shape: dimensions de la grille.
        thresholds: seuils DRC.

    Returns:
        EvalMetrics rempli (longueur mm incluse via ROUTING_GRID_MM).
    """
    thresholds = thresholds or DrcThresholds()
    metrics = EvalMetrics(nets_total=len(paths), episodes=1)
    for name, path in paths.items():
        metrics.routed_length_cells += path_length_cells(path)
        metrics.total_vias += count_vias(path)
        if connected.get(name, False):
            metrics.nets_completed += 1
    metrics.routed_length_mm = metrics.routed_length_cells * ROUTING_GRID_MM
    metrics.drc_violations = len(
        clearance_violations(list(paths.values()), grid_shape, thresholds)
    )
    return metrics


def format_table(rows: List[Dict[str, str]], headers: Sequence[str]) -> str:
    """Formate un tableau ASCII simple (utilisé par bench.py)."""
    widths = {h: max(len(h), *(len(str(r.get(h, ""))) for r in rows)) for h in headers}
    line = "+-" + "-+-".join("-" * widths[h] for h in headers) + "-+"
    head = "| " + " | ".join(h.ljust(widths[h]) for h in headers) + " |"
    body = [
        "| " + " | ".join(str(r.get(h, "")).ljust(widths[h]) for h in headers) + " |"
        for r in rows
    ]
    return "\n".join([line, head, line, *body, line])
