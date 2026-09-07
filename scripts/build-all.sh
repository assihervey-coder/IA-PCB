#!/usr/bin/env bash
# =============================================================================
# YahriaCad — Build de production de tout le monorepo
# Usage : ./scripts/build-all.sh
# 1) Lint ESLint
# 2) Build Next.js (output standalone)
# 3) Le mini-service IA (Bun) ne nécessite pas de build (interprété) : la ligne
#    est volontairement commentée / décrite pour un futur bundler éventuel.
# =============================================================================
set -euo pipefail

BOLD=$'\033[1m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RESET=$'\033[0m'

info() { echo "${GREEN}[build]${RESET} $*"; }
warn() { echo "${YELLOW}[build]${RESET} $*"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$ROOT_DIR"

# --- 1. Lint ------------------------------------------------------------------
info "Lint ESLint…"
bun run lint
info "Lint : OK"

# --- 2. Build Next.js ----------------------------------------------------------
info "Build Next.js (output: standalone)…"
bun run build
info "Build Next.js : OK → .next/standalone/server.js"
warn "Pensez à copier .next/static et public dans .next/standalone/"
warn "(déjà fait automatiquement par le script npm 'build' du package.json)."

# --- 3. Mini-service IA ---------------------------------------------------------
# Le service IA est exécuté directement par Bun (bun run index.ts) : aucun
# artefact de build n'est requis aujourd'hui. Pour produire un binaire autonome
# un jour :
#   bun build ./mini-services/ai-engine/index.ts --compile --outfile dist/ai-engine
warn "Service IA : exécution directe Bun (aucun build nécessaire) — voir commande commentée ci-dessus pour un binaire compilé."

echo ""
info "✅ Build complet terminé."
echo "   App   : bun .next/standalone/server.js  (port 3000)"
echo "   IA    : cd mini-services/ai-engine && bun run dev  (port 3010)"
