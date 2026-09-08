#!/usr/bin/env bash
# ==============================================================
# IL suite ① : génération de démonstrations par lots.
# Usage :
#   bash scripts/il_gen.sh real <probes> <out.npz>          # vraie carte + sondes
#   bash scripts/il_gen.sh rscale <seed> <n_boards> <out.npz> # curriculum réaliste
#   bash scripts/il_gen.sh synth <seed> <n_boards> <out.npz>  # petites cartes
# Une session bash = un lot (< 600 s, contrainte sandbox).
# ==============================================================
set -eu
PHASE="$1"
AIROOT=/home/z/my-project/yahriacad/ai-engine
cd "$AIROOT"

case "$PHASE" in
  real)
    PROBES="$2"; OUT="$3"
    python3 training/router/imitation.py generate \
      --input /tmp/yahriacad-imitation/route_input.json \
      --probes-real "$PROBES" --out "$OUT" 2>&1 | sed 's/^/  /'
    ;;
  rscale)
    SEED="$2"; N="$3"; OUT="$4"
    python3 training/router/imitation.py generate \
      --synthetic-real "$N" --synthetic-seed "$SEED" --probes 24 \
      --out "$OUT" 2>&1 | sed 's/^/  /'
    ;;
  synth)
    SEED="$2"; N="$3"; OUT="$4"
    python3 training/router/imitation.py generate \
      --synthetic "$N" --synthetic-seed "$SEED" --probes 40 \
      --out "$OUT" 2>&1 | sed 's/^/  /'
    ;;
  *) echo "phase inconnue: $PHASE"; exit 1 ;;
esac

ls -la "$AIROOT/$OUT" 2>/dev/null || ls -la "$OUT" 2>/dev/null
echo "GEN $PHASE OK"
