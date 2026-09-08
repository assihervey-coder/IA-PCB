# Fixtures de test — YahriaCad

Jeu de fichiers d'exemple utilisés par les tests d'intégration Go
(`tests/integration/`), la documentation et l'import manuel via l'interface.

| Fichier | Format | Rôle |
|---|---|---|
| `sample.kicad_pcb` | KiCad PCB s-expression (subset, contracts.md §7) | Carte physique complète : empreintes, pads, segments, via, contour. Utilisé par `TestImportFixtureAndERC` (lecteur `fileio/reader`) et le guide de démarrage rapide. |
| `sample.netlist` | Netlist KiCad (s-expression, `.net`) | Netlist électrique seule : 4 composants, 3 nets cohérents (chaque `node` référence un composant existant). Sert au lecteur générique de netlists. |
| `sample-project.json` | Échange natif `.yahriacad.json` (valide contre `shared/schemas/interchange.schema.json`) | Projet complet schéma + layout : 3 composants, 3 nets, carte 50×40 mm 2 couches, 2 pistes, 1 via. Sert au lecteur du format d'échange natif. |

## `sample.kicad_pcb` en détail

- **Carte** : contour `Edge.Cuts` 50 × 40 mm, épaisseur 1,6 mm, 2 couches
  (`F.Cu` indice 0, `B.Cu` indice 31).
- **Nets** : `0` = "" (sans nom), `1` VCC, `2` GND, `3` SIG, `4` OUT.
- **Empreintes** (4) :
  - `R1` — 0603, 2 pads SMD sur F.Cu (VCC / GND), valeur 10k ;
  - `C1` — 0805, 2 pads SMD sur F.Cu (VCC / GND), valeur 100n ;
  - `U1` — SOIC-8, 8 pads SMD sur F.Cu (SIG, OUT, GND, VCC) ;
  - `J1` — Header 2 broches, pads **traversants** (`(drill 1.0)`, couches
    `*.Cu`/`*.Mask`) : couvre le cas « pad dupliqué sur toutes les couches »
    du parser.
- **Cuivre** : 4 segments sur `F.Cu` net VCC + 1 segment sur `B.Cu`
  (démonstration d'un changement de couche), 1 via `(size 0.6) (drill 0.3)`
  reliant F.Cu ↔ B.Cu sur le net VCC.
- **Invariant testé** : le fichier doit se parser sans erreur et produire
  ≥ 2 nets, ≥ 4 pads (en pratique 14), ≥ 4 empreintes, ≥ 1 via, des segments
  et le contour `gr_rect`.

## Mise à jour des fixtures

Toute modification doit respecter :

1. la **grammaire** du subset KiCad (contracts.md §7) — parenthèses
   équilibrées, coordonnées en millimètres dans le contour de la carte ;
2. la **cohérence** de `sample.netlist` (tous les `(ref …)` des nodes
   existent dans `(components …)`) ;
3. la **validation** de `sample-project.json` contre
   `shared/schemas/interchange.schema.json`.

Vérification rapide des parenthèses (chaînes ignorées) :

```bash
python3 - <<'EOF'
for path in ("sample.kicad_pcb", "sample.netlist"):
    txt = open(path).read()
    out, in_str = [], False
    for ch in txt:
        if ch == '"':
            in_str = not in_str
        elif not in_str and ch in "()":
            out.append(ch)
    print(path, "OK" if out.count("(") == out.count(")") else "DESEQUILIBRE")
EOF
```
