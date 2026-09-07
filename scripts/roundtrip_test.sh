#!/usr/bin/env bash
# ==============================================================
# ROUND-TRIP COMPLET : import → export → ré-import
# Prend le projet source (défaut DEMO3), exporte un .kicad_pcb via
# GET /export/kicad, le ré-importe dans un projet neuf, puis compare
# composants / nets / pistes / vias entre source et re-import.
# Usage : bash /home/z/my-project/scripts/roundtrip_test.sh [prefix_projet]
# ==============================================================
set -u
B=http://localhost:8080
PREFIX="${1:-DEMO3}"

TOKEN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
[ -n "$TOKEN" ] || { echo "login KO"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"

SRC_PID=$(curl -s "$B/api/v1/projects" -H "$AUTH" | python3 -c '
import sys, json
d = json.load(sys.stdin)
ps = d.get("projects", d) if isinstance(d, dict) else d
srcs = [p for p in ps if p.get("name","").startswith("'"$PREFIX"'")]
print(srcs[0]["id"] if srcs else "")
' 2>/dev/null)
[ -n "$SRC_PID" ] || { echo "projet source $PREFIX introuvable"; exit 1; }
echo "projet source : $SRC_PID"

# 1. Export .kicad_pcb
OUT=/tmp/roundtrip_export.kicad_pcb
CODE=$(curl -s -o "$OUT" -w '%{http_code}' "$B/api/v1/projects/$SRC_PID/export/kicad" -H "$AUTH")
[ "$CODE" = "200" ] || { echo "export/kicad KO ($CODE)"; exit 1; }
echo "export ok : $(wc -c < "$OUT") octets"
echo "  version : $(rg -m1 -o 'version [0-9]+' "$OUT" || echo '?')"
echo "  nets déclarés : $(rg -c '^\s*\(net [0-9]+ "' "$OUT" || echo 0)"

# 2. Ré-import dans un projet neuf
NAME="ROUNDTRIP-$PREFIX-$(date +%H%M%S)"
DST_PID=$(curl -s -X POST "$B/api/v1/projects" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"$NAME\",\"description\":\"Round-trip import-export-import\",\"layer_count\":2}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
[ -n "$DST_PID" ] || { echo "création projet round-trip KO"; exit 1; }
IMP=$(curl -s -X POST "$B/api/v1/projects/$DST_PID/import" -H "$AUTH" -F "file=@$OUT;type=application/octet-stream")
echo "ré-import : projet=$DST_PID"
echo "  $(echo "$IMP" | head -c 200)"

# 3. Comparaison source vs ré-import (fidélité géométrique : le writer
# aplatit les pistes multi-points en segments élémentaires, on compare donc
# segments élémentaires, longueur totale, pads, nets, vias)
python3 - "$TOKEN" "$SRC_PID" "$DST_PID" << 'EOF'
import json, sys, urllib.request
token, src, dst = sys.argv[1], sys.argv[2], sys.argv[3]
B = "http://localhost:8080"
def get(path):
    r = urllib.request.Request(B + path, headers={"Authorization": "Bearer " + token})
    return json.load(urllib.request.urlopen(r, timeout=120))
def counts(pid):
    st = get(f"/api/v1/projects/{pid}/stats")
    lay = get(f"/api/v1/projects/{pid}/layout")
    tr = lay.get("tracks", []) or []
    vias = lay.get("vias", []) or []
    segs = sum(max(1, len(t.get("points", [])) - 1) for t in tr)
    length = 0.0
    for t in tr:
        pts = t.get("points", [])
        for i in range(1, len(pts)):
            dx = pts[i]["x"] - pts[i-1]["x"]; dy = pts[i]["y"] - pts[i-1]["y"]
            length += (dx*dx + dy*dy) ** 0.5
    return {
        "components": st.get("components"),
        "nets": st.get("nets"),
        "pads": st.get("pads"),
        "longueur_mm": round(length, 1),
        "vias": len(vias),
        "_pistes": len(tr),
        "_segments": segs,
    }
a, b = counts(src), counts(dst)
print(f"{'métrique':22} {'source':>10} {'ré-import':>10}  match")
ok = True
for k in ("components", "nets", "pads", "longueur_mm", "vias"):
    va, vb = a.get(k), b.get(k)
    m = "OK" if va == vb else "DIFF"
    if va != vb: ok = False
    print(f"{k:22} {str(va):>10} {str(vb):>10}  {m}")
# granularité (informationnel) : la fusion des colinéaires réduit
# volontairement pistes/segments à l'import, la géométrie reste identique
print(f"{'(info) pistes':22} {str(a.get('_pistes')):>10} {str(b.get('_pistes')):>10}  info")
print(f"{'(info) segments':22} {str(a.get('_segments')):>10} {str(b.get('_segments')):>10}  info")
print("\nROUND-TRIP :", "FIDÈLE ✓" if ok else "PERTE DÉTECTÉE ✗")
EOF
echo "projet round-trip : $NAME ($DST_PID)"
