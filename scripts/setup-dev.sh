#!/usr/bin/env bash
# =============================================================================
# KidCAD-Pro-IA — Installation de l'environnement de développement
# Usage : ./scripts/setup-dev.sh
# Installe les dépendances Bun (racine + mini-services/ai-engine) puis pousse
# le schéma Prisma vers SQLite (db/custom.db).
# =============================================================================
set -euo pipefail

# --- Couleurs -----------------------------------------------------------------
BOLD=$'\033[1m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RED=$'\033[31m'
RESET=$'\033[0m'

info()  { echo "${GREEN}[setup]${RESET} $*"; }
warn()  { echo "${YELLOW}[setup]${RESET} $*"; }
error() { echo "${RED}[setup]${RESET} $*" >&2; }

# --- Racine du dépôt (indépendante du cwd) ------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$ROOT_DIR"

# --- Vérification de Bun ------------------------------------------------------
if ! command -v bun >/dev/null 2>&1; then
    error "Bun n'est pas installé. Rendez-vous sur https://bun.sh (curl -fsSL https://bun.sh/install | bash)"
    exit 1
fi
info "Bun détecté : $(bun --version)"

# --- Dépendances racine (Next.js, Prisma, socket.io, Three.js…) ----------------
info "Installation des dépendances de la racine…"
bun install

# --- Dépendances du mini-service IA (socket.io, port 3010) --------------------
if [ -d "mini-services/ai-engine" ]; then
    info "Installation des dépendances du mini-service IA (mini-services/ai-engine)…"
    (cd mini-services/ai-engine && bun install)
else
    warn "mini-services/ai-engine introuvable — ignoré."
fi

# --- Base de données (Prisma → SQLite) ----------------------------------------
info "Application du schéma Prisma (db/custom.db)…"
bun run db:push

echo ""
info "✅ Environnement prêt !"
echo ""
echo "  ${BOLD}Pour démarrer :${RESET}"
echo "    1) Terminal 1 : ${GREEN}bun run dev${RESET}                          → app Next.js sur http://localhost:3000"
echo "    2) Terminal 2 : ${GREEN}cd mini-services/ai-engine && bun run dev${RESET} → service IA sur :3010"
echo ""
echo "  Vérification : curl http://localhost:3000/api/health"
