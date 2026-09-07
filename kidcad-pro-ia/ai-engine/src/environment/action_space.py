"""Discrete action space for the PCB routing environment.

12 actions = 4 in-layer moves x 3 layer modes (stay / via-up / via-down).

Index layout::

    action = move_index * 3 + layer_mode_index

    move_index : 0..3 -> ``MOVES[move_index]`` = ``(dx, dy)``
    layer_mode : 0..2 -> ``LAYER_MODES[layer_mode]`` in {0, +1, -1}

Deltas are expressed in grid cells with ``+x`` to the right and ``+y``
downwards (image convention: board origin at the top-left corner).
Layer 0 is F.Cu, ``layer_count - 1`` is B.Cu.
"""

from __future__ import annotations

import random
from typing import List, Tuple

# Ordered list of in-layer moves (dx, dy): up, right, down, left.
MOVES: List[Tuple[int, int]] = [(0, -1), (1, 0), (0, 1), (-1, 0)]

# Ordered layer modes: stay, change layer +1 (up), change layer -1 (down).
LAYER_MODES: List[int] = [0, 1, -1]

N_MOVES: int = len(MOVES)
N_MODES: int = len(LAYER_MODES)
N_ACTIONS: int = N_MOVES * N_MODES

MOVE_NAMES: Tuple[str, ...] = ("up", "right", "down", "left")
MODE_NAMES: Tuple[str, ...] = ("stay", "via-up", "via-down")


class ActionSpace:
    """Discrete action space with 12 actions (4 moves x {stay, via-up, via-down}).

    Example:
        >>> space = ActionSpace()
        >>> space.n
        12
        >>> space.decode(0)
        (0, -1, 0)
        >>> space.decode(4)
        (1, 0, 0)
        >>> space.is_via(1)
        True
    """

    n: int = N_ACTIONS

    def decode(self, action: int) -> Tuple[int, int, int]:
        """Return ``(dx, dy, dlayer)`` for a valid action index.

        Raises:
            IndexError: if ``action`` is outside ``[0, n)``.
        """
        if not self.contains(action):
            raise IndexError(f"Action {action} out of range [0, {N_ACTIONS})")
        move = MOVES[int(action) // N_MODES]
        mode = LAYER_MODES[int(action) % N_MODES]
        return move[0], move[1], mode

    def sample(self, rng: random.Random | None = None) -> int:
        """Draw a uniformly random action index.

        Args:
            rng: optional ``random.Random`` instance for reproducible
                sampling; when omitted the global ``random`` module is used.
        """
        source = rng if rng is not None else random
        return int(source.randrange(self.n))

    def contains(self, action: object) -> bool:
        """Return True when ``action`` is a valid action index."""
        return isinstance(action, (int,)) and 0 <= int(action) < self.n

    def is_via(self, action: int) -> bool:
        """Return True when the action changes layer (a via transition)."""
        if not self.contains(action):
            return False
        return LAYER_MODES[int(action) % N_MODES] != 0

    def name(self, action: int) -> str:
        """Human readable action name, e.g. ``"right+via-up"``."""
        if not self.contains(action):
            return "invalid"
        move = MOVE_NAMES[int(action) // N_MODES]
        mode = MODE_NAMES[int(action) % N_MODES]
        return f"{move}+{mode}"

    def describe(self) -> str:
        """Return a multi-line description of every action (debug helper)."""
        lines = [f"ActionSpace(n={self.n}) = 4 moves x 3 layer modes"]
        for move_idx, (dx, dy) in enumerate(MOVES):
            for mode_idx, dl in enumerate(LAYER_MODES):
                action = move_idx * N_MODES + mode_idx
                lines.append(
                    f"  {action:>2}: {MOVE_NAMES[move_idx]:<5} (dx={dx:+d}, dy={dy:+d})"
                    f" + {MODE_NAMES[mode_idx]:<8} (dlayer={dl:+d})"
                )
        return "\n".join(lines)

    def __len__(self) -> int:
        return self.n

    def __repr__(self) -> str:  # pragma: no cover - debug helper
        return f"ActionSpace(n={self.n})"
