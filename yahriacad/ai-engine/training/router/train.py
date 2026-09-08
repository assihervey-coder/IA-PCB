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
                        help="longueur d'un rollout (défaut : 256 synthétique, "
                             "64 cartes réalistes/mixtes — RAM des grandes grilles)")
    parser.add_argument("--out", default="training/router/model_v1.pt",
                        help="chemin du checkpoint de sortie (relatif à ai-engine/)")
    parser.add_argument("--save-every", type=int, default=0,
                        help="sauvegarde intermédiaire tous les N pas (0 = final seul)")
    parser.add_argument("--init-from", default=None,
                        help="checkpoint de départ (fine-tuning PPO depuis un "
                             "checkpoint BC — le meilleur des deux mondes)")
    parser.add_argument("--layers", type=int, default=2,
                        help="nombre de couches cuivre des cartes réalistes "
                             "(défaut 2 ; 4 = curriculum 4 couches, observation "
                             "8 canaux — doit correspondre aux canaux du "
                             "checkpoint --init-from, torch.load est strict)")
    parser.add_argument("--arch", choices=["auto", "base", "compass", "wide"],
                        default="auto",
                        help="architecture du réseau : auto = suit l'archi "
                             "du checkpoint --init-from (défaut, rétro-compat) ; "
                             "base = CNN historique ; compass = boussole cible "
                             "(2 canaux dx/dy vers la cible + tête 1x1) ; "
                             "wide = tronc élargi 128 canaux. Une archi forcée "
                             "différente du checkpoint --init-from déclenche "
                             "un chargement PARTIEL (tenseurs compatibles "
                             "transférés seulement)")
    parser.add_argument("--board", choices=["synthetic", "realistic", "mixed"],
                        default="synthetic",
                        help="distribution d'environnements : synthetic = petites "
                             "cartes historiques ; realistic = curriculum 0,25 mm à "
                             "l'échelle réelle ; mixed = alternance des deux")
    parser.add_argument("--lr", type=float, default=None,
                        help="taux d'apprentissage PPO (défaut : config ; "
                             "1e-4 conseillé en fine-tuning)")
    parser.add_argument("--ent-coef", type=float, default=None,
                        help="coefficient d'entropie (défaut : config ; "
                             "0.005 conseillé en fine-tuning depuis BC)")
    parser.add_argument("--vf-coef", type=float, default=None,
                        help="poids de la perte de valeur (0.05 conseillé en "
                             "fine-tuning depuis BC : la tête de valeur d'un "
                             "checkpoint BC est aléatoire — avec 0.5, ses "
                             "gradients détruisent le tronc partagé)")
    parser.add_argument("--epochs", type=int, default=None,
                        help="epochs PPO par mise à jour (2 conseillé en fine-tuning)")
    args = parser.parse_args(argv)

    _require_torch()

    import numpy as np
    from evaluation.bench import make_realistic_board, make_synthetic_board
    from src.agents.ppo_agent import PPOAgent, PPOConfig, PPOTrainer
    from src.environment.pcb_env import EnvConfig, PCBRouteEnv

    cfg = PPOConfig()
    cfg.seed = args.seed
    if args.lr is not None:
        cfg.lr = args.lr
    if args.ent_coef is not None:
        cfg.ent_coef = args.ent_coef
    if args.vf_coef is not None:
        cfg.vf_coef = args.vf_coef
    if args.epochs is not None:
        cfg.epochs = args.epochs

    board_makers = {
        "synthetic": make_synthetic_board,
        "realistic": make_realistic_board,
    }

    def board_for(k: int) -> tuple:
        if args.board == "mixed":
            if k % 2:
                return board_makers["realistic"](args.seed * 1000 + k,
                                                 layer_count=args.layers)
            return board_makers["synthetic"](args.seed * 1000 + k,
                                             layer_count=args.layers)
        if args.board == "realistic":
            return board_makers["realistic"](args.seed * 1000 + k,
                                             layer_count=args.layers)
        return board_makers["synthetic"](args.seed * 1000 + k,
                                         layer_count=args.layers)

    probe_env = PCBRouteEnv(*board_for(0),
                            EnvConfig(clearance_cells=1, seed=args.seed))
    in_channels = probe_env.observation_shape[0]
    arch = "base" if args.arch == "auto" else args.arch
    agent = PPOAgent(cfg, in_channels=in_channels, n_actions=12, device="cpu",
                     arch=arch)
    if args.init_from:
        follow = args.arch == "auto"
        if not agent.load(args.init_from, follow_arch=follow):
            sys.exit(f"checkpoint initial illisible : {args.init_from}")
        print(f"[init-from] politique initialisée depuis {args.init_from} "
              f"(archi={agent.arch}, "
              f"{'strict/suivi' if follow else 'partiel/transfert'})", flush=True)

    rollout_counter = {"k": 0}

    def env_factory() -> PCBRouteEnv:
        board, nets = board_for(rollout_counter["k"])
        rollout_counter["k"] += 1
        return PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=args.seed))

    # Longueur de rollout par défaut adaptée à la taille des grilles : les
    # cartes réalistes (grilles 300x400) consomment ~3 Mo par pas d'obs —
    # 256 pas feraient exploser la RAM du buffer.
    rollout_len = args.rollout
    if rollout_len is None:
        rollout_len = 256 if args.board == "synthetic" else 64

    trainer = PPOTrainer(env_factory, agent, total_steps=args.steps,
                         rollout_len=rollout_len)

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
