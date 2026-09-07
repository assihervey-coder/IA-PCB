#!/usr/bin/env bash
# ==============================================================
# Seed des DÉMOS en ligne sur le serveur YahriaCad live (port 8080)
# Ré-exécutable : saute les démos déjà présentes (préfixe DEMO).
# Contenu :
#   DEMO1 — Carte cauchemar routée A*   (le produit)
#   DEMO2 — Carte cauchemar routée RL   (la politique BC v6 en ligne)
#   DEMO3 — Vraie carte KiCad complex_hierarchy (52 nets, routée A*)
#   DEMO4 — Vraie carte KiCad pic_programmer    (routée A*)
#   DEMO5 — Vraie carte KiCad video (588 nets ; routage lancé, ~5 min)
# Usage : bash /home/z/my-project/scripts/seed_demos.sh
# ==============================================================
set -u
B=http://localhost:8080

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
[ -n "$TOKEN" ] || { echo "login KO — serveur démarré ?"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"

EXISTING=$(curl -s "$B/api/v1/projects" -H "$AUTH")
have() { echo "$EXISTING" | grep -qF "$1"; }

wait_job() { # $1=pid $2=job $3=max_seconds
  for _ in $(seq 1 "$3"); do
    ST=$(curl -s "$B/api/v1/projects/$1/jobs/$2" -H "$AUTH" \
      | python3 -c 'import sys,json; print(json.load(sys.stdin).get("state",""))' 2>/dev/null)
    [ "$ST" = "done" ] || [ "$ST" = "failed" ] && { echo "$ST"; return; }
    sleep 1
  done
  echo "running"
}

seed_nightmare() { # $1=nom $2=stratégie
  if have "$1"; then echo "  = $1 : déjà présent, sauté"; return; fi
  PID=$(curl -s -X POST "$B/api/v1/demo/nightmare" -H "$AUTH" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("project_id",""))' 2>/dev/null)
  [ -n "$PID" ] || { echo "  ! $1 : demo/nightmare KO"; return; }
  curl -s -X PUT "$B/api/v1/projects/$PID" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$1\",\"description\":\"Démo seedée automatiquement\"}" > /dev/null
  NCOMP=$(curl -s -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
    | python3 -c 'import sys,json; print(len(json.load(sys.stdin).get("components",[])))' 2>/dev/null)
  JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"strategy\":\"$2\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))' 2>/dev/null)
  ST=$(wait_job "$PID" "$JOB" 150)
  echo "  + $1 : projet=$PID, $NCOMP comp., routage $2 → $ST"
}

seed_kicad() { # $1=nom $2=fichier $3=max_wait_route
  if have "$1"; then echo "  = $1 : déjà présent, sauté"; return; fi
  PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$1\",\"description\":\"Vraie carte KiCad importée\",\"layer_count\":2}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  [ -n "$PID" ] || { echo "  ! $1 : création projet KO"; return; }
  IMP=$(curl -s -X POST "$B/api/v1/projects/$PID/import" -H "$AUTH" -F "file=@$2;type=application/octet-stream" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print(str(d.get("nets","?"))+" nets/"+str(d.get("file_version","?")))' 2>/dev/null)
  NCOMP=$(curl -s --max-time 300 -X POST "$B/api/v1/projects/$PID/place" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
    | python3 -c 'import sys,json; print(len(json.load(sys.stdin).get("components",[])))' 2>/dev/null)
  JOB=$(curl -s -X POST "$B/api/v1/projects/$PID/route" -H "$AUTH" -H 'Content-Type: application/json' -d '{"strategy":"astar"}' \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("job_id",""))' 2>/dev/null)
  ST=$(wait_job "$PID" "$JOB" "$3")
  echo "  + $1 : projet=$PID, nets=$IMP, $NCOMP comp., routage A* → $ST"
}

echo "=== Seed des démos sur $B ==="
seed_nightmare "DEMO1-Cauchemar-Astar" astar
seed_nightmare "DEMO2-Cauchemar-RL" rl
seed_kicad "DEMO3-KiCad-complex_hierarchy" /tmp/kicad-complex_hierarchy.kicad_pcb 120
seed_kicad "DEMO4-KiCad-pic_programmer" /tmp/kicad-pic_programmer.kicad_pcb 120
seed_kicad "DEMO5-KiCad-video-588nets" /tmp/kicad-video.kicad_pcb 90

echo ""
echo "=== Projets en ligne ==="
curl -s "$B/api/v1/projects" -H "$AUTH" | python3 -c '
import sys, json
for p in json.load(sys.stdin):
    print("  {}  {}  [{}]".format(p.get("id","?")[:8], p.get("name","?"), p.get("status","")))
'
echo ""
echo "Ouvrir : http://localhost:3000/pages/project-manager"
