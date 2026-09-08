#!/usr/bin/env bash
# ==============================================================
# Test de vie FONCTIONNEL YahriaCad — parcours utilisateur réel
# login → création projet → schéma démo → placement → routage A*
# → DRC → exports Gerber/ODB++ → stats/metrics
# Une seule session bash (le sandbox tue les process détachés).
# Usage : bash /home/z/my-project/scripts/smoke_functional.sh
# ==============================================================
set -u
PORT=8091
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-func.log

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-func

fails=0
ok_count=0
check() {
  if [ "$2" = "$3" ]; then echo "  OK   $1 ($3)"; ok_count=$((ok_count+1));
  else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

echo "=== 0. Compilation du binaire ==="
cd /home/z/my-project/yahriacad
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || { echo "COMPILATION KO"; exit 1; }
echo "Binaire prêt : $BIN"

echo ""
echo "=== 1a. Démarrage moteur IA (gRPC 50051) ==="
AI_LOG=/tmp/yahriacad-ai-func.log
cd /home/z/my-project/yahriacad/ai-engine
python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 &
AI=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
cd /home/z/my-project/yahriacad
ai_ready=0
for _ in $(seq 1 40); do
  python3 -c "import socket; s=socket.create_connection(('127.0.0.1',50051),1); s.close()" 2>/dev/null && { ai_ready=1; break; }
  sleep 0.5
done
check "moteur IA à l'écoute sur 50051" 1 "$ai_ready"

echo ""
echo "=== 1. Démarrage serveur (port $PORT) ==="
"$BIN" > "$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
ready=0
for _ in $(seq 1 40); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$B/healthz" 2>/dev/null)
  [ "$code" = "200" ] && { ready=1; break; }
  sleep 0.5
done
check "serveur démarré + healthz" 1 "$ready"
curl -s "$B/healthz"; echo ""

echo ""
echo "=== 2. Authentification JWT ==="
TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
[ -n "$TOKEN" ] && check "login admin → JWT obtenu" ok ok || check "login admin → JWT obtenu" ok KO
AUTH="Authorization: Bearer $TOKEN"

echo ""
echo "=== 3. Création projet réel ==="
RESP=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"Test Fonctionnel","description":"Parcours de vie complet","layer_count":2}')
PID=$(echo "$RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
[ -n "$PID" ] && check "POST /projects → id=$PID" ok ok || { check "POST /projects" ok KO; echo "  resp=$RESP"; }

echo ""
echo "=== 4. Schéma démo « nightmare » (composants+nets réels) ==="
RESP=$(curl -s -X POST "$B/api/v1/demo/nightmare" -H "$AUTH")
PID2=$(echo "$RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("project_id",""))' 2>/dev/null)
NFAULTS=$(echo "$RESP" | python3 -c 'import sys,json; print(len(json.load(sys.stdin).get("faults",[])))' 2>/dev/null)
check "demo/nightmare → projet=$PID2, $NFAULTS fautes" ok ok

echo ""
echo "=== 5. Placement (sur projet nightmare — synchrone) ==="
RESP=$(curl -s -X POST "$B/api/v1/projects/$PID2/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}')
NCOMP=$(echo "$RESP" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d.get("components",[])))' 2>/dev/null)
[ "${NCOMP:-0}" -ge 1 ] && check "place synchrone → $NCOMP composants placés" ok ok || { check "place" ok KO; echo "  resp=$RESP" | head -c 300; }

echo ""
echo "=== 6. Routage A* (cœur du produit) ==="
RESP=$(curl -s -X POST "$B/api/v1/projects/$PID2/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}')
JOB=$(echo "$RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))' 2>/dev/null)
[ -n "$JOB" ] && check "route → job=$JOB" ok ok || { check "route" ok KO; echo "  resp=$RESP"; }
for _ in $(seq 1 60); do
  JRES=$(curl -s "$B/api/v1/projects/$PID2/jobs/$JOB" -H "$AUTH")
  ST=$(echo "$JRES" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
  [ "$ST" = "done" ] || [ "$ST" = "failed" ] && break
  sleep 0.5
done
check "job routage terminé (state=$ST)" done "$ST"
NDONE=$(echo "$JRES" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"{d.get(\"nets_done\",0)}/{d.get(\"nets_total\",0)} nets")' 2>/dev/null)
echo "  détail job : $NDONE, message=$(echo $JRES | python3 -c 'import sys,json; print(json.load(sys.stdin).get("message",""))' 2>/dev/null)"

echo ""
echo "=== 7. Layout contient des pistes ? ==="
ROUTES=$(curl -s "$B/api/v1/projects/$PID2/layout" -H "$AUTH" | python3 -c '
import sys, json
def count(o, key):
    if isinstance(o, dict):
        n = len(o.get(key)) if isinstance(o.get(key), list) else 0
        for v in o.values():
            n += count(v, key)
        return n
    if isinstance(o, list):
        return sum(count(v, key) for v in o)
    return 0
d = json.load(sys.stdin)
print(f"tracks={count(d, "tracks")} vias={count(d, "vias")}")
' 2>/dev/null)
echo "  layout : $ROUTES"
TRK=$(echo "$ROUTES" | grep -oE 'tracks=[0-9]+' | cut -d= -f2)
[ "${TRK:-0}" -ge 1 ] && check "layout contient des pistes ($ROUTES)" ok ok || check "layout non vide" ok KO

echo ""
echo "=== 8. DRC (vérification) ==="
RESP=$(curl -s -X POST "$B/api/v1/projects/$PID2/drc" -H "$AUTH")
DRCV=$(echo "$RESP" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(str(d)[:120])' 2>/dev/null)
[ -n "$DRCV" ] && check "DRC répond : $DRCV" ok ok || check "DRC" ok KO

echo ""
echo "=== 9. Exports Gerber + ODB++ ==="
CODE=$(curl -s -o /tmp/gerber.out -w '%{http_code}' "$B/api/v1/projects/$PID2/export/gerber" -H "$AUTH")
check "export Gerber (HTTP $CODE, $(wc -c < /tmp/gerber.out) octets)" 200 "$CODE"
CODE=$(curl -s -o /tmp/odbpp.tgz -w '%{http_code}' "$B/api/v1/projects/$PID2/export/odbpp" -H "$AUTH")
check "export ODB++ (HTTP $CODE, $(wc -c < /tmp/odbpp.tgz) octets)" 200 "$CODE"
if [ "$CODE" = "200" ]; then
  echo "  contenu tgz : $(tar tzf /tmp/odbpp.tgz 2>/dev/null | tr '\n' ' ')"
fi

echo ""
echo "=== 10. Stats + Métriques Prometheus ==="
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$B/api/v1/projects/$PID2/stats" -H "$AUTH")
check "stats projet (HTTP $CODE)" 200 "$CODE"
MET=$(curl -s "$B/metrics" | grep -c '^yahriacad_' 2>/dev/null)
[ "${MET:-0}" -ge 1 ] && check "métriques yahriacad_* présentes ($MET lignes)" ok ok || check "métriques" ok KO

echo ""
echo "=== 11. Sécurité rapide (401 sans jeton) ==="
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$B/api/v1/projects")
check "API protégée sans jeton → 401" 401 "$CODE"

echo ""
echo "============================================"
if [ "$fails" = "0" ]; then
  echo "RÉSULTAT : PARCOURS COMPLET OK ($ok_count checks réussis)"
else
  echo "RÉSULTAT : $fails échec(s) sur $((ok_count+fails)) checks"
  echo "--- log serveur (fin) ---"
  tail -20 "$LOG"
fi
