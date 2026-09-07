#!/usr/bin/env bash
# ==============================================================
# Imitation learning (BC de A*) — STAGE 1 : dump vraie carte
# → génération des démonstrations → entraînement.
# Usage : bash scripts/smoke_imitation_train.sh
# ==============================================================
set -u
PORT=8095
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-imit-server
LOG=/tmp/yahriacad-imit.log
AI_LOG=/tmp/yahriacad-imit-ai.log
WORK=/tmp/yahriacad-imitation
DUMP="$WORK/dump"
BOARD=/tmp/kicad-complex_hierarchy.kicad_pcb
AIROOT=/home/z/my-project/yahriacad/ai-engine

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=imit-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-imit
export YAHRIACAD_DUMP_ROUTE_INPUT="$DUMP"

mkdir -p "$WORK"
# Purge des moteurs/servers orphelins des sessions précédentes : ils
# partagent le port 50051 via SO_REUSEPORT et interceptent les requêtes.
pkill -f "cmd/ai-server/main.py" 2>/dev/null; pkill -f yahriacad 2>/dev/null; sleep 1
fails=0
check() {
  if [ "$2" = "$3" ]; then echo "  OK   $1"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

# ---------- services ----------
cd /home/z/my-project/yahriacad
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || { echo "BUILD KO"; exit 1; }
# Ordre critique : moteur d'abord, attente stricte du bind, PUIS backend —
# sinon la connexion gRPC du backend entre en backoff et le premier RPC
# tombe en "connection refused".
cd ai-engine
python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 & AI=$!
cd ..
AI_OK=0
for _ in $(seq 1 90); do
  python3 -c "import socket; socket.create_connection(('127.0.0.1',50051),1).close()" 2>/dev/null && { AI_OK=1; break; }; sleep 0.5
done
[ "$AI_OK" = "1" ] || { echo "moteur IA jamais prêt sur 50051"; exit 1; }
"$BIN" > "$LOG" 2>&1 & SRV=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
for _ in $(seq 1 60); do curl -sf -o /dev/null "$B/healthz" && break; sleep 0.5; done
echo "services prêts"

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"
[ -n "$TOKEN" ] && check "login JWT" ok ok || check "login JWT" ok KO

echo "--- 1. Import vraie carte + routage A* (déclenche le dump) ---"
PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"Imitation complex_hierarchy","description":"BC source","layer_count":2}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
RESP=$(curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$BOARD;type=application/octet-stream")
NCOMP=$(echo "$RESP" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d.get("components",[])))' 2>/dev/null)
check "import ($NCOMP composants)" 68 "$NCOMP"

curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' > /dev/null
JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))')
for _ in $(seq 1 120); do
  ST=$(curl -s "$B/api/v1/projects/$PID/jobs/$JOB" -H "$AUTH" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
  [ "$ST" = "done" ] || [ "$ST" = "failed" ] && break
  sleep 1
done
check "routage A* complet (state=$ST)" done "$ST"

if [ -f "$DUMP/route_input.json" ]; then
  cp "$DUMP/route_input.json" "$WORK/route_input.json"
  check "dump route_input.json copié" ok ok
  python3 -c "import json; d=json.load(open('$WORK/route_input.json')); print('  board', d['board']['width_mm'], 'x', d['board']['height_mm'], '-', len(d['nets']), 'nets')"
else
  check "dump route_input.json" ok KO
fi

echo "--- 2. Génération des démonstrations (vraie carte + curriculum synthétique) ---"
cd "$AIROOT"
python3 training/router/imitation.py generate \
  --input "$WORK/route_input.json" --probes-real 0 \
  --synthetic 32 --probes 40 \
  --out training/router/demos_real.npz 2>&1 | sed 's/^/  /'

echo "--- 3. Entraînement BC (warm-start DAgger, budget 330 s) ---"
python3 training/router/imitation.py train \
  --demos training/router/demos_real.npz \
  --out training/router/model_bc_real.pt \
  --epochs 6 --batch 128 --lr 3e-4 --time-budget 330 \
  --init-from training/router/model_dag.pt 2>&1 | sed 's/^/  /'

ls -la training/router/model_bc_real.pt 2>/dev/null && check "checkpoint model_bc_real.pt" ok ok

echo ""
[ "$fails" = "0" ] && echo "SMOKE TRAIN OK" || echo "SMOKE TRAIN : $fails echec(s)"
exit $fails
