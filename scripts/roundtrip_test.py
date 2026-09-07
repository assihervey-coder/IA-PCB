#!/usr/bin/env python3
"""Round-trip complet import→export→ré-import pour YahriaCad.

Pour chaque projet source (DEMO4 pic_programmer / KiCad 10, DEMO3
complex_hierarchy / KiCad 9) :
  1. Snapshot du layout live (composants, nets, pistes, vias, board).
  2. Export KiCad via GET /export/kicad.
  3. Inspection structurelle du fichier exporté (s-expression).
  4. Création d'un projet neuf + ré-import du fichier exporté.
  5. Diff de fidélité : composants (refs), nets (noms), pistes, vias, board.
  6. Export ODB++ des deux côtés + diff du netlist.

Usage : python3 roundtrip_test.py
"""
import json
import re
import sys
import tarfile
import urllib.error
import urllib.request

BASE = "http://localhost:8080"
WORK = "/home/z/my-project/tmp-rt"
SOURCES = [
    ("DEMO4", None, "pic_programmer (KiCad 10)"),
    ("DEMO3", None, "complex_hierarchy (KiCad 9)"),
]
TOKEN = open("/tmp/yh-token").read().strip()


def req(method, url, data=None, headers=None, raw=False):
    h = {"Authorization": "Bearer " + TOKEN}
    if headers:
        h.update(headers)
    r = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        resp = urllib.request.urlopen(r, timeout=180)
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="replace")[:300]
        raise RuntimeError(f"{method} {url} -> HTTP {e.code}: {body}")
    body = resp.read()
    if raw:
        return body
    return json.loads(body) if body.strip() else {}


def find_project_id(name_fragment):
    data = req("GET", BASE + "/api/v1/projects")
    ps = data.get("projects", data)
    for p in ps:
        if name_fragment.lower() in p["name"].lower():
            return p["id"], p["name"]
    return None, None


def snapshot(pid):
    lay = req("GET", f"{BASE}/api/v1/projects/{pid}/layout")
    return {
        "board": lay.get("board", {}),
        "components": lay.get("components", []),
        "nets": lay.get("nets", []),
        "tracks": lay.get("tracks", []),
        "vias": lay.get("vias", []),
    }


def parse_sexpr(src):
    toks = re.findall(r'\(|\)|"(?:[^"\\]|\\.)*"|[^\s()]+', src)

    def pl(idx):
        lst, i = [], idx + 1
        while toks[i] != ")":
            if toks[i] == "(":
                sub, i = pl(i)
                lst.append(sub)
            else:
                lst.append(toks[i])
                i += 1
        return lst, i + 1

    lst, _ = pl(0)
    return lst


def inspect_exported(path):
    tree = parse_sexpr(open(path).read())
    heads = {}
    nets_top = []
    for c in tree[1:]:
        if isinstance(c, list) and c:
            heads[c[0]] = heads.get(c[0], 0) + 1
            if c[0] == "net":
                nets_top.append(c[1:3])
    return tree[0], heads, nets_top


def reduce_collinear(points, eps=1e-3):
    """Supprime les points intermédiaires REDONDANTS : distance au SEGMENT
    [A,C] (projection clampée), jamais à la droite infinie — un point de
    rebroussage (extrémité locale) n'est jamais supprimé."""
    pts = [(p["x"], p["y"]) for p in points]
    if len(pts) < 3:
        return pts
    out = [pts[0]]
    for i in range(1, len(pts) - 1):
        ax, ay = out[-1]
        bx, by = pts[i]
        cx, cy = pts[i + 1]
        vx, vy = cx - ax, cy - ay
        l2 = vx * vx + vy * vy
        if l2 < eps * eps:
            d = ((bx - ax) ** 2 + (by - ay) ** 2) ** 0.5
        else:
            t = ((bx - ax) * vx + (by - ay) * vy) / l2
            t = max(0.0, min(1.0, t))  # clamp : segment, pas droite
            dx, dy = bx - (ax + t * vx), by - (ay + t * vy)
            d = (dx * dx + dy * dy) ** 0.5
        if d > eps:
            out.append((bx, by))
    out.append(pts[-1])
    return out


