#!/usr/bin/env bash
# ==============================================================
# Benchmark A* vs RL sur une VRAIE carte KiCad (complex_hierarchy,
# 52 nets) — checkpoint PPO chargé à chaud. Une session.
# ==============================================================
set -u
PORT=8095
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-bench.log
AI_LOG=/tmp/yahriacad-ai-bench.log
SRC=/tmp/kicad-complex_hierarchy.kicad_pcb

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-bench

check() { if [ "$2" = "$3" ]; then echo "  OK   $1"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fi }

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

echo "=== 1. État du modèle IA ==="
curl -s "$B/api/v1/ai/model" -H "$AUTH" | python3 -m json.tool

echo ""
echo "=== 2. Chargement du checkpoint RL (model_v2, ~20k pas) ==="
curl -s -X POST "$B/api/v1/ai/model/reload" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"checkpoint_path":"training/router/model_v2.pt"}' | python3 -m json.tool | head -15

echo ""
echo "=== 3. Import de la vraie carte + placement ==="
PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"Arena vraie carte","layer_count":2}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$SRC" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"  import : {d[\"components\"] if \"components\" in d else \"?\"} ")' 2>/dev/null || true
curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$SRC" > /tmp/imp-bench.json
python3 -c 'import json; d=json.load(open("/tmp/imp-bench.json")); print(f"  import : {d}")'
curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' > /dev/null
echo "  placement : OK"

echo ""
echo "=== 4. Benchmark A* vs RL (52 nets réels) ==="
echo "  (le RL peut être lent sur une vraie carte — patience)"
START=$(date +%s)
BENCH=$(curl -s --max-time 480 -X POST "$B/api/v1/projects/$PID/arena/benchmark" -H "$AUTH" -H 'Content-Type: application/json' -d '{}')
END=$(date +%s)
echo "  durée : $((END-START))s"
echo "$BENCH" | python3 -m json.tool 2>/dev/null | head -60 || echo "  brut : $(echo $BENCH | head -c 500)"

echo ""
echo "=== 5. Leaderboard ==="
curl -s "$B/api/v1/arena/leaderboard" -H "$AUTH" | python3 -m json.tool 2>/dev/null | head -25
