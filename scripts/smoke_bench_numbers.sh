#!/usr/bin/env bash
# ==============================================================
# Benchmark A* vs RL chiffré (carte démo 6 nets) avec le checkpoint
# model_v2 (~20k pas) chargé à chaud. Une session.
# ==============================================================
set -u
PORT=8096
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-bench2.log
AI_LOG=/tmp/yahriacad-ai-bench2.log

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-bench2

cd /home/z/my-project/yahriacad
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || exit 1
cd ai-engine && python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 & AI=$!
cd ..
"$BIN" > "$LOG" 2>&1 & SRV=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
for _ in $(seq 1 40); do python3 -c "import socket; socket.create_connection(('127.0.0.1',50051),1).close()" 2>/dev/null && break; sleep 0.5; done
for _ in $(seq 1 40); do curl -sf -o /dev/null "$B/healthz" && break; sleep 0.5; done

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"

echo "=== Chargement model_v2 (~20k pas) ==="
curl -s -X POST "$B/api/v1/ai/model/reload" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"checkpoint_path":"training/router/model_v2.pt"}' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(" ", d.get("message","?"))'

echo ""
echo "=== Carte démo nightmare (6 nets, 5 composants) ==="
PID=$(curl -s -X POST "$B/api/v1/demo/nightmare" -H "$AUTH" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("project_id",""))')
echo "  projet : $PID"

START=$(date +%s)
BENCH=$(curl -s --max-time 300 -X POST "$B/api/v1/projects/$PID/arena/benchmark" -H "$AUTH" -H 'Content-Type: application/json' -d '{}')
DUR=$(( $(date +%s) - START ))
echo "  benchmark : ${DUR}s"
echo "$BENCH" | python3 -m json.tool 2>/dev/null || echo "  brut : $(echo $BENCH | head -c 400)"

echo ""
echo "=== Leaderboard après le match ==="
curl -s "$B/api/v1/arena/leaderboard" -H "$AUTH" | python3 -m json.tool 2>/dev/null | head -30