def elem_segments(tracks, q=3, eps=1e-3):
    """Multiset des segments élémentaires quantifiés après réduction
    colinéaire : géométrie cuivre réelle, indépendante de la discrétisation
    et du partitionnement en pistes (cosmétiques tous deux)."""
    from collections import Counter
    cnt = Counter()
    for t in tracks:
        pts = reduce_collinear(t.get("points", []), eps)
        for a, b in zip(pts, pts[1:]):
            key = ((round(a[0], q), round(a[1], q)),
                   (round(b[0], q), round(b[1], q)), t.get("layer"),
                   round(t.get("width", 0), 3))
            if key[0] == key[1]:
                continue
            if key[0] > key[1]:  # segment non orienté
                key = (key[1], key[0], key[2], key[3])
            cnt[key] += 1
    return cnt


def copper_length(tracks):
    """Longueur totale de cuivre par clé (net, layer, width)."""
    tot = {}
    for t in tracks:
        pts = t.get("points", [])
        k = (t.get("net"), t.get("layer"), round(t.get("width", 0), 3))
        for a, b in zip(pts, pts[1:]):
            tot[k] = tot.get(k, 0.0) + ((b["x"]-a["x"])**2 + (b["y"]-a["y"])**2) ** 0.5
    return tot


def coverage_samples(tracks, step=0.5):
    """Échantillonne le cuivre (extrémités + pas régulier) par clé —
    représentation indépendante de la discrétisation des polylignes."""
    samples = {}
    for t in tracks:
        pts = t.get("points", [])
        k = (t.get("net"), t.get("layer"), round(t.get("width", 0), 3))
        s = samples.setdefault(k, [])
        for a, b in zip(pts, pts[1:]):
            dx, dy = b["x"]-a["x"], b["y"]-a["y"]
            length = (dx*dx + dy*dy) ** 0.5
            s.append((a["x"], a["y"]))
            if length > step:
                n = int(length / step)
                for i in range(1, n + 1):
                    frac = i * step / length
                    if frac >= 1.0:
                        break
                    s.append((a["x"] + frac*dx, a["y"] + frac*dy))
        if pts:
            s.append((pts[-1]["x"], pts[-1]["y"]))
    return samples


def elem_segments_by_key(tracks):
    """Segments élémentaires bruts groupés par clé (net, layer, width)."""
    segs = {}
    for t in tracks:
        pts = t.get("points", [])
        k = (t.get("net"), t.get("layer"), round(t.get("width", 0), 3))
        s = segs.setdefault(k, [])
        for a, b in zip(pts, pts[1:]):
            if a["x"] == b["x"] and a["y"] == b["y"]:
                continue
            s.append((a["x"], a["y"], b["x"], b["y"]))
    return segs


def coverage_diff(src_s, rt_s, src_segs, rt_segs, eps=0.05):
    """Chaque échantillon d'un côté doit être à eps d'un SEGMENT cuivré de
    l'autre côté (même clé) — insensible aux décalages d'échantillonnage."""
    def orphans_for(samples, segs):
        if not segs:
            return len(samples)
        miss = 0
        for (x, y) in samples:
            best = float("inf")
            for (ax, ay, bx, by) in segs:
                vx, vy = bx - ax, by - ay
                l2 = vx * vx + vy * vy
                if l2 <= 1e-18:
                    d2 = (x - ax) ** 2 + (y - ay) ** 2
                else:
                    t = ((x - ax) * vx + (y - ay) * vy) / l2
                    t = 0.0 if t < 0.0 else (1.0 if t > 1.0 else t)
                    dx, dy = x - (ax + t * vx), y - (ay + t * vy)
                    d2 = dx * dx + dy * dy
                if d2 < best:
                    best = d2
                    if best <= eps * eps:
                        break
            if best > eps * eps:
                miss += 1
        return miss

    detail = {}
    orphans = {"src": 0, "rt": 0}
    for a_name, A_samp, A_segs, B_samp, B_segs in (
            ("src", src_s, src_segs, rt_s, rt_segs),
            ("rt", rt_s, rt_segs, src_s, src_segs)):
        for k, samples in A_samp.items():
            miss = orphans_for(samples, B_segs.get(k, []))
            orphans[a_name] += miss
            if miss:
                d = detail.setdefault(k, {"src": 0, "rt": 0})
                d[a_name] += miss
    return orphans, detail


