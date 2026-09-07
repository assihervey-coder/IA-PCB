#!/usr/bin/env python3
"""Diagnostic des écarts géométriques round-trip : isole les segments
manquants/ajoutés et montre les pistes source/ré-importées autour."""
import json
import sys
import urllib.request

sys.path.insert(0, "/home/z/my-project/scripts")
from roundtrip_test import (BASE, TOKEN, req, elem_segments,  # noqa: E402
                            reduce_collinear)

SRC = {"DEMO4": "5a40f93b-1bd1-43f0-a7c1-c661dc02df8b"}
RT_NAME = "RT-DEMO4-roundtrip"


def layout(pid):
    return req("GET", f"{BASE}/api/v1/projects/{pid}/layout")


def main():
    data = req("GET", BASE + "/api/v1/projects")
    ps = data.get("projects", data)
    rt_pid = next(p["id"] for p in ps if p["name"] == RT_NAME)
    src = layout(SRC["DEMO4"])
    rt = layout(rt_pid)

    ss, rs = elem_segments(src["tracks"]), elem_segments(rt["tracks"])
    missing = ss - rs
    added = rs - ss
    print(f"manquants={sum(missing.values())} ajoutés={sum(added.values())}\n")

    def show(track, tag):
        red = reduce_collinear(track["points"])
        pts = " → ".join(f"({p['x']:.4f},{p['y']:.4f})" for p in track["points"])
        print(f"    {tag}: net={track.get('net')} layer={track.get('layer')} "
              f"w={track.get('width')} pts={len(track['points'])} (réduits={len(red)})")
        print(f"      {pts}")

    # pour chaque segment manquant, piste source qui le contient + pistes RT proches
    for key, n in list(missing.items())[:6]:
        (ax, ay), (bx, by), layer, w = key
        print(f"\n### MANQUANT ×{n}: ({ax},{ay})→({bx},{by}) L{layer} w={w}")
        for t in src["tracks"]:
            pts = t.get("points", [])
            if t.get("layer") != layer or abs(t.get("width", 0) - w) > 1e-9:
                continue
            red = reduce_collinear(pts)
            for a, b in zip(red, red[1:]):
                k = ((round(a[0], 3), round(a[1], 3)), (round(b[0], 3), round(b[1], 3)))
                ka = k if k[0] <= k[1] else (k[1], k[0])
                if ka == ((min(ax, bx), min(ay, by)), (max(ax, bx), max(ay, by))) or \
                   {(round(ax, 3), round(ay, 3)), (round(bx, 3), round(by, 3))} == set(ka):
                    show(t, "SRC")
                    break
        # pistes RT dans le voisinage (≤0.5mm du centre du segment)
        cx, cy = (ax + bx) / 2, (ay + by) / 2
        for t in rt["tracks"]:
            if t.get("layer") != layer or abs(t.get("width", 0) - w) > 1e-9:
                continue
            if any(abs(p["x"] - cx) < 0.6 and abs(p["y"] - cy) < 0.6 for p in t["points"]):
                show(t, "RT ")

    for key, n in list(added.items())[:4]:
        (ax, ay), (bx, by), layer, w = key
        print(f"\n### AJOUTÉ ×{n}: ({ax},{ay})→({bx},{by}) L{layer} w={w}")


if __name__ == "__main__":
    main()
