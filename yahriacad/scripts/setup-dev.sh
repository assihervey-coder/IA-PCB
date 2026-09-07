#!/usr/bin/env bash
# ==============================================================
# YahriaCad — installation de l'environnement de développement
# Vérifie la toolchain, installe les dépendances Go / Python / Node
# et génère les stubs gRPC. Aucun emoji, sortie lisible en français.
# ==============================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

err() { printf 'ERREUR : %s\n' "$1" >&2; }
info() { printf '==> %s\n' "$1"; }

# --------------------------------------------------------------
# 1. Vérification de la toolchain
# --------------------------------------------------------------
missing=0
check_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        err "« $1 » est introuvable. Installez-le avant de continuer : $2"
        missing=1
    fi
}
check_cmd go      "https://go.dev/dl/ (Go >= 1.22)"
check_cmd python3 "https://www.python.org/downloads/ (Python >= 3.10)"
check_cmd node    "https://nodejs.org (Node >= 20)"
check_cmd npm     "https://nodejs.org (fourni avec Node)"

if [ "$missing" -ne 0 ]; then
    err "Toolchain incomplète : corrigez les points ci-dessus puis relancez le script."
    exit 1
fi

info "Go       : $(go version)"
info "Python   : $(python3 --version)"
info "Node     : $(node --version)"
info "npm      : $(npm --version)"

# --------------------------------------------------------------
# 2. Environnement virtuel Python + dépendances
# --------------------------------------------------------------
if [ ! -d .venv ]; then
    info "Création de l'environnement virtuel Python (.venv)"
    python3 -m venv .venv
fi
# shellcheck disable=SC1091
source .venv/bin/activate
info "Mise à jour de pip"
python3 -m pip install --quiet --upgrade pip
info "Installation des dépendances Python (requirements.txt + grpcio-tools)"
python3 -m pip install --quiet -r requirements.txt grpcio-tools

# --------------------------------------------------------------
# 3. Dépendances Go
# --------------------------------------------------------------
info "Téléchargement des modules Go (go mod download)"
go mod download

# --------------------------------------------------------------
# 4. Dépendances frontend
# --------------------------------------------------------------
if [ -d frontend ]; then
    info "Installation des dépendances frontend (npm install)"
    (cd frontend && npm install)
else
    info "Répertoire frontend/ absent : étape ignorée."
fi

# --------------------------------------------------------------
# 5. Génération des stubs gRPC (Python + Go si plugins présents)
# --------------------------------------------------------------
if [ -f Makefile ]; then
    info "Génération des stubs gRPC (make proto)"
    make proto
else
    info "Makefile absent : génération des stubs ignorée."
fi

cat <<'EOF'

Environnement prêt. Prochaines étapes :

  source .venv/bin/activate          # activer le venv Python
  make run-ai                        # moteur IA gRPC  (localhost:50051)
  make run-backend                   # backend Go      (localhost:8080)
  make run-frontend                  # frontend Next   (localhost:3000)

Ou en Docker : docker compose -f docker/docker-compose.yml up -d --build
Documentation : docs/guides/guide-demarrage-rapide.md
EOF
