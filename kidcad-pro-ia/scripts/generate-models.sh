#!/usr/bin/env bash
# ==============================================================
# KidCAD-Pro-IA — génération des checkpoints .pt initiaux
#   ai-engine/training/router/model_v1.pt     (ActorCritic, PPO)
#   ai-engine/training/placer/model_v1.pt     (GraphNet, GNN placement)
#   ai-engine/training/optimizer/model_v1.pt  (BoardViT, évaluateur)
#
# Ces modèles sont OPTIONNELS : sans torch, le moteur IA utilise le
# repli A* intégré et le service démarre normalement (contrat §6).
#
# Variables :
#   TORCH_PRESENT=1  torch est déjà installé (pas d'installation pip)
#   STRICT=1         échouer si torch est indisponible (défaut : tolerant)
# ==============================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

info() { printf '==> %s\n' "$1"; }
err()  { printf 'ERREUR : %s\n' "$1" >&2; }

# --------------------------------------------------------------
# 1. Environnement virtuel (créé si absent)
# --------------------------------------------------------------
PY=python3
if [ ! -x .venv/bin/python ]; then
    info "Création de l'environnement virtuel (.venv)"
    python3 -m venv .venv
fi
# shellcheck disable=SC1091
source .venv/bin/activate
PY=python

# --------------------------------------------------------------
# 2. Torch : présent ? sinon installation CPU ; sinon message clair
# --------------------------------------------------------------
if [ "${TORCH_PRESENT:-0}" != "1" ]; then
    if ! "$PY" -c "import torch" >/dev/null 2>&1; then
        info "torch absent : installation depuis l'index CPU PyTorch"
        "$PY" -m pip install --quiet torch --index-url https://download.pytorch.org/whl/cpu || true
    fi
fi

if ! "$PY" -c "import torch" >/dev/null 2>&1; then
    cat <<'EOF'
torch est indisponible : les checkpoints .pt ne peuvent pas etre generes.

Rappel : les modeles RL sont OPTIONNELS. Sans torch, le moteur IA
utilise le repli A* integre (ai-engine/src/environment) et le service
gRPC demarre normalement. Aucune action n'est requise pour developper
ou tester le backend et le frontend.

Pour generer les modeles plus tard :
  pip install torch --index-url https://download.pytorch.org/whl/cpu
  bash scripts/generate-models.sh
EOF
    if [ "${STRICT:-0}" = "1" ]; then
        err "STRICT=1 : abandon (torch requis)."
        exit 1
    fi
    exit 0
fi

# --------------------------------------------------------------
# 3. Instanciation + sauvegarde des trois réseaux (petites tailles)
#    router = ActorCritic | placer = GraphNet | optimizer = BoardViT
# --------------------------------------------------------------
info "Génération des checkpoints (canaux d'observation 5, 12 actions)"

mkdir -p ai-engine/training/router ai-engine/training/placer ai-engine/training/optimizer

cd ai-engine
PYTHONPATH=. "$PY" -c '
import importlib
import os
import torch

OUT = {
    "router": ("src.agents.ppo_agent", "ActorCritic"),
    "placer": ("src.models.graph_net", "GraphNet"),
    "optimizer": ("src.models.transformer", "BoardViT"),
}

def build(module_name, class_name):
    """Instancie le reseau en essayant plusieurs signatures courantes."""
    cls = getattr(importlib.import_module(module_name), class_name)
    attempts = [
        ((), {}),
        ((5, 12), {}),
        ((), {"obs_channels": 5, "n_actions": 12}),
        ((), {"in_channels": 5, "n_actions": 12}),
    ]
    last = "signature inconnue"
    for args, kwargs in attempts:
        try:
            return cls(*args, **kwargs)
        except TypeError as exc:
            last = str(exc)
    raise SystemExit("Instanciation impossible de %s (%s)" % (class_name, last))

for target, (module_name, class_name) in OUT.items():
    net = build(module_name, class_name)
    path = os.path.join("training", target, "model_v1.pt")
    torch.save(net.state_dict(), path)
    params = sum(p.numel() for p in net.parameters())
    size_kb = os.path.getsize(path) / 1024.0
    print("%-9s -> %-34s %8d parametres  %8.1f Ko" % (target, path, params, size_kb))

print("Checkpoints generes (poids initiaux aleatoires, entrainement requis).")
'
cd "$ROOT_DIR"

info "Terminé. Les fichiers .pt ne doivent pas être commités (voir .gitignore)."
