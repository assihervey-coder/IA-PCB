#!/usr/bin/env bash
# ==============================================================
# Redémarrage CHIRURGICAL du backend Go YahriaCad (port 8080)
# — sans toucher au moteur IA (50051) ni au frontend (3000).
# Règle Task 13-b : le processus doit être lancé DEPUIS CE SCRIPT
# (nohup + disown) pour survivre à la coupure de la session bash.
# Usage : bash /home/z/my-project/scripts/start_backend.sh
# ==============================================================
set -u
export PATH=/home/z/toolchain/go/bin:$PATH
REPO=/home/z/my-project/yahriacad
BIN=/tmp/yahriacad-live-server
SRV_LOG=/tmp/yahriacad-live.log
PORT=8080

echo "=== 1. Arrêt de l'instance courante ==="
pkill -f 'yahriacad-live-server' 2>/dev/null && { echo "  backend arrêté"; sleep 1; } || echo "  backend déjà arrêté"

echo "=== 2. Compilation (HEAD du dépôt) ==="
cd "$REPO"
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || { echo "COMPILATION KO"; exit 1; }
echo "  binaire prêt : $BIN"

echo "=== 3. Démarrage (depuis ce script — survit à la session bash) ==="
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=yahriacad-live-2026-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/home/z/my-project/yahriacad-data
nohup "$BIN" > "$SRV_LOG" 2>&1 &
SRV_PID=$!
disown "$SRV_PID" 2>/dev/null
echo "$SRV_PID" > /tmp/yahriacad-live.pid
echo "  pid=$SRV_PID (log: $SRV_LOG)"

ready=0
for _ in $(seq 1 40); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:$PORT/healthz)" = "200" ] && { ready=1; break; }
  sleep 0.5
done
[ "$ready" = "1" ] && echo "  backend healthz 200 ✓" || { echo "  ÉCHEC backend — log:"; tail -20 "$SRV_LOG"; exit 1; }

echo "=== 4. Vérification auth + IA ==="
TOKEN=$(curl -s -X POST "http://localhost:$PORT/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
[ -n "$TOKEN" ] && echo "  login admin:admin → JWT ✓" || { echo "  login KO"; tail -20 "$SRV_LOG"; exit 1; }
curl -s "http://localhost:$PORT/healthz" | python3 -c 'import sys,json; d=json.load(sys.stdin); print("  ai_model_loaded:", d.get("ai_model_loaded"))'
echo "BACKEND OPÉRATIONNEL — http://localhost:$PORT"
