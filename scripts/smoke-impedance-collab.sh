#!/usr/bin/env bash
# Smoke test HTTP réel : impédance différentielle + collaboration CRDT.
# Démarre le backend en mémoire, exécute le scénario, arrête le serveur.
set -euo pipefail

cd /home/z/my-project/yahriacad
export PATH=/home/z/toolchain/go/bin:$PATH
PORT=8091
BASE="http://127.0.0.1:${PORT}"

echo "== démarrage du backend (repo mémoire) =="
/home/z/my-project/scripts/.bin/yahriacad-server &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null || true' EXIT

for i in $(seq 1 30); do
  if curl -sf "${BASE}/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.5
done
echo "-- healthz :"
curl -s "${BASE}/healthz"
echo

echo "== création du projet =="
PID=$(curl -s -X POST "${BASE}/api/v1/projects" -H 'Content-Type: application/json' \
  -d '{"name":"SmokeImpedance","description":"smoke CRDT+impedance","layer_count":2}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
echo "project_id=${PID}"

echo "== PUT layout (paire USB à 0.2 mm d'écart + paire ETH déséquilibrée + R1) =="
curl -s -X PUT "${BASE}/api/v1/projects/${PID}/layout" -H 'Content-Type: application/json' -d '{
  "board": {"width_mm": 60, "height_mm": 40, "layer_count": 2, "layer_names": ["F.Cu","B.Cu"]},
  "components": [{"ref":"R1","footprint":"R0603","x":10,"y":30,"rotation":0,"fixed":false}],
  "nets": [],
  "tracks": [
    {"net":"USB_DP","layer":0,"width":0.25,"points":[{"x":5,"y":10},{"x":35,"y":10}]},
    {"net":"USB_DN","layer":0,"width":0.25,"points":[{"x":5,"y":10.2},{"x":35,"y":10.2}]},
    {"net":"ETH_TXP","layer":0,"width":0.2,"points":[{"x":5,"y":20},{"x":50,"y":20}]},
    {"net":"ETH_TXN","layer":0,"width":0.2,"points":[{"x":5,"y":20.25},{"x":42,"y":20.25}]}
  ],
  "vias": []
}' > /dev/null
echo "layout posé."

echo "== POST /impedance =="
curl -s -X POST "${BASE}/api/v1/projects/${PID}/impedance" -H 'Content-Type: application/json' -d '{}' | python3 -c '
import sys, json
d = json.load(sys.stdin)
print("summary:", d["summary"])
print("stackup:", d["stackup"])
print("paires analysees:", d["analyzed_pairs"], "dans bande:", d["in_tolerance_count"])
for c in d["classes"]:
    for p in c["pairs"]:
        print("  %s/%s: Z0=%s Zdiff=%s cible=%s ecart=%s mm skew=%s ps rec_w=%s mm conforme=%s" % (
            p["net_plus"], p["net_minus"], p["z0_ohms"], p["zdiff_ohms"], p["target_ohms"],
            p["gap_mm"], p["skew_ps"], p["recommended_width_mm"], p["in_tolerance"]))'

echo "== cible surchargée : default → 90 Ω =="
curl -s -X POST "${BASE}/api/v1/projects/${PID}/impedance" -H 'Content-Type: application/json' \
  -d '{"targets":{"default":90}}' | python3 -c '
import sys, json
d = json.load(sys.stdin)
print("summary:", d["summary"])
for c in d["classes"]:
    for p in c["pairs"]:
        print("  %s/%s: Zdiff=%s ohm cible=%s ohm ecart=%s mm rec_w=%s mm skew=%s ps" % (
            p["net_plus"], p["net_minus"], p["zdiff_ohms"], p["target_ohms"],
            p["gap_mm"], p["recommended_width_mm"], p["skew_ps"]))'

echo "== collab : track.add =="
curl -s -X POST "${BASE}/api/v1/projects/${PID}/collab/ops" -H 'Content-Type: application/json' -d '{
  "actor":"smoke",
  "ops":[{"client_id":"op1","kind":"track.add","payload":{"net":"TEST","layer":0,"width":0.3,"points":[{"x":1,"y":1},{"x":5,"y":1}]}}]
}' | python3 -c 'import sys,json;d=json.load(sys.stdin);print("applied:",d["applied"],"seq:",d["seq"],"rejected:",d["rejected"])'

echo "-- déplacement R1 via CRDT :"
curl -s -X POST "${BASE}/api/v1/projects/${PID}/collab/ops" -H 'Content-Type: application/json' -d '{
  "actor":"smoke",
  "ops":[{"client_id":"op2","kind":"component.move","target":"R1","payload":{"x":22.5,"y":31.25}}]
}' | python3 -c 'import sys,json;d=json.load(sys.stdin);print("applied:",d["applied"],"seq:",d["seq"],"rejected:",d["rejected"])'

echo "-- state (seq + clock) :"
curl -s "${BASE}/api/v1/projects/${PID}/collab/state" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("seq:",d["seq"],"clock:",d["clock"])'

echo "-- undo (doit retirer la piste TEST ou replacer R1, la dernière op de smoke) :"
curl -s -X POST "${BASE}/api/v1/projects/${PID}/collab/undo" -H 'Content-Type: application/json' -d '{"actor":"smoke"}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print("undone:",d["undone"],"kind:",(d.get("op") or {}).get("kind"))'

echo "-- redo :"
curl -s -X POST "${BASE}/api/v1/projects/${PID}/collab/redo" -H 'Content-Type: application/json' -d '{"actor":"smoke"}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print("undone(redo):",d["undone"],"kind:",(d.get("op") or {}).get("kind"))'

echo "-- layout final : position de R1 et présence de TEST :"
curl -s "${BASE}/api/v1/projects/${PID}/layout" | python3 -c '
import sys, json
d = json.load(sys.stdin)
r1 = next(c for c in d["components"] if c["ref"] == "R1")
print("R1 à", r1["x"], r1["y"])
print("piste TEST présente:", any(t["net"] == "TEST" for t in d["tracks"]))'

echo "== SMONE OK =="
