#!/usr/bin/env bash
# ==============================================================
# ROUND-TRIP KiCad réel : vraie carte pcbnew 9 → import YahriaCad
# → placement + routage A* → export .kicad_pcb → ré-import →
# comparaison des données. Une seule session bash.
# ==============================================================
set -u
PORT=8094
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-rt.log
AI_LOG=/tmp/yahriacad-ai-rt.log
SRC=/tmp/kicad-complex_hierarchy.kicad_pcb

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-rt

fails=0
check() { if [ "$2" = "$3" ]; then echo "  OK   $1"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi }

cd /home/z/my-project/yahriacad
go build -o "$BIN" ./backend/cmd/yahriacad-server/ || exit 1
cd ai-engine && python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 & AI=$!
cd ..
"$BIN" > "$LOG" 2>&1 & SRV=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
for _ in $(seq 1 40); do python3 -c "import socket; socket.create_connection(('127.0.0.1',50051),1).close()" 2>/dev/null && break; sleep 0.5; done
for _ in $(seq 1 40); do curl -sf -o /dev/null "$B/healthz" && break; sleep 0.5; done
echo "services prêts"

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"

echo ""
echo "=== A. Aller : import de la vraie carte KiCad ==="
PID1=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"RT aller","layer_count":2}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
IMP1=$(curl -s -X POST "$B/api/v1/projects/$PID1/import" -H "$AUTH" -F "file=@$SRC")
echo "  import source : $IMP1"
curl -s -X POST "$B/api/v1/projects/$PID1/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' > /dev/null
JOB=$(curl -s -X POST "$B/api/v1/projects/$PID1/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))')
for _ in $(seq 1 240); do
  ST=$(curl -s "$B/api/v1/projects/$PID1/jobs/$JOB" -H "$AUTH" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
  [ "$ST" = "done" ] || [ "$ST" = "failed" ] && break; sleep 1
done
check "routage de la carte source ($ST)" done "$ST"

echo ""
echo "=== B. Export .kicad_pcb ==="
CODE=$(curl -s -o /tmp/roundtrip-out.kicad_pcb -w '%{http_code}' "$B/api/v1/projects/$PID1/export/kicad" -H "$AUTH")
check "GET /export/kicad (HTTP $CODE, $(wc -c < /tmp/roundtrip-out.kicad_pcb) octets)" 200 "$CODE"
echo "  -- structure (tête du fichier) --"
head -8 /tmp/roundtrip-out.kicad_pcb
echo "  -- empreintes/pads/segments/vias --"
echo "  footprints: $(grep -c '(footprint "' /tmp/roundtrip-out.kicad_pcb) | pads: $(grep -c '(pad "' /tmp/roundtrip-out.kicad_pcb) | segments: $(grep -c '(segment ' /tmp/roundtrip-out.kicad_pcb) | vias: $(grep -c '(via ' /tmp/roundtrip-out.kicad_pcb)"
echo "  -- vrais nets KiCad présents ? --"
for NET in '+12V' '-VAA' '/12Vext' 'GND'; do
  grep -q "\"$NET\"" /tmp/roundtrip-out.kicad_pcb && echo "    net $NET : présent" || echo "    net $NET : ABSENT"
done

echo ""
echo "=== C. Retour : ré-import du fichier exporté ==="
PID2=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"RT retour","layer_count":2}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
IMP2=$(curl -s -X POST "$B/api/v1/projects/$PID2/import" -H "$AUTH" -F "file=@/tmp/roundtrip-out.kicad_pcb")
echo "  import retour : $IMP2"

echo ""
echo "=== D. Comparaison aller/retour ==="
python3 - "$IMP1" "$IMP2" << 'PYEOF'
import sys, json
a, b = json.loads(sys.argv[1]), json.loads(sys.argv[2])
ok = True
for k in ("components", "nets"):
    if a[k] != b[k]:
        print(f"  DIFF {k} : aller={a[k]} retour={b[k]}"); ok = False
    else:
        print(f"  OK   {k} : {a[k]}")
# les segments YahriaCad ressortent en pistes (le fichier contient les
# segments du routage) : tracks_retour >= tracks_aller attendu
print(f"  OK   pistes : aller={a['tracks']} → retour={b['tracks']} (le routage ajoute des segments)" if b['tracks'] >= a['tracks'] else f"  DIFF tracks : {a['tracks']} → {b['tracks']}")
print("COMPARAISON : OK" if ok and b['tracks'] >= a['tracks'] else "COMPARAISON : DIFFÉRENCES")
PYEOF

echo ""
if [ "$fails" = "0" ]; then echo "ROUND-TRIP : OK"; else echo "ROUND-TRIP : $fails échec(s)"; tail -10 "$LOG"; fi