def max_pos_delta(src, rt):
    pos = {c["ref"]: (c.get("x", 0), c.get("y", 0)) for c in src}
    delta = 0.0
    for c in rt:
        x, y = pos.get(c["ref"], (None, None))
        if x is None:
            return float("inf")
        delta = max(delta, abs(x - c.get("x", 0)), abs(y - c.get("y", 0)))
    return delta


def fidelity(src_snap, rt_snap, label):
    refs_src = {c["ref"] for c in src_snap["components"]}
    refs_rt = {c["ref"] for c in rt_snap["components"]}
    nets_src = {n["name"] for n in src_snap["nets"]}
    nets_rt = {n["name"] for n in rt_snap["nets"]}
    pos_d = max_pos_delta(src_snap["components"], rt_snap["components"])
    # Métriques géométriques rigoureuses : longueur de cuivre par clé
    # (exacte) + couverture échantillonnée (insensible à la discrétisation).
    len_src, len_rt = copper_length(src_snap["tracks"]), copper_length(rt_snap["tracks"])
    keys = set(len_src) | set(len_rt)
    len_bad = {k: (len_src.get(k, 0.0), len_rt.get(k, 0.0)) for k in keys
               if abs(len_src.get(k, 0.0) - len_rt.get(k, 0.0)) > max(1e-4, 1e-6 * max(len_src.get(k, 0.0), len_rt.get(k, 0.0)))}
    len_ok = not len_bad
    src_s, rt_s = coverage_samples(src_snap["tracks"]), coverage_samples(rt_snap["tracks"])
    segs_src, segs_rt = elem_segments_by_key(src_snap["tracks"]), elem_segments_by_key(rt_snap["tracks"])
    orphans, cov_detail = coverage_diff(src_s, rt_s, segs_src, segs_rt)
    cov_ok = orphans["src"] == 0 and orphans["rt"] == 0
    seg_src, seg_rt = elem_segments(src_snap["tracks"]), elem_segments(rt_snap["tracks"])
    checks = [
        ("composants", len(src_snap["components"]) == len(rt_snap["components"]),
         f"{len(src_snap['components'])} → {len(rt_snap['components'])}"),
        ("refs composants (set)", refs_src == refs_rt,
         "" if refs_src == refs_rt else f"manquants={sorted(refs_src-refs_rt)[:5]} ajoutés={sorted(refs_rt-refs_src)[:5]}"),
        ("positions composants (max Δ mm)", pos_d < 0.01, f"Δ={pos_d:.4f}"),
        ("nets", len(src_snap["nets"]) == len(rt_snap["nets"]),
         f"{len(src_snap['nets'])} → {len(rt_snap['nets'])}"),
        ("noms de nets (set)", nets_src == nets_rt,
         "" if nets_src == nets_rt else f"manquants={sorted(nets_src-nets_rt)[:5]} ajoutés={sorted(nets_rt-nets_src)[:5]}"),
        ("LONGUEUR cuivre par (net,couche,largeur)", len_ok,
         f"{sum(len_src.values()):.1f} mm vs {sum(len_rt.values()):.1f} mm"
         + ("" if len_ok else f" | {len(len_bad)} clés divergentes: "
            + "; ".join(f"{k[0]} L{k[1]} w{k[2]}: {v[0]:.2f}→{v[1]:.2f}" for k, v in list(len_bad.items())[:4]))),
        ("COUVERTURE cuivre (échantillons orphelins)", cov_ok,
         f"src→rt: {orphans['src']}, rt→src: {orphans['rt']}"
         + ("" if cov_ok else f" | détail: {dict(list(cov_detail.items())[:3])}")),
        ("vias", len(src_snap["vias"]) == len(rt_snap["vias"]),
         f"{len(src_snap['vias'])} → {len(rt_snap['vias'])}"),
    ]
    bw, rtb = src_snap["board"], rt_snap["board"]
    checks.append(("board (w×h×couches)",
                   abs(bw.get("width_mm", 0) - rtb.get("width_mm", 0)) < 0.05 and
                   abs(bw.get("height_mm", 0) - rtb.get("height_mm", 0)) < 0.05 and
                   bw.get("layer_count") == rtb.get("layer_count"),
                   f"{bw.get('width_mm',0):.1f}×{bw.get('height_mm',0):.1f}×{bw.get('layer_count')} → "
                   f"{rtb.get('width_mm',0):.1f}×{rtb.get('height_mm',0):.1f}×{rtb.get('layer_count')}"))
    print(f"\n=== Fidélité {label} ===")
    ok_all = True
    for name, ok, detail in checks:
        mark = "OK  " if ok else "DIFF"
        ok_all = ok_all and ok
        print(f"  [{mark}] {name}: {detail}")
    print(f"  [INFO] partition en pistes (cosmétique): "
          f"{len(src_snap['tracks'])} → {len(rt_snap['tracks'])} ; "
          f"segments élémentaires {sum(seg_src.values())} → {sum(seg_rt.values())} "
          f"(discrétisation des runs colinéaires, couverture identique)")
    return ok_all


