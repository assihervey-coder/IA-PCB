#!/usr/bin/env bash
# ==============================================================
# Imitation learning — STAGE 2 : éval hors-ligne sur la vraie
# carte → reload à chaud → arène A* vs RL(BC) → routage RL réel.
# Usage : bash scripts/smoke_imitation_eval.sh
# ==============================================================
set -u
PORT=8096
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-imit-server
LOG=/tmp/yahriacad-imit2.log
AI_LOG=/tmp/yahriacad-imit2-ai.log
WORK=/tmp/yahriacad-imitation
BOARD=/tmp/kicad-complex_hierarchy.kicad_pcb
AIROOT=/home/z/my-project/yahriacad/ai-engine
MODEL=training/router/model_bc_real2.pt
SKIP_OFFLINE=${SKIP_OFFLINE:-0}

export PATH=/home/z/toolchain/go/bin:$PATH
export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=imit-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_APP_ENV=development
export YAHRIACAD_HTTP_PORT=$PORT
export YAHRIACAD_DATA_DIR=/tmp/yahriacad-data-imit2

fails=0
check() {
  if [ "$2" = "$3" ]; then echo "  OK   $1"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

# Purge des processus orphelins (SO_REUSEPORT sur 50051)
pkill -f "cmd/ai-server/main.py" 2>/dev/null; pkill -f yahriacad 2>/dev/null; sleep 1

if [ "$SKIP_OFFLINE" != "1" ]; then
echo "--- 1. Éval hors-ligne : A* vs BC sur la vraie carte (52 nets, cap 400 pas) ---"
cd "$AIROOT"
python3 training/router/imitation.py eval \
  --model "$MODEL" --input "$WORK/route_input.json" --rollout-cap 400 \
  > "$WORK/eval_real.json" 2>"$WORK/eval_real.err"
if python3 -c "import json; json.load(open('$WORK/eval_real.json'))" 2>/dev/null; then
  python3 - "$WORK/eval_real.json" << 'PYEOF'
import json, sys
r = json.load(open(sys.argv[1]))
a, bc, fb = r["astar"], r["bc"], r["bc_with_fallback"]
print(f"  A*            : {a['completed']}/{r['nets']} nets, {a['length_mm']} mm, {a['vias']} vias, {a['time_s']} s")
print(f"  BC seul       : {bc['completed']}/{r['nets']} nets, {bc['length_mm']} mm, {bc['vias']} vias, {bc['time_s']} s")
print(f"  BC + repli A* : {fb['completed']}/{r['nets']} nets, {fb['length_mm']} mm, {fb['vias']} vias")
ok_bc = [row for row in r["rows"] if row["bc_ok"]]
if ok_bc:
    ratios = [row["bc_mm"] / row["astar_mm"] for row in ok_bc if row["astar_mm"] > 0]
    print(f"  longueur BC/A* sur nets BC-complets : moy {sum(ratios)/len(ratios):.3f}")
PYEOF
  check "éval hors-ligne" ok ok
else
  echo "  ERREUR éval : $(tail -3 "$WORK/eval_real.err")"
  check "éval hors-ligne" ok KO
fi
fi

# ---------- services ----------
cd /home/z/my-project/yahriacad
# Moteur d'abord, attente stricte du bind, PUIS backend (backoff gRPC).
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

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
AUTH="Authorization: Bearer $TOKEN"

echo "--- 2. Reload à chaud du checkpoint BC ---"
RELOAD=$(curl -s -X POST "$B/api/v1/ai/model/reload" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"checkpoint_path\":\"$MODEL\"}")
echo "$RELOAD" | python3 -c 'import sys,json; d=json.load(sys.stdin); print("  loaded:", d.get("loaded"), "-", d.get("message",""))'
LOADED=$(echo "$RELOAD" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("loaded", False))')
check "reload BC" True "$LOADED"

echo "--- 3. Arène A* vs RL(BC) sur la carte démo ---"
PID2=$(curl -s -X POST "$B/api/v1/demo/nightmare" -H "$AUTH" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("project_id",""))')
ARENA=$(curl -s -X POST "$B/api/v1/projects/$PID2/arena/benchmark" -H "$AUTH")
echo "$ARENA" > "$WORK/arena.json"
python3 - "$WORK/arena.json" << 'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
a, r = d.get("astar", {}), d.get("rl", {})
print(f"  A*     : score {a.get('score',0):.2f} — {a.get('total_length_mm',0):.1f} mm, {a.get('via_count',0)} vias, {a.get('completed',0)}/{a.get('completed',0)+a.get('failed',0)} nets, {a.get('duration_ms',0)} ms")
print(f"  RL(BC) : score {r.get('score',0):.2f} — {r.get('total_length_mm',0):.1f} mm, {r.get('via_count',0)} vias, {r.get('completed',0)}/{r.get('completed',0)+r.get('failed',0)} nets, {r.get('duration_ms',0)} ms")
print(f"  verdict: {d.get('winner')} (marge {d.get('margin',0):.2f}) — ELO {[s.get('rating') for s in d.get('elo', [])]}")
PYEOF
W=$(echo "$ARENA" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("winner",""))' 2>/dev/null)
[ -n "$W" ] && check "arène exécutée (verdict=$W)" ok ok || check "arène" ok KO

echo "--- 4. Routage RL(BC) en ligne sur la vraie carte (6 nets, filtre) ---"
PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"RL BC complex_hierarchy","description":"test echelle","layer_count":2}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$BOARD;type=application/octet-stream" > /dev/null
curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' > /dev/null
FILTRES=$(python3 -c "import json; d=json.load(open('$WORK/route_input.json')); ns=[n['name'] for n in d['nets']]; print(json.dumps(ns[:6]))")
T0=$(date +%s)
JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"strategy\":\"rl\",\"net_filter\":$FILTRES}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))')
for _ in $(seq 1 420); do
  JRES=$(curl -s "$B/api/v1/projects/$PID/jobs/$JOB" -H "$AUTH")
  ST=$(echo "$JRES" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
  [ "$ST" = "done" ] || [ "$ST" = "failed" ] && break
  sleep 1
done
T1=$(date +%s)
echo "  job state=$ST en $((T1-T0)) s — $(echo "$JRES" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"nets {d.get(\"nets_done\",0)}/{d.get(\"nets_total\",0)} — {d.get(\"message\",\"\")}")' 2>/dev/null)"
[ "$ST" = "done" ] && check "routage RL 6 nets vraie carte" ok ok || check "routage RL 6 nets" ok KO

echo ""
[ "$fails" = "0" ] && echo "SMOKE EVAL OK" || echo "SMOKE EVAL : $fails echec(s)"
exit $fails
