# Guide utilisateur — KidCAD-Pro-IA

> Manuel complet de l'application : interface, éditeur de schéma, éditeur PCB, jobs IA,
> vérifications DRC/ERC, imports/exports, raccourcis clavier.

## Sommaire

1. [L'interface générale](#1-linterface-générale)
2. [Éditeur de schéma](#2-éditeur-de-schéma)
3. [Éditeur PCB](#3-éditeur-pcb)
4. [Jobs IA et logs temps réel](#4-jobs-ia-et-logs-temps-réel)
5. [Vérification : DRC et ERC](#5-vérification--drc-et-erc)
6. [Exports](#6-exports)
7. [Import d'une netlist KiCad](#7-import-dune-netlist-kicad)
8. [Raccourcis clavier](#8-raccourcis-clavier)

---

## 1. L'interface générale

L'application est une **SPA mono-page** organisée autour d'une **barre d'activités**
(colonne de gauche) :

| Icône | Vue | Rôle |
|---|---|---|
| 📁 **Projets** | Liste des projets | Créer, ouvrir, renommer, supprimer, importer une netlist |
| 📐 **Schéma** | Éditeur de schéma électrique | Composants, câblage, nets |
| 🔲 **PCB** | Éditeur de circuit imprimé 2D | Empreintes, pistes, vias, couches |
| 🧊 **3D** | Visualisation Three.js | Aperçu volumétrique de la carte assemblée |
| ✅ **Vérification** | Rapports DRC / ERC | Violations et navigation vers les défauts |
| 📤 **Export** | Exports de fabrication | Gerber ZIP, BOM, STEP, STL, JSON, netlist KiCad |

Autres éléments communs :

- **Barre d'outils contextuelle** en haut du canvas (selon la vue active) ;
- **Panneau de propriétés** à droite (élément sélectionné) ;
- **Panneau de log IA** en bas (visible pendant/après un job IA) avec horodatage et
  progression ;
- **Zoom** : molette ; **pan** : clic milieu ou `Espace` + glisser.

## 2. Éditeur de schéma

### Placer des composants

1. Cliquer **Ajouter un composant** (ou touche `A`) ;
2. Choisir dans la bibliothèque (filtre par nom, ex. `NE555`, `LED`, `R`) ;
3. Cliquer sur le canvas pour déposer ; `R` fait tourner de 90° avant dépôt ;
4. Sélectionner un composant pour éditer ses **propriétés** (référence, valeur,
   empreinte) dans le panneau de droite.

### Câblage clic-borne-à-borne

1. Cliquer **Câbler** (ou `W`) ;
2. Cliquer sur la **borne** du premier composant (les bornes se surlignent au survol) ;
3. Cliquer sur la borne du second composant : un fil est créé et le **net** est nommé
   automatiquement (`N1`, `N2`…) ;
4. `Échap` annule le câblage en cours.

### Gérer les nets

- Clic droit sur un fil → **Renommer le net** : donne un nom explicite
  (`VCC`, `GND`, `CLK`…) — indispensable pour la lisibilité de la netlist ;
- Les nets portant le même nom sont **fusionnés** (utile pour l'alimentation) ;
- Panneau **Nets** (à droite) : liste de tous les nets avec leurs nœuds
  `(ref, pin)` ; double-clic pour surligner le net dans le canvas.

### Bonnes pratiques

- Nommer `VCC` et `GND` explicitement (l'ERC les connaît) ;
- Utiliser des valeurs réalistes (`4.7k`, `10µF`) : elles alimentent la BOM ;
- Vérifier l'empreinte **avant** de passer en PCB (DIP-8 ≠ SOIC-8).

## 3. Éditeur PCB

### Les couches

| Couche | Couleur | Rôle |
|---|---|---|
| F.Cu | 🔴 rouge | Cuivre face supérieure (pistes, pads) |
| B.Cu | 🔵 bleu | Cuivre face inférieure |
| F.Mask / B.Mask | 🟢 vert (masque) | Ouvertures du masque de soudure |
| F.Silk / B.Silk | ⚪ blanc | Sérigraphie (références, logos) |
| Edge.Cuts | 🟡 jaune | Contour de la carte |

Le sélecteur de couche (barre du haut) définit la couche **active** : les pistes
dessinées et le routage IA y sont placés. La case **Afficher tout / couche seule**
assombrit les couches inactives.

### Le ratsnest

Le ratsnest (fines lignes blanches en pointillé) relie chaque paire de pads d'un même
net encore **non routés**. Il se raccourcit à mesure que le placement/routage progresse ;
un ratsnest vide = routage complet. Bouton 👁 pour l'afficher/masquer.

### Outils

| Outil | Rôle |
|---|---|
| **Sélection** (`V`) | Sélectionner/déplacer composants, pistes, vias |
| **Piste** (`W`) | Dessiner une piste sur la couche active (accrochage grille 0.635 mm) |
| **Via** (`P`) | Placer un via (bascule F.Cu ↔ B.Cu), perçage 0.35 mm, Ø 0.7 mm |
| **Contour** (`B`) | Dessiner/éditer le contour de carte (Edge.Cuts) |
| **Texte** (`T`) | Texte de sérigraphie |
| **Gomme** (`E`) | Supprimer piste/via/texte |
| **Contour auto** | Cadre automatique autour du placement |

### Propriétés (panneau droit)

- **Composant** : référence, valeur, empreinte, position X/Y (mm), rotation, couche
  (F.Cu/B.Cu pour les composants CMS) ;
- **Piste** : net, couche, largeur (mm) — alerte si < 0.25 mm (règle DRC) ;
- **Via** : position, perçage, diamètre ;
- **Carte** : dimensions, épaisseur (1.6 mm par défaut).

La grille est de **0.635 mm (25 mil)** : les placements et pistes s'alignent
automatiquement ; bascule `Grille` dans la barre d'état.

## 4. Jobs IA et logs temps réel

Les trois jobs IA sont dans la barre **PCB → Outils IA** et communiquent via
socket.io (`ai:place`, `ai:route`, `ai:optimize`) :

### Placement IA (`ai:place`)

Recuit simulé minimisant le **HPWL** (longueur totale de l'arbre demi-périmètre) plus
des pénalités de chevauchement.

| Option | Défaut | Rôle |
|---|---|---|
| Graine (`seed`) | 42 | Déterminisme du résultat |
| Itérations | 20 000 | Budget de recuit (plus = meilleur, plus long) |
| Température initiale | 1000 | Agitation de départ |

Pendant le job, les composants **bougent en direct** dans le canvas et le panneau de log
affiche `ai:progress {stage, progress, message}` à chaque étape.

### Routage IA (`ai:route`)

A\* maze-routing multi-couches (F.Cu/B.Cu) sur la grille 0.635 mm, coût de via
configurable, **rip-up & reroute** automatique des nets bloqués, puis optimisation de
réduction de vias.

| Option | Défaut | Rôle |
|---|---|---|
| Coût de via | 30 | Pénalité A\* par via (haut = préfère détourner que percer) |
| Passes rip-up & reroute | 5 | Tentatives de déblocage |
| Largeur de piste | 0.25 mm | Respecte la règle DRC |
| Optimiser les vias | ✅ | Post-traitement de réduction |

Le log type :

```text
00:00 [ai:progress] routing  0%   Tri des nets (critiques d'abord)…
00:02 [ai:progress] routing 42%   NET_R1 routé F.Cu, 12 segments
00:05 [ai:progress] routing 71%   K7 bloqué → rip-up & reroute (pass 2/5)
00:08 [ai:result]    stats { routedNets: 26/26, vias: 14, lengthMm: 612, timeMs: 8400 }
```

### Optimisation (`ai:optimize`)

Réduction du nombre de vias (re-routage local sans changer la topologie des nets).
Utile après un routage manuel ou un routage IA « rapide ».

> Si le service IA est arrêté, un message `ai:error` explicite apparaît dans le log et
> les boutons IA sont désactivés. Relancer : `cd mini-services/ai-engine && bun run dev`.

## 5. Vérification : DRC et ERC

Vue **Vérification** → boutons **Lancer DRC** (layout) et **Lancer ERC** (schéma).

### Règles DRC (paramétrables dans `rulesJson`)

| Règle | Défaut |
|---|---|
| Largeur de piste min | 0.25 mm |
| Isolation (clearance) min | 0.2 mm |
| Perçage de via min | 0.35 mm |
| Diamètre de via min | 0.7 mm |
| Distance au bord | 0.3 mm |

### Violations courantes et corrections

| Violation | Cause typique | Correction |
|---|---|---|
| `MIN_TRACK_WIDTH` | Piste dessinée trop fine | Sélectionner la piste → largeur ≥ 0.25 mm |
| `MIN_CLEARANCE` | Deux pistes/pads trop proches | Écarter, ou re-router le net (A\* respecte l'isolation) |
| `VIA_DRILL` / `VIA_SIZE` | Via trop petit | Via standard 0.35/0.7 mm |
| `EDGE_CLEARANCE` | Cuivre à moins de 0.3 mm du bord | Déplacer la piste ou agrandir le contour |
| `UNROUTED_NET` | Ratsnest restant | Lancer le routage IA ou router à la main |

### Violations ERC

| Violation | Signification |
|---|---|
| `SHORTED_OUTPUTS` | Deux sorties reliées au même net |
| `FLOATING_NET` | Net à une seule borne |
| `FLOATING_INPUT` | Entrée non connectée |
| `POWER_UNCONNECTED` | VCC/GND absent du projet |

Chaque violation est cliquable : le canvas se centre sur le défaut concerné.

## 6. Exports

Vue **Export** — chaque bouton télécharge le fichier correspondant
(`GET /api/projects/{id}/export/{format}`) :

### Gerber ZIP (format `gerber`)

Archive contenant les couches **RS-274X** + le perçage **Excellon** :

| Fichier | Extension usuelle | Couche |
|---|---|---|
| `*.F_Cu.gbr` | `.GTL` | Cuivre top |
| `*.B_Cu.gbr` | `.GBL` | Cuivre bottom |
| `*.F_Mask.gbr` | `.GTS` | Masque de soudure top (brillance = ouverture) |
| `*.B_Mask.gbr` | `.GBS` | Masque de soudure bottom |
| `*.F_Silk.gbr` | `.GTO` | Sérigraphie top |
| `*.B_Silk.gbr` | `.GBO` | Sérigraphie bottom |
| `*.Edge_Cuts.gbr` | `.GKO` | Contour de carte |
| `*.drl` | — | Perçages (Excellon) : trous de vias et de composants |

Le ZIP est directement accepté par les fabricants en ligne. **Penser à lancer le DRC
avant** — un export peut contenir des violations non résolues.

### BOM CSV (format `bom`)

Colonnes : `Référence, Valeur, Empreinte, Quantité` — groupée par (valeur, empreinte).
Ouvrable dans n'importe quel tableur (UTF-8, séparateur `,`).

### STEP (format `step`)

**AP214 simplifié** : la plaque et les composants modélisés en boîtes (empreinte 3D
approximative). Pour l'insertion dans un CAO mécanique (FreeCAD, SolidWorks).

### STL (format `stl`)

Maillage triangulé de la plaque seule — impression 3D d'un « mockup » de la carte.

### JSON (format `json`)

Sauvegarde native complète (schematicJson + layoutJson + rulesJson) — réimportable
comme projet de secours.

### Netlist KiCad (format `netlist-kicad`)

S-expression KiCad du schéma courant — utilisable pour comparer avec un schéma KiCad
ou alimenter un outil tiers.

## 7. Import d'une netlist KiCad

Vue **Projets → Importer une netlist** (ou `POST /api/projects/{id}/import`) :

1. Choisir le **format** : `kicad` (s-expression) ou `json` ;
2. Coller le contenu ou sélectionner un fichier `.net` (essai :
   `tests/fixtures/led-chaser.netlist.kicad.net`) ;
3. Valider.

Le parseur lit les sections `(components …)` (référence, valeur, empreinte, broches)
et `(nets …)` (code, nom, nœuds `(ref, pin)`), puis :

- crée les composants du schéma avec leurs empreintes ;
- reconstruit les nets et le ratsnest en vue PCB ;
- les composants existants portant la même référence sont mis à jour (pas dupliqués).

Erreurs courantes : `(export` manquant, parenthèse déséquilibrée → message `PARSE_ERROR`
avec la ligne approximative.

## 8. Raccourcis clavier

| Touche | Action | Contexte |
|---|---|---|
| `V` | Outil Sélection | Schéma & PCB |
| `A` | Ajouter un composant | Schéma |
| `W` | Outil Câbler / Piste | Schéma & PCB |
| `P` | Placer un via | PCB |
| `B` | Outil Contour de carte | PCB |
| `T` | Texte de sérigraphie | PCB |
| `E` | Gomme | Schéma & PCB |
| `R` | Rotation 90° (sélection) | Schéma & PCB |
| `Suppr` / `Backspace` | Supprimer la sélection | Schéma & PCB |
| `Ctrl/Cmd + S` | Sauvegarder le projet | Global |
| `Ctrl/Cmd + Z` | Annuler | Global |
| `Ctrl/Cmd + Y` / `Ctrl/Cmd + Maj + Z` | Rétablir | Global |
| `1` … `6` | Vues Projets/Schéma/PCB/3D/Vérif/Export | Global |
| `G` | Bascule grille | PCB |
| `M` | Bascule couche active F.Cu ↔ B.Cu | PCB |
| `F` | Zoom sur tout | Global |
| `Échap` | Annuler l'outil en cours | Global |
