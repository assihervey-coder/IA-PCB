"""Client socket.io Python — comment brancher un modèle RL entraîné sur YahriaCad.

RÔLE (documenté, non requis en production) :
En production, les jobs IA (`ai:place`, `ai:route`, `ai:optimize`) sont traités par
le moteur TypeScript déterministe du mini-service `mini-services/ai-engine`
(port 3010, voir docs/architecture/adr-002-moteur-ia.md). Ce script montre,
à titre de démonstration R&D, comment un **service Python** servant une
politique PPO entraînée (checkpoints .pt de ai-engine/) pourrait **remplacer**
ce moteur : le protocole événementiel reste strictement identique côté client.

Prérequis : pip install python-socketio requests
Lancement (exemple) : python src/service.py --url http://localhost:3110
"""

from __future__ import annotations

import argparse
import logging
from typing import Any, Dict

import socketio  # python-socketio

logging.basicConfig(level=logging.INFO, format="[ai-rl] %(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("yahriacad-ai-rl")

# Types partagés : miroir Python de src/lib/yahriacad/shared/types.ts (contrat JSON).
AiJobOptions = Dict[str, Any]
Design = Dict[str, Any]


class AiRlService:
    """Mini-service socket.io exposant une politique RL pour ai:place / ai:route.

    Événements gérés (identiques au moteur TS) :
        entrée  : ai:place {design, options} | ai:route {design, options} | ai:optimize
        sortie  : ai:progress {stage, progress, message} | ai:result {design, stats}
                  | ai:error {code, message}
    """

    def __init__(self, checkpoint: str | None = None) -> None:
        self.checkpoint = checkpoint
        self.model = self._load_model(checkpoint)
        self.sio = socketio.Client()

        @self.sio.on("connect")
        def _on_connect() -> None:
            logger.info("Connecté à la gateway YahriaCad")

        @self.sio.on("ai:place")
        def _on_place(data: Dict[str, Any]) -> None:
            self._run_job("placement", data)

        @self.sio.on("ai:route")
        def _on_route(data: Dict[str, Any]) -> None:
            self._run_job("routage", data)

    # ------------------------------------------------------------------ modèle
    def _load_model(self, checkpoint: str | None):
        """Charge une politique PPO (référence : src/agents/ppo_agent.py)."""
        if not checkpoint:
            logger.warning("Aucun checkpoint fourni — service en mode démo (heuristic).")
            return None
        try:
            import torch  # torch est requis uniquement pour un vrai modèle

            ckpt = torch.load(checkpoint, map_location="cpu", weights_only=False)
            logger.info("Modèle chargé : %s (steps=%s)", checkpoint, ckpt.get("total_steps"))
            return ckpt
        except Exception as exc:  # pragma: no cover
            logger.error("Chargement impossible (%s) — moteur TS requis en prod.", exc)
            return None

    # ------------------------------------------------------------------ jobs
    def _run_job(self, stage: str, data: Dict[str, Any]) -> None:
        """Exécute un job en émettant une progression réaliste puis un résultat."""
        design: Design = data.get("design", {})
        options: AiJobOptions = data.get("options", {})
        nets = design.get("layoutJson", {}).get("tracks", []) or []
        total = max(len(nets), 1)

        # 1) progression (streaming, identique au moteur TS)
        for step in range(0, 101, 20):
            self.sio.emit(
                "ai:progress",
                {
                    "stage": stage,
                    "progress": step,
                    "message": f"[RL {stage}] étape {step} % (checkpoint={self.checkpoint or 'demo'})",
                },
            )

        # 2) résultat : ICI on réutiliserait la politique (PpoAgent.select_action)
        #    pour ordonner les nets / choisir les couches, puis on renverrait le
        #    design modifié. En mode démo on renvoie le design inchangé.
        result = dict(design)
        self.sio.emit(
            "ai:result",
            {
                "design": result,
                "stats": {
                    "engine": "python-rl-reference",
                    "stage": stage,
                    "seed": options.get("seed"),
                    "netsTouched": total,
                    "note": "Mode démonstration — production = moteur TS déterministe",
                },
            },
        )
        logger.info("Job '%s' terminé (%d nets touchés).", stage, total)

    # ------------------------------------------------------------------ service
    def serve_forever(self, url: str) -> None:
        """Se connecte à la gateway (socket.io) et boucle jusqu'à interruption."""
        logger.info("Connexion à %s …", url)
        self.sio.connect(url, wait=True, retry=True)
        try:
            self.sio.wait()
        except KeyboardInterrupt:
            logger.info("Arrêt du service RL.")
        finally:
            self.sio.disconnect()


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Service RL de référence (remplacement documenté du moteur TS)."
    )
    parser.add_argument("--url", default="http://localhost:3110",
                        help="URL de la gateway socket.io (test : mini serveur dédié)")
    parser.add_argument("--checkpoint", default=None, help="Chemin d'un checkpoint .pt (optionnel)")
    args = parser.parse_args()

    service = AiRlService(checkpoint=args.checkpoint)
    service.serve_forever(args.url)


if __name__ == "__main__":
    main()
