#!/usr/bin/env python3
"""Sous-ensemble latin des polices + génération @font-face en data URI inline."""
import base64, io, os, re, subprocess

FONTS = "/home/z/my-project/download/fonts"
HTML = "/home/z/my-project/download/yahriacad-guide-demarrage.html"

# Glyphes utiles : latin de base + latin-1 (accents FR, guillemets) + ponctuation typographique
UNICODE = "U+0020-007E,U+00A0-00FF,U+0152-0153,U+2010-2027,U+2030-203A,U+20AC,U+2190-2199"

def subset(src, dest):
    subprocess.run([
        "python3", "-m", "fontTools.subset", src,
        f"--unicodes={UNICODE}",
        "--layout-features=kern,liga",
        "--output-file=" + dest,
    ], check=True)

faces = []
for fam, weights, cssname in [
    ("Inter", [300, 400, 500, 600, 700, 800, 900], "Inter"),
    ("JetBrains Mono", [400, 500, 700], "JetBrainsMono"),
]:
    for w in weights:
        src = os.path.join(FONTS, f"{cssname}-{w}.ttf")
        sub = os.path.join(FONTS, f"sub-{cssname}-{w}.ttf")
        subset(src, sub)
        b64 = base64.b64encode(open(sub, "rb").read()).decode()
        size_kb = os.path.getsize(sub) // 1024
        faces.append(
            "@font-face { font-family: '%s'; font-style: normal; font-weight: %d; "
            "src: url(data:font/ttf;base64,%s) format('truetype'); }"
            % (fam, w, b64)
        )
        print(f"{cssname}-{w}: {size_kb} Ko")

block = ("<style>\n/* Polices sous-ensemblisées, embarquées en data URI "
         "(aucune dépendance réseau ni CORS) */\n" + "\n".join(faces) + "\n</style>")

s = open(HTML, encoding="utf-8").read()
# remplace le bloc de polices inline précédent (peu importe son commentaire)
s = re.sub(r'<style>\n/\* Polices (?:auto-hébergées|sous-ensemblisées)[^<]*</style>', block, s, flags=re.S)
s = re.sub(r'<link href="fonts/fonts\.css" rel="stylesheet">', block, s)
open(HTML, "w", encoding="utf-8").write(s)
print("HTML mis à jour :", len(block) // 1024, "Ko de polices inline")
