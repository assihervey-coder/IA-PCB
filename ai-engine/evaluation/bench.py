"""Benchmark CLI — évalue un agent sur N grilles aléatoires.

Pipeline R&D YahriaCad (non requis en production). Compare des agents :
  - ``random`` : agent aléatoire (baseline) ;
  - ``greedy`` : heuristique gloutonne (se rapproche de la cible) ;
  - ``ppo``    : politique PPO entraînée (checkpoint .pt, optionnel).

Usage :
    python evaluation/bench.py --grids 20 --agent random
    python evaluation/bench.py --grids 50 --agent greedy --grid-size 48
    python evaluation/bench.py --grids 50 --agent ppo --ckpt training/router/model_v1.pt

Affiche un tableau des métriques : longueur totale routée (mm), vias, taux de
complétion, violations DRC (voir evaluation/metrics.py).
"""

from __future__ import annotations

import argparse
import os
import random
import sys
from typing import Callable, Dict, List, Sequence, Tuple

# Rend les packages ``src`` et ``evaluation`` importables quel que soit le cwd
_HERE = os.path.dirname(os.path.abspath(__file__))                  # ai-engine/evaluation
_AI_ROOT = os.path.dirname(_HERE)                                   # ai-engine
sys.path.insert(0, os.path.join(_AI_ROOT, "src"))                   # environment.*, agents.*
sys.path.insert(0, _AI_ROOT)                                        # evaluation.*

from environment.pcb_env import PcbEnv, PcbEnvConfig  # noqa: E402
from evaluation.metrics import (  # noqa: E402
    DrcThresholds,
    EvalMetrics,
    episode_metrics,
    format_table,
)


# --------------------------------------------------------------------------- agents
def random_agent(obs: "object", action_space_n: int, rng: random.Random) -> int:
    """Agent aléatoire — baseline inférieure."""
    return rng.randrange(action_space_n)


def greedy_agent(env: PcbEnv) -> int:
    """Heuristique gloutonne : avance vers la cible, bascule de couche si besoin.

    Retourne l'index d'action qui réduit la distance Manhattan à la cible ;
    si bloqué, tente un via, sinon FINISH.
    """
    (y, x, layer) = env._state.pos
    (ty, tx, tlayer) = env.net.target
    candidates: List[Tuple[int, int]] = []
    if ty > y:
        candidates.append((1, 1))     # ACTION_DOWN
    if ty < y:
        candidates.append((1, 0))     # ACTION_UP
    if tx > x:
        candidates.append((1, 3))     # ACTION_RIGHT
    if tx < x:
        candidates.append((1, 2))     # ACTION_LEFT
    if tlayer != layer:
        candidates.append((0, 4))     # ACTION_VIA (via gratuit à viser)
    candidates.sort(reverse=True)     # réduit d'abord le plus grand écart
    for _pref, action in candidates:
        return action
    return 5                           # ACTION_FINISH


def make_ppo_agent(checkpoint: str | None):
    """Charge l'agent PPO si torch est disponible, sinon None."""
    try:
        import torch  # noqa: F401

        from agents.ppo_agent import PpoAgent  # type: ignore

        agent = PpoAgent()
        if checkpoint:
            agent.load(checkpoint)
        agent.train_mode(False)
        return agent
    except Exception as exc:  # pragma: no cover
        print(f"[bench] PPO indisponible ({exc}) — utiliser --agent random|greedy.")
        return None


# --------------------------------------------------------------------------- rollout
def run_episode(env: PcbEnv, policy: Callable[..., int]) -> Tuple[EvalMetrics, Dict[str, object]]:
    """Fait tourner un épisode complet et agrège les métriques.

    ``policy(obs, env)`` reçoit l'observation ET l'environnement (les
    heuristiques comme ``greedy_agent`` ont besoin de l'état interne de l'env).
    """
    obs, _info = env.reset()
    done = False
    info: Dict[str, object] = {}
    while not done:
        action = policy(obs, env)
        obs, _reward, terminated, truncated, info = env.step(action)
        done = terminated or truncated
    connected = bool(info.get("connected", False))
    metrics = episode_metrics(
        paths={env.net.name: env._state.path},
        connected={env.net.name: connected},
        grid_shape=env.grid_shape,
        thresholds=DrcThresholds(),
    )
    return metrics, {"net": env.net.name, "connected": connected, "steps": info.get("steps", 0)}


# --------------------------------------------------------------------------- main
def main() -> None:
    parser = argparse.ArgumentParser(
        description="Évalue un agent de routage sur N grilles aléatoires (R&D, non requis en prod)."
    )
    parser.add_argument("--grids", type=int, default=20, help="Nombre de grilles/épisodes")
    parser.add_argument("--agent", choices=["random", "greedy", "ppo"], default="greedy")
    parser.add_argument("--grid-size", type=int, default=32, help="Taille de grille (carrée)")
    parser.add_argument("--seed", type=int, default=42, help="Graine aléatoire")
    parser.add_argument("--ckpt", default=None, help="Checkpoint .pt pour --agent ppo")
    args = parser.parse_args()

    rng = random.Random(args.seed)
    total = EvalMetrics()

    if args.agent == "ppo":
        agent = make_ppo_agent(args.ckpt)
        if agent is None:
            sys.exit(1)

        def policy(obs: object, env: PcbEnv) -> int:
            return agent.select_action(obs, deterministic=True)  # type: ignore[union-attr]
    elif args.agent == "greedy":

        def policy(obs: object, env: PcbEnv) -> int:
            return greedy_agent(env)
    else:

        def policy(obs: object, env: PcbEnv) -> int:
            return random_agent(obs, 6, rng)

    for episode in range(args.grids):
        env = PcbEnv(PcbEnvConfig(grid_h=args.grid_size, grid_w=args.grid_size,
                                  seed=args.seed + episode))
        metrics, _ = run_episode(env, policy)
        total.merge(metrics)

    rows: List[Dict[str, str]] = [
        {
            "agent": args.agent,
            "grilles": str(args.grids),
            "taille": f"{args.grid_size}x{args.grid_size}x2",
            "longueur (mm)": f"{total.routed_length_mm:.1f}",
            "vias": str(total.total_vias),
            "completion": f"{total.completion_rate * 100:.1f}%",
            "violations DRC": str(total.drc_violations),
        }
    ]
    print(format_table(rows, ["agent", "grilles", "taille", "longueur (mm)",
                              "vias", "completion", "violations DRC"]))
    print("\nRappel : ce benchmark est un outil R&D. L'application YahriaCad")
    print("utilise le moteur TS déterministe (A* + rip-up & reroute) en production.")


if __name__ == "__main__":
    main()
