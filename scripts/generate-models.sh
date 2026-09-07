#!/usr/bin/env bash
# =============================================================================
# YahriaCad — Entraînement RL OPTIONNEL (pipeline R&D Python)
# Usage : ./scripts/generate-models.sh
# Crée un venv Python, installe ai-engine/requirements.txt (torch, numpy, tqdm,
# matplotlib, tensorboard), puis lance ai-engine/train.py s'il existe.
# ⚠️  LES MODÈLES .pt NE SONT PAS REQUIS POUR L'APPLICATION : l'inférence en
#     production utilise le moteur TypeScript déterministe embarqué
#     (A* + recuit simulé, src/lib/yahriacad/ai-engine). Ce script ne sert qu'à la
#     recherche (PPO + GNN, voir ai-engine/README.md).
# =============================================================================
set -euo pipefail

BOLD=$'\033[1m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RESET=$'\033[0m'

info() { echo "${GREEN}[rl-train]${RESET} $*"; }
warn() { echo "${YELLOW}[rl-train]${RESET} $*"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
AI_DIR="$ROOT_DIR/ai-engine"

cd "$AI_DIR"

# --- Python disponible ? -------------------------------------------------------
if ! command -v python3 >/dev/null 2>&1; then
    warn "python3 introuvable."
    warn "Les modèles .pt ne sont pas requis pour l'inférence — moteur TS embarqué."
    warn "L'application fonctionne parfaitement sans ce pipeline (voir ai-engine/README.md)."
    exit 0
fi

# --- Dépendance torch présente ? ------------------------------------------------
if ! python3 -c "import torch" >/dev/null 2>&1; then
    info "PyTorch absent : création d'un venv et installation des dépendances…"
    if [ ! -d "venv" ]; then
        python3 -m venv venv
    fi
    # shellcheck disable=SC1091
    source venv/bin/activate
    pip install --upgrade pip
    pip install -r requirements.txt
else
    warn "PyTorch déjà disponible dans l'environnement courant — venv non recréé."
    [ -d "venv" ] && source venv/bin/activate || true
fi

# --- Lancement de l'entraînement ------------------------------------------------
if [ -f "train.py" ]; then
    info "Lancement de l'entraînement RL (ai-engine/train.py)…"
    warn "C'est long (heures sur CPU, sans GPU). Ctrl+C pour interrompre."
    python train.py --config training/router/config.yaml
    info "Modèles écrits dans training/router/model_v1.pt (et itérations suivantes)."
else
    warn "ai-engine/train.py n'est pas encore fourni."
    warn "Les modèles .pt ne sont pas requis pour l'inférence — moteur TS embarqué."
    warn "Le pipeline de référence (environment, agent PPO, GNN, benchmark) est dans"
    warn "ai-engine/src/ et s'utilise via : ai-engine/evaluation/bench.py"
    warn "Exemple : python evaluation/bench.py --grids 20 --agent random"
fi

echo ""
info "✅ Terminé. Rappel : l'application n'a PAS besoin des modèles .pt pour tourner."
