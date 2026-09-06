"""Classe abstraite de base pour les agents RL du pipeline KidCAD-Pro-IA.

Tout agent (PPO aujourd'hui, autres demain) implémente :
  - ``select_action(obs, deterministic)`` : politique d'action ;
  - ``learn(...)`` : mise à jour des paramètres ;
  - ``save(path)`` / ``load(path)`` : persistance des checkpoints ``.pt``.
"""

from __future__ import annotations

import abc
from typing import Any, Dict, Tuple

import numpy as np


class BaseAgent(abc.ABC):
    """Interface commune des agents d'entraînement RL."""

    def __init__(self, observation_shape: Tuple[int, ...], n_actions: int) -> None:
        self.observation_shape = observation_shape
        self.n_actions = n_actions
        self.total_steps = 0

    # ------------------------------------------------------------------ politique
    @abc.abstractmethod
    def select_action(self, obs: np.ndarray, deterministic: bool = False) -> int:
        """Choisit une action pour l'observation donnée.

        Args:
            obs: observation (peut contenir une dimension de batch).
            deterministic: si vrai, utilise l'action gloutonne (évaluation).

        Returns:
            Index d'action dans [0, n_actions).
        """

    # ------------------------------------------------------------------ apprentissage
    @abc.abstractmethod
    def learn(self, batch: Dict[str, Any]) -> Dict[str, float]:
        """Effectue une mise à jour sur un batch de transitions.

        Args:
            batch: dict de tenseurs numpy {obs, actions, advantages, returns, ...}.

        Returns:
            Métriques d'apprentissage (pertes, entropie, KL…).
        """

    # ------------------------------------------------------------------ persistance
    @abc.abstractmethod
    def save(self, path: str) -> None:
        """Sauvegarde le checkpoint (torch.save du state_dict + métadonnées)."""

    @abc.abstractmethod
    def load(self, path: str) -> None:
        """Charge un checkpoint écrit par :meth:`save`."""

    # ------------------------------------------------------------------ helpers
    def _as_batch(self, obs: np.ndarray) -> np.ndarray:
        """Normalise l'observation en batch (1, *shape)."""
        arr = np.asarray(obs, dtype=np.float32)
        if arr.shape == self.observation_shape:
            arr = arr[None, ...]
        return arr

    @property
    def name(self) -> str:
        return self.__class__.__name__
