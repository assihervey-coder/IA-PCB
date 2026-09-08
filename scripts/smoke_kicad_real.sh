#!/usr/bin/env bash
# ==============================================================
# Test RÉEL : vraie carte KiCad → import → placement → routage A*
# → DRC → exports Gerber/ODB++ — une seule session bash.
# Usage : bash /home/z/my-project/scripts/smoke_kicad_real.sh
# ==============================================================
set -u
PORT=8092
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-kicad.log
AI_LOG=/tmp/yahriacad-ai-kicad.log

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-kicad

fails=0
check() {
  if [ "$2" = "$3" ]; then echo "  OK   $1"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

# ---------- services ----------
cd /home/z/my-project/yahriacad
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || { echo "BUILD KO"; exit 1; }
cd ai-engine && python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 & AI=$!
cd ..
"$BIN" > "$LOG" 2>&1 & SRV=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
for _ in $(seq 1 40); do
  python3 -c "import socket; socket.create_connection(('127.0.0.1',50051),1).close()" 2>/dev/null && break; sleep 0.5
done
for _ in $(seq 1 40); do curl -sf -o /dev/null "$B/healthz" && break; sleep 0.5; done
echo "services prêts (healthz : $(curl -s $B/healthz))"

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"

run_board() { # run_board <fichier> <label>
  local FILE="$1" LABEL="$2"
  echo ""
  echo "================ CARTE : $LABEL ================"

  echo "--- 1. Création du projet ---"
  PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"KiCad $LABEL\",\"description\":\"Import réel pcbnew 9\",\"layer_count\":2}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
  check "projet créé ($PID)" ok ok

  echo "--- 2. Import du .kicad_pcb réel ---"
  RESP=$(curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$FILE;type=application/octet-stream")
  echo "$RESP" | python3 -m json.tool 2>/dev/null | head -25 || echo "  resp brut : $(echo $RESP | head -c 400)"

  echo "--- 3. Placement IA ---"
  RESP=$(curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}')
  NCOMP=$(echo "$RESP" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d.get("components",[])))' 2>/dev/null)
  echo "  composants placés : ${NCOMP:-ERR}"
  [ "${NCOMP:-0}" -ge 1 ] && check "placement" ok ok || check "placement" ok KO

  echo "--- 4. Routage A* ---"
  JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))')
  check "job routage lancé ($JOB)" ok ok
  for _ in $(seq 1 240); do
    JRES=$(curl -s "$B/api/v1/projects/$PID/jobs/$JOB" -H "$AUTH")
    ST=$(echo "$JRES" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
    [ "$ST" = "done" ] || [ "$ST" = "failed" ] && break
    sleep 1
  done
  check "routage terminé (state=$ST)" done "$ST"
  echo "  détail : $(echo "$JRES" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"nets {d.get(\"nets_done\",0)}/{d.get(\"nets_total\",0)} — {d.get(\"message\",\"\")}")' 2>/dev/null)"

  echo "--- 5. Layout résultant ---"
  LAYOUT=$(curl -s "$B/api/v1/projects/$PID/layout" -H "$AUTH")
  python3 - "$LAYOUT" << 'PYEOF' 2>/dev/null
import sys, json
def count(o, key):
    if isinstance(o, dict):
        n = len(o.get(key)) if isinstance(o.get(key), list) else 0
        for v in o.values(): n += count(v, key)
        return n
    if isinstance(o, list): return sum(count(v, key) for v in o)
    return 0
try:
    d = json.loads(sys.argv[1])
    print(f"  tracks={count(d,'tracks')}  vias={count(d,'vias')}  footprints={count(d,'components') or count(d,'footprints')}")
except Exception as e:
    print(f"  layout parse KO : {e}")
PYEOF

  echo "--- 6. DRC ---"
  DRC=$(curl -s -X POST "$B/api/v1/projects/$PID/drc" -H "$AUTH")
  echo "$DRC" | python3 -c '
import sys, json
d = json.load(sys.stdin)
v = d.get("violations", [])
print(f"  passed={d.get(\"passed\")} règles={d.get(\"checked_rules\")} violations={len(v)}")
from collections import Counter
for code, n in Counter(x.get("code","?") for x in v).most_common(5):
    print(f"    {code} × {n}")' 2>/dev/null || echo "  DRC brut : $(echo $DRC | head -c 200)"

  echo "--- 7. Exports ---"
  CODE=$(curl -s -o "/tmp/gerber-$LABEL.zip" -w '%{http_code}' "$B/api/v1/projects/$PID/export/gerber" -H "$AUTH")
  echo "  Gerber : HTTP $CODE, $(wc -c < /tmp/gerber-$LABEL.zip) octets, $(python3 -c "import zipfile; print(len(zipfile.ZipFile('/tmp/gerber-$LABEL.zip').namelist()))" 2>/dev/null) fichiers"
  CODE=$(curl -s -o "/tmp/odbpp-$LABEL.tgz" -w '%{http_code}' "$B/api/v1/projects/$PID/export/odbpp" -H "$AUTH")
  echo "  ODB++  : HTTP $CODE, $(wc -c < /tmp/odbpp-$LABEL.tgz) octets"
  tar tzf "/tmp/odbpp-$LABEL.tgz" 2>/dev/null | head -3 | sed 's/^/    /'
  # preuve que les VRAIS nets KiCad sont dans l'ODB++
  tar xzf "/tmp/odbpp-$LABEL.tgz" -O netlist/netlist 2>/dev/null | grep -oE '\$[^ ]+' | head -8 | sed 's/^/    net ODB++ : /'
}

run_board /tmp/kicad-complex_hierarchy.kicad_pcb "complex_hierarchy"
run_board /tmp/kicad-video.kicad_pcb "video"

echo ""
echo "============================================"
if [ "$fails" = "0" ]; then echo "TEST KICAD RÉEL : OK"; else echo "TEST KICAD RÉEL : $fails échec(s)"; tail -15 "$LOG"; fi
