#!/usr/bin/env bash
# ==============================================================
# Démarrage SERVEUR YahriaCad (stack complète)
#   1. Moteur IA Python  (gRPC 50051)   — démarré EN PREMIER
#   2. Backend Go        (HTTP 8080)    — le frontend 3000 proxyfie /api/v1 → :8080
#   3. Frontend Next.js  (port 3000)    — port standard des previews space-z.ai
# NB : UNE SEULE instance next-server (limite mémoire 4 Go du sandbox —
# deux instances => OOM killer, leçon Task 13).
# Leçon worklog Task 11 : purge des orphelins AVANT tout,
# ordre strict moteur IA → attente bind → backend.
# Usage : bash /home/z/my-project/scripts/start_server.sh
# ==============================================================
set -u

export PATH=/home/z/toolchain/go/bin:$PATH
REPO=/home/z/my-project/yahriacad
BIN=/tmp/yahriacad-live-server
AI_LOG=/tmp/yahriacad-ai-live.log
SRV_LOG=/tmp/yahriacad-live.log
DATA_DIR=/home/z/my-project/yahriacad-data
PORT=8080

echo "=== 0. Purge des orphelins (sessions précédentes) ==="
pkill -f 'cmd/ai-server/main.py' 2>/dev/null && { echo "  moteur IA orphelin tué"; sleep 1; }
pkill -f 'yahriacad-live-server' 2>/dev/null && { echo "  backend orphelin tué"; sleep 1; }
python3 -c "import socket; s=socket.create_connection(('127.0.0.1',50051),1); s.close()" 2>/dev/null \
  && { echo "  50051 encore occupé, attente…"; sleep 2; } || echo "  50051 libre"

echo "=== 1. Compilation du backend (HEAD) ==="
cd "$REPO"
# Toolchain Go : emplacements connus (toolchain externe OU local persistant .tools/go)
if ! command -v go >/dev/null 2>&1; then
  for GD in /home/z/toolchain/go /home/z/my-project/.tools/go; do
    [ -x "$GD/bin/go" ] && export PATH="$GD/bin:$GD/packages/bin:$PATH" && break
  done
fi
command -v go >/dev/null 2>&1 || { echo "go introuvable (installer dans .tools/go)"; exit 1; }
echo "  toolchain : $(command -v go) [$(go version | awk '{print $3}')]"
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || { echo "COMPILATION KO"; exit 1; }
echo "  binaire prêt : $BIN"

echo "=== 2. Démarrage moteur IA (gRPC 50051) ==="
cd "$REPO/ai-engine"
nohup python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 &
AI_PID=$!
disown "$AI_PID" 2>/dev/null
echo "$AI_PID" > /tmp/yahriacad-ai-live.pid
echo "  pid=$AI_PID (log: $AI_LOG)"
ai_ready=0
for _ in $(seq 1 60); do
  python3 -c "import socket; s=socket.create_connection(('127.0.0.1',50051),1); s.close()" 2>/dev/null && { ai_ready=1; break; }
  sleep 0.5
done
[ "$ai_ready" = "1" ] && echo "  moteur IA à l'écoute sur 50051 ✓" || { echo "  ÉCHEC moteur IA — log:"; tail -15 "$AI_LOG"; exit 1; }

echo "=== 3. Démarrage backend Go (port $PORT) ==="
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=yahriacad-live-2026-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=$DATA_DIR
cd "$REPO"
nohup "$BIN" > "$SRV_LOG" 2>&1 &
SRV_PID=$!
disown "$SRV_PID" 2>/dev/null
echo "$SRV_PID" > /tmp/yahriacad-live.pid
echo "  pid=$SRV_PID (log: $SRV_LOG)"
ready=0
for _ in $(seq 1 40); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/healthz" 2>/dev/null)
  [ "$code" = "200" ] && { ready=1; break; }
  sleep 0.5
done
[ "$ready" = "1" ] && echo "  backend healthz 200 ✓" || { echo "  ÉCHEC backend — log:"; tail -20 "$SRV_LOG"; exit 1; }

echo "=== 4. Frontend Next.js (3000) — état ==="
FE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/ 2>/dev/null)
echo "  http://localhost:3000/ → $FE"
if [ "$FE" != "200" ]; then
  echo "  frontend absent — démarrage…"
  cd "$REPO/frontend"
  nohup npx next start -p 3000 > /tmp/yahriacad-front.log 2>&1 &
  disown "$!" 2>/dev/null
  for _ in $(seq 1 60); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/)" = "200" ] && { echo "  frontend démarré ✓"; break; }
    sleep 1
  done
fi

echo ""
echo "=== 5. Vérification bout-en-bout ==="
TOKEN=$(curl -s -X POST "http://localhost:$PORT/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
[ -n "$TOKEN" ] && echo "  login admin:admin → JWT ✓" || { echo "  login KO"; tail -20 "$SRV_LOG"; exit 1; }
PROXY=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/api/v1/projects -H "Authorization: Bearer $TOKEN")
echo "  proxy frontend→backend (/api/v1/projects via 3000) → $PROXY"
HEALTH=$(curl -s "http://localhost:$PORT/healthz")
echo "  healthz : $HEALTH"
echo ""
echo "STACK COMPLÈTE OPÉRATIONNELLE"
echo "  Moteur IA   : gRPC 50051 (pid $(cat /tmp/yahriacad-ai-live.pid))"
echo "  Backend API : http://localhost:$PORT (pid $(cat /tmp/yahriacad-live.pid))"
echo "  Frontend    : http://localhost:3000"
echo "  Identifiants: admin / admin"