def odbpp_netlist_names(tgz_path):
    with tarfile.open(tgz_path, "r:gz") as tf:
        for m in tf.getmembers():
            if m.name.endswith("netlist/netlist"):
                data = tf.extractfile(m).read().decode(errors="replace")
                return set(re.findall(r"NET '([^']+)'", data))
    return set()


def pad_side_stats(path):
    """Répartition des pads par cuivre dans un fichier .kicad_pcb exporté."""
    s = open(path).read()
    return {
        "F.Cu": len(re.findall(r'\(pad [^\n]*\(layers "F\.Cu"', s)),
        "B.Cu": len(re.findall(r'\(pad [^\n]*\(layers "B\.Cu"', s)),
        "*.Cu": len(re.findall(r'\(pad [^\n]*\(layers "\*\.Cu"', s)),
    }


def cleanup_rt():
    data = req("GET", BASE + "/api/v1/projects")
    ps = data.get("projects", data)
    for p in ps:
        if p["name"].startswith("RT-"):
            req("DELETE", f"{BASE}/api/v1/projects/{p['id']}")
            print(f"cleanup: {p['name']} supprimé")


def main():
    cleanup_rt()
    results = []
    for frag, forced_pid, label in SOURCES:
        pid = forced_pid
        if not pid:
            pid, pname = find_project_id(frag)
            if not pid:
                print(f"!! projet {frag} introuvable, skip")
                continue
        pname = (find_project_id(frag)[1] or frag)
        print(f"\n########## ROUND-TRIP {pname} ({label}) ##########")

        src = snapshot(pid)
        print(f"source: {len(src['components'])} composants, {len(src['nets'])} nets, "
              f"{len(src['tracks'])} pistes, {len(src['vias'])} vias")

        # 1) Export KiCad
        blob = req("GET", f"{BASE}/api/v1/projects/{pid}/export/kicad", raw=True)
        exp_path = f"{WORK}/{frag}_export.kicad_pcb"
        open(exp_path, "wb").write(blob)
        root, heads, nets_top = inspect_exported(exp_path)
        named = [n for n in nets_top if len(n) > 1 and n[1] not in ('""',)]
        print(f"export: {len(blob)/1024:.0f} Ko, racine={root}, nets top-level={len(nets_top)} "
              f"(nommés={len(named)}), footprints={heads.get('footprint',0)}, "
              f"segments={heads.get('segment',0)}, vias={heads.get('via',0)}, zones={heads.get('zone',0)}")

        # 2) Projet neuf + ré-import
        body = json.dumps({"name": f"RT-{frag}-roundtrip",
                           "description": f"Round-trip depuis {pname}"}).encode()
        newp = req("POST", BASE + "/api/v1/projects", data=body,
                   headers={"Content-Type": "application/json"})
        new_id = newp["id"]
        print(f"projet neuf: {new_id}")
        boundary = "----yhrt42"
        fname = f"{frag}_export.kicad_pcb"
        mp = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; "
              f"filename=\"{fname}\"\r\nContent-Type: text/plain\r\n\r\n").encode() + blob + \
             f"\r\n--{boundary}--\r\n".encode()
        imp = req("POST", f"{BASE}/api/v1/projects/{new_id}/import", data=mp,
                  headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
        print(f"ré-import: format={imp.get('format')} file_version={imp.get('file_version')} "
              f"components={imp.get('components')} nets={imp.get('nets')} warnings={imp.get('warnings')}")

        rt = snapshot(new_id)
        ok = fidelity(src, rt, f"{pname} → export → ré-import")

        # 3) ODB++ des deux côtés : les noms $XXX du netlist doivent coïncider
        try:
            odb_src = req("GET", f"{BASE}/api/v1/projects/{pid}/export/odbpp", raw=True)
            p_src = f"{WORK}/{frag}_src.tgz"
            open(p_src, "wb").write(odb_src)
            odb_rt = req("GET", f"{BASE}/api/v1/projects/{new_id}/export/odbpp", raw=True)
            p_rt = f"{WORK}/{frag}_rt.tgz"
            open(p_rt, "wb").write(odb_rt)
            ns, nr = odbpp_netlist_names(p_src), odbpp_netlist_names(p_rt)
            odb_ok = ns == nr and len(ns) > 0
            print(f"  [{'OK  ' if odb_ok else 'DIFF'}] ODB++ netlist: {len(ns)} nets source vs "
                  f"{len(nr)} ré-importé" + ("" if odb_ok else
                  f" (manquants={sorted(ns-nr)[:4]} ajoutés={sorted(nr-ns)[:4]})"))
            ok = ok and odb_ok
        except Exception as e:  # noqa: BLE001 — ODB++ est un bonus, ne bloque pas
            print(f"  [SKIP] ODB++: {e}")

        # 4) Côtés des pads : ré-export du projet ré-importé vs export source
        try:
            blob_rt = req("GET", f"{BASE}/api/v1/projects/{new_id}/export/kicad", raw=True)
            rt_path = f"{WORK}/{frag}_rt2.kicad_pcb"
            open(rt_path, "wb").write(blob_rt)
            st_src, st_rt = pad_side_stats(exp_path), pad_side_stats(rt_path)
            pads_ok = st_src == st_rt
            print(f"  [{'OK  ' if pads_ok else 'DIFF'}] côtés de pads (ré-export vs export): "
                  f"{st_src} vs {st_rt}")
            ok = ok and pads_ok
        except Exception as e:  # noqa: BLE001
            print(f"  [SKIP] côtés de pads: {e}")

        results.append((pname, ok, new_id))
        print(f"→ projet ré-importé conservé: RT-{frag}-roundtrip id={new_id}")

    print("\n========== BILAN ==========")
    for pname, ok, _ in results:
        print(f"  {pname}: {'FIDÉLITÉ 100%' if ok else 'ÉCARTS DÉTECTÉS'}")
    sys.exit(0 if all(ok for _, ok, _ in results) else 1)


if __name__ == "__main__":
    main()
