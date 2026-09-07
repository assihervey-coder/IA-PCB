# Guide utilisateur — YahriaCad

> Manuel de référence de l'interface. UI en français, thème sombre professionnel.

## 1. Concepts clés

| Concept | Définition |
|---|---|
| **Projet** | Conteneur principal : métadonnées, schéma, layout et règles de conception. |
| **Net** | Nœud électrique reliant plusieurs broches (ex. `VCC`, `GND`, `SIG`). Deux broches d'un même net doivent finir reliées par du cuivre. |
| **Empreinte (footprint)** | Encombrement physique d'un composant : pads, corps, hauteur 3D (ex. `R_0603_1608Metric`, `SOIC-8_3.9x4.9mm_P1.27mm`). |
| **Couche** | Niveau de cuivre. L'indice 0 est **F.Cu** (dessus), le dernier **B.Cu** (dessous). Une carte standard est deux couches. |
| **Piste (track)** | Polyligne de cuivre sur une seule couche, largeur typique 0,25 mm. Les changements de couche se font par **via**. |
| **Via** | Cylindre de cuivre reliant deux couches (diamètre 0,6 mm, perçage 0,3 mm par défaut). |
| **DRC** | *Design Rule Check* : vérification géométrique (clearances, largeurs, vias, bord de carte). |
| **ERC** | *Electrical Rule Check* : vérification logique du schéma (nets à une broche, références dupliquées, composants non connectés). |
| **Stratégie `astar`** | Routage déterministe A* multi-couches, toujours disponible, résultat reproductible. |
| **Stratégie `rl`** | Routage par apprentissage par renforcement (PPO) si un checkpoint `.pt` est chargé ; sinon bascule automatique sur A*. |

## 2. Gestionnaire de projets (`/pages/project-manager`)

Première page de l'application. Elle affiche la **grille des projets**
(`projects-grid`) : chaque carte (`project-card`) montre le nom, la
description, le nombre de couches et la date de modification.

Actions :

- **Nouveau projet** : ouvre le formulaire (nom obligatoire, description,
  nombre de couches 1 à 34, défaut 2).
- **Ouvrir** : sélectionne le projet et charge le design dans les autres pages.
- **Supprimer** : confirmation puis suppression définitive.

> En cas d'API injoignable, la page bascule en **mode démo** avec une bannière
> (`demo-banner`) et un projet d'exemple en lecture seule.

## 3. Éditeur de schéma (`/pages/schematic-editor`)

Vue logique de la carte : composants (symboles), nets et connexions
borne-à-borne.

- Palette à gauche : résistances, condensateurs, CI, connecteurs.
- Clic sur un composant : sélection et édition de la valeur/empreinte.
- Les nets sont calculés à partir des connexions ; le panneau de droite
  liste chaque net et le nombre de broches.
- Canvas SVG (`schematic-canvas`) : zoom à la molette, pan au glisser.

Après édition, relancez l'**ERC** (page Vérification) pour valider la
cohérence électrique avant le placement.

## 4. Éditeur PCB (`/pages/pcb-layout`)

Vue physique : carte, empreintes, pistes, vias. Canvas 2D (`pcb-canvas`).

Interactions souris :

| Action | Contrôle |
|---|---|
| Pan (déplacer la vue) | Glisser avec le bouton **clic droit** ou clic molette |
| Zoom avant/arrière | **Molette** (centré sur le curseur) |
| Sélectionner un composant | Clic gauche sur l'empreinte |
| Déplacer un composant | Glisser-gauche, snap sur la grille |
| Rotation d'un composant | Touche **R** pendant la sélection (par pas de 90°) |
| Déplacer une piste | Clic + glisser sur un segment |
| Supprimer la sélection | Touche **Suppr** |

Panneau des couches : cochez/décochez F.Cu, B.Cu (et couches internes) pour
filtrer l'affichage. Le **ratsnest** (lignes pointillées des nets non routés)
se dévoile au fur et à mesure du routage. `Ctrl+S` enregistre le layout
(`PUT /layout`).

## 5. Viewer 3D (`/pages/pcb-layout/viewer`)

Rendu Three.js de la carte : plaque verte 1,6 mm, composants extrudés à leur
hauteur réelle, vias en cylindres.

| Action | Contrôle |
|---|---|
| Orbiter autour de la carte | Glisser **clic gauche** |
| Zoom | **Molette** |
| Déplacer le point de vue | Glisser **clic droit** |

