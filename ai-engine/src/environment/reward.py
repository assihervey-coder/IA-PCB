"""Fonctions de récompense pour l'environnement PCB (RL).

Récompense = progression (delta de distance Manhattan à la cible)
           + bonus de connexion quand le net est complété
           − pénalités (via, collision, pas de temps, abandon).
Tous les poids sont paramétrables via :class:`RewardConfig`, y compris les
clearances DRC (utilisées pour pénaliser les placements trop proches du bord
ou des autres nets lors de l'évaluation).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Tuple


@dataclass
class RewardConfig:
    """Poids des récompenses/pénalités (valeurs par défaut cohérentes)."""

    # Progression : +1 par case de rapprochement, −1 par éloignement
    progress_scale: float = 1.0
    # Pénalités structurelles
    via_penalty: float = 5.0          # coût d'un changement de couche (via)
    collision_penalty: float = 25.0   # tentative de déplacement invalide
    step_penalty: float = 0.05        # coût par pas (favorise les chemins courts)
    give_up_penalty: float = 10.0     # action FINISH hors cible
    # Bonus
    connect_bonus: float = 50.0       # net connecté
    # Clearances DRC (mm) — utilisées par les pénalités d'évaluation
    drc_min_clearance: float = 0.2    # isolation min entre pistes
    drc_edge_clearance: float = 0.3   # distance min au bord de carte
    drc_violation_penalty: float = 15.0


def progress_reward(prev_dist: float, new_dist: float, scale: float = 1.0) -> float:
    """Delta de progression : positif quand l'agent se rapproche de la cible."""
    return (prev_dist - new_dist) * scale


def length_penalty(path_length: int, optimal_length: int, weight: float = 0.1) -> float:
    """Pénalité relative d'excès de longueur par rapport à un optimum théorique."""
    if optimal_length <= 0:
        return 0.0
    return -weight * max(0, path_length - optimal_length) / optimal_length


def via_penalty(is_via: bool, weight: float = 5.0) -> float:
    """Pénalité fixe appliquée à chaque changement de couche (via)."""
    return -weight if is_via else 0.0


def collision_penalty(collision: bool, weight: float = 25.0) -> float:
    """Pénalité de collision (hors-grille, piste existante, pad étranger)."""
    return -weight if collision else 0.0


def drc_clearance_penalty(
    min_gap_mm: float,
    edge_gap_mm: float,
    cfg: RewardConfig | None = None,
) -> float:
    """Pénalité si les clearances DRC ne sont pas respectées.

    Args:
        min_gap_mm: gap réel mesuré entre la piste et le cuivre voisin (mm).
        edge_gap_mm: gap réel entre la piste et le bord de carte (mm).
        cfg: configuration (pénalité et seuils) ; défaut si None.

    Returns:
        Pénalité (négative) cumulée, 0.0 si tout est conforme.
    """
    cfg = cfg or RewardConfig()
    penalty = 0.0
    if min_gap_mm < cfg.drc_min_clearance:
        penalty -= cfg.drc_violation_penalty
    if edge_gap_mm < cfg.drc_edge_clearance:
        penalty -= cfg.drc_violation_penalty
    return penalty


def compute_reward(
    cfg: RewardConfig,
    *,
    progress_delta: float = 0.0,
    via: bool = False,
    collision: bool = False,
    connected: bool = False,
    gave_up: bool = False,
    steps: int = 0,
) -> Tuple[float, bool]:
    """Récompense composite d'un pas de simulation.

    Returns:
        (reward, terminated) — ``terminated`` est vrai si le net est connecté
        ou si l'agent a abandonné/collisionné (selon la politique choisie ici :
        collision termine l'épisode avec pénalité, abandon aussi).
    """
    reward = progress_delta * cfg.progress_scale
    reward -= cfg.step_penalty
    if via:
        reward -= cfg.via_penalty
    if collision:
        return reward - cfg.collision_penalty, True
    if gave_up:
        return reward - cfg.give_up_penalty, True
    if connected:
        reward += cfg.connect_bonus
    return reward, connected or gave_up
