#!/usr/bin/env bash
# ==============================================================
# KidCAD-Pro-IA — compilation de l'ensemble des modules
#   backend Go      -> bin/kidcad-server
#   ai-engine Python-> vérification syntaxique (compileall)
#   frontend Next   -> frontend/.next
# ==============================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

info() { printf '==> %s\n' "$1"; }
ok()   { printf '    OK : %s\n' "$1"; }
skip() { printf '    IGNORÉ : %s\n' "$1"; }

failures=0

# --------------------------------------------------------------
# 1. Backend Go
# --------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
    info "Compilation du backend Go"
    mkdir -p bin
    if go build -ldflags "-s -w" -o bin/kidcad-server ./backend/cmd/kidcad-server; then
        ok "bin/kidcad-server"
    else
        printf '    ÉCHEC : compilation backend\n' >&2
        failures=$((failures + 1))
    fi
else
    skip "Go introuvable, backend non compilé."
    failures=$((failures + 1))
fi

# --------------------------------------------------------------
# 2. Moteur IA Python
# --------------------------------------------------------------
if command -v python3 >/dev/null 2>&1; then
    info "Vérification syntaxique du moteur IA Python (compileall)"
    if [ -d ai-engine ]; then
        if python3 -m compileall -q ai-engine; then
            ok "ai-engine : syntaxe vérifiée"
        else
            printf '    ÉCHEC : erreurs de syntaxe dans ai-engine\n' >&2
            failures=$((failures + 1))
        fi
    else
        skip "répertoire ai-engine/ absent."
    fi
else
    skip "python3 introuvable."
    failures=$((failures + 1))
fi

# --------------------------------------------------------------
# 3. Frontend Next.js
# --------------------------------------------------------------
if command -v npm >/dev/null 2>&1; then
    if [ -d frontend ]; then
        info "Compilation du frontend Next.js"
        if (cd frontend && npm run build); then
            ok "frontend/.next"
        else
            printf '    ÉCHEC : build frontend\n' >&2
            failures=$((failures + 1))
        fi
    else
        skip "répertoire frontend/ absent."
    fi
else
    skip "npm introuvable, frontend non compilé."
    failures=$((failures + 1))
fi

# --------------------------------------------------------------
# Résumé
# --------------------------------------------------------------
echo
if [ "$failures" -eq 0 ]; then
    info "Compilation terminée sans erreur : backend, ai-engine, frontend."
else
    printf '==> Compilation terminée avec %d échec(s). Corrigez les erreurs ci-dessus.\n' "$failures" >&2
    exit 1
fi
