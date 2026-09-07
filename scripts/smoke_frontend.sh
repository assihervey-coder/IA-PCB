#!/usr/bin/env bash
# Smoke frontend : build de prod Next.js + page login + page éditeur
set -u
cd /home/z/my-project/yahriacad/frontend
LOG=/tmp/yahriacad-front.log
PORT=3311

npx next start -p $PORT > "$LOG" 2>&1 &
FRONT=$!
trap 'kill $FRONT 2>/dev/null' EXIT

fails=0
check() {
  if [ "$2" = "$3" ]; then echo "  OK   $1 ($3)"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

ready=0
for _ in $(seq 1 40); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/" 2>/dev/null)
  [ "$code" = "200" ] || [ "$code" = "307" ] || [ "$code" = "302" ] && { ready=1; break; }
  sleep 0.5
done
check "frontend démarré (GET /)" 1 "$ready"

for path in "/pages/login" "/pages/pcb-layout" "/pages/project-manager" "/pages/schematic-editor" "/pages/export"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT$path" 2>/dev/null)
  if [ "$code" = "200" ]; then
    check "$path" 200 "$code"
  else
    # Next peut rediriger ; considérer 3xx comme non-échec si page existe dans build
    check "$path (HTTP $code)" 200 "$code"
  fi
done

# titre de la page login
TITRE=$(curl -s "http://localhost:$PORT/pages/login" | grep -oiE '<title>[^<]*' | head -1 | sed 's/<title>//I')
echo "  titre login : $TITRE"

echo ""
if [ "$fails" = "0" ]; then echo "FRONTEND : OK"; else echo "FRONTEND : $fails échec(s)"; tail -10 "$LOG"; fi
