#!/usr/bin/env python3
"""Entraînement PPO du routeur IA (optionnel, requiert torch).

Le checkpoint produit est chargé automatiquement par ``src/service.py`` au
démarrage (clé ``router.model_path`` de ``training/router/config.yaml``) :
tant qu'aucun modèle n'est présent — ou torch absent — le RPC ``RouteBoard``
utilise le repli A* déterministe.

Exemples :
    # sortie rapide de démonstration (quelques minutes CPU) :
    python3 ai-engine/training/router/train.py --steps 50_000

    # entraînement complet reproductible :
    python3 ai-engine/training/router/train.py --steps 1_000_000 \\
        --seed 42 --out training/router/model_v1.pt
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

AI_ENGINE_ROOT = Path(__file__).resolve().parents[2]
REPO_ROOT = AI_ENGINE_ROOT.parents[0]

for _path in (str(AI_ENGINE_ROOT), str(REPO_ROOT / "shared" / "gen" / "python")):
    if _path not in sys.path:
        sys.path.insert(0, _path)


def _require_torch() -> None:
    """Abort with an actionable message when torch is missing."""
    try:
        import torch  # noqa: F401
    except Exception:
        sys.exit(
            "torch est requis pour l'entraînement (l'inférence n'en a PAS besoin).\n"
            "  pip install torch --index-url https://download.pytorch.org/whl/cpu\n"
            "Rappel : sans checkpoint, le moteur IA utilise le repli A* intégré."
        )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Entraînement PPO du routeur YahriaCad")
    parser.add_argument("--steps", type=int, default=200_000,
                        help="nombre total de pas d'environnement (défaut : 200000)")
    parser.add_argument("--seed", type=int, default=0, help="graine globale")
    parser.add_argument("--rollout", type=int, default=None,
                        help="longueur d'un rollout (défaut : config PPO)")
    parser.add_argument("--out", default="training/router/model_v1.pt",
                        help="chemin du checkpoint de sortie (relatif à ai-engine/)")
    parser.add_argument("--save-every", type=int, default=0,
                        help="sauvegarde intermédiaire tous les N pas (0 = final seul)")
    args = parser.parse_args(argv)

    _require_torch()

    import numpy as np
    from evaluation.bench import make_synthetic_board
    from src.agents.ppo_agent import PPOAgent, PPOConfig, PPOTrainer
    from src.environment.pcb_env import EnvConfig, PCBRouteEnv

    cfg = PPOConfig()
    cfg.seed = args.seed
    probe_env = PCBRouteEnv(*make_synthetic_board(args.seed),
                            EnvConfig(clearance_cells=1, seed=args.seed))
    in_channels = probe_env.observation_shape[0]
    agent = PPOAgent(cfg, in_channels=in_channels, n_actions=12, device="cpu")

    def env_factory() -> PCBRouteEnv:
        board, nets = make_synthetic_board(args.seed)
        return PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=args.seed))

    trainer = PPOTrainer(env_factory, agent, total_steps=args.steps,
                         rollout_len=args.rollout)

    history: dict[str, list] = {"steps": [], "episode_reward": []}

    def log(line: str) -> None:
        print(line, flush=True)
        # Capturer les métriques publiées par le trainer pour le résumé final.
        if getattr(trainer, "history", None):
            h = trainer.history
            if h.get("steps"):
                history["steps"] = list(h["steps"])
            if h.get("episode_reward"):
                history["episode_reward"] = list(h["episode_reward"])

    out_path = AI_ENGINE_ROOT / args.out
    out_path.parent.mkdir(parents=True, exist_ok=True)

    # Boucle manuelle pour la sauvegarde intermédiaire : le trainer expose
    # aussi .train() qui fait tout d'un coup quand --save-every est absent.
    if args.save_every and args.save_every > 0:
        steps_done = 0
        while steps_done < args.steps:
            chunk = min(args.save_every, args.steps - steps_done)
            trainer.total_steps = steps_done + chunk
            trainer.train()
            steps_done = int(trainer.history["steps"][-1]) if trainer.history["steps"] else steps_done + chunk
            agent.save(str(out_path))
            reward = trainer.history["episode_reward"][-1] if trainer.history["episode_reward"] else float("nan")
            print(f"[checkpoint] {steps_done}/{args.steps} pas — récompense moyenne {reward:.1f} — {out_path}", flush=True)
    else:
        trainer.log = log
        trainer.train()
        agent.save(str(out_path))

    h = trainer.history
    rewards = [r for r in h.get("episode_reward", []) if r == r]  # retire NaN
    print("", flush=True)
    print(f"Checkpoint écrit : {out_path}", flush=True)
    print(f"  pas totaux      : {h.get('steps', [])[-1] if h.get('steps') else args.steps}", flush=True)
    if rewards:
        last = rewards[-50:]
        print(f"  récompense moy. : {float(np.mean(last)):.1f} (50 derniers épisodes)", flush=True)
    print("Redémarrez le moteur IA pour charger le modèle : make run-ai", flush=True)
    print("(vérification : /healthz du backend affiche ai_model_loaded=true)", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
