"""Reward shaping for the PCB routing environment.

Reward design
-------------
The total return of an episode decomposes into::

    R = sum_t [ progress_coef * (d_{t-1} - d_t)      # potential-based shaping
                - step_penalty                        # length / time cost
                - via_penalty        (on via)         # layer changes are costly
                - collision_penalty  (on invalid)     # invalid moves are discouraged ]
        + success_bonus - step_penalty * path_len      # terminal success reward

The progress term is potential-based (it telescopes over any trajectory), so it
changes the shape of the learning problem without changing the optimal policy:
the fastest valid route to a target pad maximises the return. Distances are
expressed in grid cells (a via counts as one layer unit in the distance).
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class RewardConfig:
    """Weights mirroring ``EnvConfig`` (kept separate for pure-function use).

    Attributes:
        via_penalty: reward subtracted for each layer change (via).
        step_penalty: reward subtracted for each environment step.
        progress_coef: multiplier of the per-step distance decrease.
        success_bonus: reward granted when a target pad is reached.
        collision_penalty: reward subtracted for an invalid move.
    """

    via_penalty: float = 15.0
    step_penalty: float = 0.02
    progress_coef: float = 8.0
    success_bonus: float = 100.0
    collision_penalty: float = 2.0


class RewardShaper:
    """Pure reward functions derived from a :class:`RewardConfig`.

    Every method is side-effect free and returns a ``float`` so the shaping
    can be unit-tested and reused outside the environment.
    """

    def __init__(self, config: RewardConfig | None = None) -> None:
        self.cfg = config if config is not None else RewardConfig()

    def progress(self, prev_dist: float, new_dist: float) -> float:
        """Potential-based progress reward (positive when closer to a target)."""
        return self.cfg.progress_coef * (float(prev_dist) - float(new_dist))

    def step_cost(self) -> float:
        """Per-step penalty (encourages short routes)."""
        return -self.cfg.step_penalty

    def via_cost(self) -> float:
        """Penalty applied once per successful layer change."""
        return -self.cfg.via_penalty

    def collision(self) -> float:
        """Penalty applied when a move is invalid (position unchanged)."""
        return -self.cfg.collision_penalty

    def success(self) -> float:
        """Bonus granted when a target pad cell is reached."""
        return self.cfg.success_bonus

    def terminal_length_cost(self, path_len_cells: int) -> float:
        """Extra length penalty applied at termination (mirrors step costs)."""
        return -self.cfg.step_penalty * float(max(0, int(path_len_cells)))
