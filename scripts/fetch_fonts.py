#!/usr/bin/env python3
"""Télécharge Inter + JetBrains Mono (latin/latin-ext) et génère fonts.css local."""
import re, os, urllib.request

OUT = "/home/z/my-project/download/fonts"
os.makedirs(OUT, exist_ok=True)

FAMILIES = [
    ("Inter", [300, 400, 500, 600, 700, 800, 900]),
    ("JetBrains Mono", [400, 500, 700]),
]

css_out = []
for fam, weights in FAMILIES:
    for w in weights:
        # Sans UA => l'API renvoie des .ttf (compat maximal avec Chromium local)
        url = f"https://fonts.googleapis.com/css2?family={fam.replace(' ', '+')}:wght@{w}"
        req = urllib.request.Request(url, headers={"User-Agent": "curl/8.0"})
        css = urllib.request.urlopen(req, timeout=20).read().decode()
        # réponse sans UA : un bloc @font-face TTF unique par requête
        for face in re.findall(r"@font-face \{[^}]+\}", css):
            m = re.search(r"url\((https://[^)]+\.ttf)\)", face)
            if not m:
                continue
            src = m.group(1)
            fname = f"{fam.replace(' ', '')}-{w}.ttf"
            dest = os.path.join(OUT, fname)
            if not os.path.exists(dest):
                urllib.request.urlretrieve(src, dest)
            face = face.replace(src, fname)          # chemin relatif
            face = re.sub(r"unicode-range:[^;]+;", "", face)  # plus de range CDN
            css_out.append(f"/* {fam} {w} */\n{face}")
            print(f"ok {fname}")

with open(os.path.join(OUT, "fonts.css"), "w", encoding="utf-8") as f:
    f.write("\n".join(css_out))
print(f"\nfonts.css : {len(css_out)} faces")