Le viewer charge la géométrie depuis le layout enregistré ; modifiez la vue
PCB puis `Ctrl+S` avant d'ouvrir le 3D pour voir vos changements.

## 6. Routage IA (`/pages/pcb-layout/router`)

Page dédiée au moteur IA :

1. **Stratégie** (`route-strategy`) : `astar` (déterministe) ou `rl`
   (modèle entraîné, sinon repli A* automatique).
2. **Filtre de nets** (optionnel) : router uniquement les nets sélectionnés
   (utile pour reprendre un routage partiel).
3. **Lancer** (`route-start-btn`) : le job part en arrière-plan
   (`202 JobStarted`), la **barre de progression** (`progress-bar`) et le
   **journal** (`progress-log`) s'animent en temps réel via WebSocket :
   un message par net terminé (longueur, nombre de vias), puis le bilan
   final (nets complétés, durée, violations DRC résiduelles).
4. À la fin, les pistes sont appliquées au layout et enregistrées ; un
   **DRC** est recommandé.

**Optimiser** relance un passage de rip-up & reroute pour raccourcir les pires
nets et réduire le nombre de vias.

## 7. Vérification

Deux boutons lancent les contrôles sur le design courant :

- **DRC** (`drc-run-btn`) : clearances piste/piste/pad/via, cuivre/bord
  (0,5 mm par défaut), largeur de piste (0,2 mm), diamètre de via (0,6 mm),
  perçage (0,3 mm), anneau annulaire (0,15 mm). Chaque violation est listée
  avec son code (`DRC_CLEARANCE`, `DRC_EDGE_CLEARANCE`, …), sa position et
  sa gravité.
- **ERC** (`erc-run-btn`) : `ERC_SINGLE_PIN_NET` (avertissement),
  `ERC_DUPLICATE_REF`, `ERC_EMPTY_NET_NAME`, `ERC_MISSING_FOOTPRINT`,
  `ERC_UNCONNECTED_COMPONENT`.

`passed` reste vrai tant qu'aucune violation de gravité `error` n'est
détectée ; les avertissements n'empêchent pas l'export.

## 8. Export (`/pages/export`)

| Bouton | Résultat |
|---|---|
| **Gerber** (`export-gerber-btn`) | ZIP : `-F_Cu.gbr`, `-B_Cu.gbr`, masques, sérigraphies, `-Edge_Cuts.gbr` (RS-274X, mm, 4.6) |
| **BOM** (`export-bom-btn`) | CSV `Ref;Qty;Value;Footprint`, références groupées |
| **STEP** (`export-step-btn`) | Modèle 3D AP214 (plaque + boîtes composants) pour la mécanique |
| **Import** (`import-input`) | Recharge un design : `.kicad_pcb`, `.net`, `.brd`/`.sch` Eagle, `.yahriacad.json` |

## 9. FAQ

**Le placement IA déplace mon connecteur d'alimentation, comment le figer ?**
Cochez « Fixe » dans les propriétés du composant (ou verrouillez-le) : les
composants `fixed` sont ignorés par le moteur.

**`rl` et `astar` donnent-ils le même résultat ?**
`astar` est déterministe : même carte + même graine = même routage. `rl`
exploration différente et résultats variables ; sans checkpoint, il bascule
sur A* et le comportement devient identique à `astar`.

**Pourquoi l'ERC signale-t-il des nets à une seule broche ?**
Un net doit relier au moins deux broches. Une broche isolée est souvent un
symbole mal câblé — corrigez le schéma ou supprimez le net.

**Puis-je travailler hors ligne ?**
Oui : sans backend, l'application bascule en mode démo (projet d'exemple).
Les modifications ne sont alors pas persistées.

**Le DRC est vert mais mon fabricant demande 0,3 mm d'isolement.**
Ajoutez/surveillez la règle « Isolation minimale entre nets » : les valeurs
par défaut (0,2 mm) sont adaptées aux prototypages, pas aux fabriques
grand public. Ajustez avant le routage.

**Quelle est la précision des exports ?**
Les Gerber sont en millimètres avec 4 décimales (FSLAX46Y46). Les coordonnées
internes sont stockées en double précision ; les vérifications DRC utilisent
une tolérance par défaut largement suffisante pour des cartes deux couches.
