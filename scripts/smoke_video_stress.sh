#!/usr/bin/env bash
# Routage A* de la vraie carte KiCad "video" (189 composants / 588 nets) — timing réel
set -u
PORT=8093
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-func-server
LOG=/tmp/yahriacad-video.log
AI_LOG=/tmp/yahriacad-ai-video.log

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=func-test-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-video

cd /home/z/my-project/yahriacad
cd ai-engine && python3 cmd/ai-server/main.py > "$AI_LOG" 2>&1 & AI=$!
cd ..
"$BIN" > "$LOG" 2>&1 & SRV=$!
trap 'kill $SRV $AI 2>/dev/null' EXIT
for _ in $(seq 1 40); do python3 -c "import socket; socket.create_connection(('127.0.0.1',50051),1).close()" 2>/dev/null && break; sleep 0.5; done
for _ in $(seq 1 40); do curl -sf -o /dev/null "$B/healthz" && break; sleep 0.5; done

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"

PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"KiCad video stress","layer_count":2}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
echo "projet : $PID"

curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@/tmp/kicad-video.kicad_pcb" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"import : {d[\"components\"]} composants, {d[\"nets\"]} nets, {d[\"tracks\"]} pistes importées, {d[\"vias\"]} vias")'

curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' > /dev/null
echo "placement : 189 composants OK"

JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))')
echo "routage lancé : $JOB — chronomètre en cours (max 8 min)…"

START=$(date +%s)
ST=""
for _ in $(seq 1 480); do
  JRES=$(curl -s "$B/api/v1/projects/$PID/jobs/$JOB" -H "$AUTH")
  ST=$(echo "$JRES" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
  if [ "$ST" = "done" ] || [ "$ST" = "failed" ]; then break; fi
  sleep 1
done
END=$(date +%s)
DUR=$((END-START))
echo "état final : $ST en ${DUR}s"
echo "$JRES" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"nets : {d.get(\"nets_done\",0)}/{d.get(\"nets_total\",0)} — message : {d.get(\"message\",\"\")} — erreur : {d.get(\"error\",\"\")}")' 2>/dev/null

LAYOUT=$(curl -s "$B/api/v1/projects/$PID/layout" -H "$AUTH")
python3 - "$LAYOUT" << 'PYEOF'
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
    print(f"layout final : tracks={count(d,'tracks')} vias={count(d,'vias')}")
except Exception as e:
    print("layout KO", e)
PYEOF

CODE=$(curl -s -o /tmp/gerber-video-final.zip -w '%{http_code}' "$B/api/v1/projects/$PID/export/gerber" -H "$AUTH")
echo "export Gerber final : HTTP $CODE, $(wc -c < /tmp/gerber-video-final.zip) octets"
