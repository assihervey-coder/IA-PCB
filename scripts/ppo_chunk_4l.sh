#!/usr/bin/env bash
# Chunk PPO 4 couches — un appel = un chunk résilient (~7 min au premier plan).
# Recette fine-tune (Task 21) : lr 1e-4, ent 0,005, vf 0,05, epochs 2, rollout 64.
# Usage : ppo_chunk_4l.sh <init.pt> <out.pt> <seed> [steps=4000]
set -euo pipefail
cd /home/z/my-project/yahriacad/ai-engine
INIT="$1"; OUT="$2"; SEED="$3"; STEPS="${4:-4000}"
OMP_NUM_THREADS=4 exec python3 training/router/train.py \
  --steps "$STEPS" --board realistic --layers 4 --seed "$SEED" \
  --init-from "$INIT" --lr 1e-4 --ent-coef 0.005 --vf-coef 0.05 \
  --epochs 2 --rollout 64 --out "$OUT"
