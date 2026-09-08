"""Espace d'actions discret de l'environnement PCB.

6 actions : haut, bas, gauche, droite, via (changement de couche), fin.
Chaque index se mappe vers (dx, dy, is_via) via ``ACTION_DELTAS`` ;
``action_delta(index)`` renvoie (dy, dx, is_via) dans l'ordre (ligne, colonne)
de la grille (axe y vers le bas, convention image).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import List, Tuple

# Ordre imposé des actions
ACTION_UP = 0
ACTION_DOWN = 1
ACTION_LEFT = 2
ACTION_RIGHT = 3
ACTION_VIA = 4
ACTION_FINISH = 5

# index → (dy, dx, is_via) ; VIA ne change pas (dy, dx) mais bascule de couche
ACTION_DELTAS: List[Tuple[int, int, bool]] = [
    (-1, 0, False),  # haut
    (1, 0, False),   # bas
    (0, -1, False),  # gauche
    (0, 1, False),   # droite
    (0, 0, True),    # via (F.Cu ↔ B.Cu)
    (0, 0, False),   # fin de tentative
]

NUM_ACTIONS = len(ACTION_DELTAS)
ACTION_NAMES: Tuple[str, ...] = ("up", "down", "left", "right", "via", "finish")


def action_delta(index: int) -> Tuple[int, int, bool]:
    """Retourne (dy, dx, is_via) pour l'index d'action donné.

    Raises:
        IndexError: si l'index est hors [0, NUM_ACTIONS).
    """
    if not 0 <= index < NUM_ACTIONS:
        raise IndexError(f"Action {index} hors bornes [0, {NUM_ACTIONS})")
    return ACTION_DELTAS[index]


@dataclass(frozen=True)
class ActionSpace:
    """Espace d'actions discret à 6 actions (compatible gym.spaces.Discrete).

    Attributs pratiques : ``UP``, ``DOWN``, ``LEFT``, ``RIGHT``, ``VIA``,
    ``FINISH`` ; méthodes ``sample()`` (aléatoire), ``contains()``.
    """

    UP: int = ACTION_UP
    DOWN: int = ACTION_DOWN
    LEFT: int = ACTION_LEFT
    RIGHT: int = ACTION_RIGHT
    VIA: int = ACTION_VIA
    FINISH: int = ACTION_FINISH
    n: int = NUM_ACTIONS

    def sample(self, rng=None) -> int:
        """Tire une action uniformément (rng optionnel type random.Random)."""
        import random

        r = rng or random
        return r.randrange(self.n)

    def contains(self, index: int) -> bool:
        """Vrai si l'index est une action valide."""
        return isinstance(index, int) and 0 <= index < self.n

    def delta(self, index: int) -> Tuple[int, int, bool]:
        """Mapping index → (dy, dx, is_via)."""
        return action_delta(index)

    def name(self, index: int) -> str:
        """Nom lisible de l'action (logs/debug)."""
        return ACTION_NAMES[index] if self.contains(index) else "invalid"

    def __len__(self) -> int:
        return self.n
