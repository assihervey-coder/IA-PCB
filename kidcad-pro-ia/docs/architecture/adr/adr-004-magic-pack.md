# ADR-004 — Magic Pack : fonctions d'intelligence embarquées

- **Statut** : accepté
- **Date** : 2026-09-07
- **Décideurs** : équipe KidCAD-Pro-IA

## Contexte

Le cœur du produit (projets, placement, routage RL, DRC, exports) est en place.
Pour différencier KidCAD-Pro-IA des EDA classiques, nous ajoutons un ensemble
additif de fonctions à haute valeur perçue — le « Magic Pack » — sans toucher
aux contrats figés (`docs/architecture/contracts.md`, proto gRPC, OpenAPI).

## Décision

Cinq capacités, toutes testables et déterministes :

1. **Magic Copilot** (`backend/internal/application/magic/`) — parseur
   langage naturel FR/EN *en Go pur* (aucune dépendance externe, aucun appel
   réseau) qui traduit « place R1 près de U3 », « largeur de piste 12 mil »,
   « drc puis exporte gerber » en actions structurées. Les actions sûres
   (déplacer/poser/supprimer un composant, régler une largeur, créer une
   classe) sont **exécutées directement sur l'agrégat** ; les actions lourdes
   (routage, exports, DRC/ERC) sont **déléguées** au front, qui appelle les
   endpoints existants. Endpoint : `POST /api/v1/projects/{id}/magic`.

2. **Thermal Ghost** (`verification/thermal.go`) — solveur d'équation de la
   chaleur stationnaire (Gauss-Seidel, bords = ambiance) sur grille ~1,2 mm,
   sources = dissipation estimée des composants, étalement cuivre croissant
   avec le nombre de couches. Sortie : carte de chaleur row-major, hotspots
   classés, pire gradient. Endpoint : `POST /api/v1/projects/{id}/thermal`
   (payload what-if optionnel).

3. **Eye Oracle** (`verification/signal_integrity.go`) — analytique SI :
   impédance microstrip (forme fermée IPC-2141), délai de propagation
   (εr effectif), budget de réflexion par via, ouverture d'œil estimée
   (hauteur % et largeur ps) à 1 Gbps, verdict ok/warning/critical +
   conseils. Endpoint : `POST /api/v1/projects/{id}/si`.

4. **AI Arena** (`backend/internal/application/arena/`) — duel de stratégies
   de routage sur le netlist réel : *greedy* (L-Manhattan naïf, compte ses
   collisions) contre *astar* (A* 4-connexe sur grille 1 mm, keep-outs =
   empreintes gonflées, pads libérés pour leurs propres composants).
   Score = nets complétés − longueur − collisions − temps ; classement ELO
   (K=24, persistance processus). Endpoints : `POST .../arena`,
   `GET /api/v1/arena/leaderboard`, récit en direct sur le hub WebSocket
   (stage `arena`).

5. **EvoPlace** (`ai-engine/src/evolution/evo_place.py`) — algorithme
   génétique de placement (tournoi, BLX-α, mutation gaussienne, élitisme,
   fitness HPWL + pénalité de recouvrement), stdlib pur, callback par
   génération pour le streaming. Démo CLI : `python -m src.evolution.evo_place --demo`.

Frontend : **MagicBar** (`frontend/src/app/components/magic/MagicBar.tsx`),
palette ⌘K/Ctrl+K montée dans `AppShell`, aperçu des actions interprétées,
exécution sur Entrée, raccourcis thermique/oracle/arène, styles SCSS
thématisés (section 15 de `globals.scss`).

## Justifications

- **Parseur en Go, pas en LLM** : déterminisme, latence nulle, testable en
  unitaires, aucune fuite de données vers l'extérieur. Le copilot reste
  *instrumental* : il orchestre des use cases existants au lieu de les
  dupliquer.
- **Physique analytique plutôt que simulation SPICE** : réponse instantanée
  (< 100 ms pour 96×96 cellules), déterministe, suffisant pour *classer* les
  risques — pas pour certifier.
- **Additivité** : nouveaux champs `Deps` optionnels (nil ⇒ 503), aucune
  modification des DTOs contractuels ; les anciens déploiements continuent
  de fonctionner.

## Conséquences

- Le classement ELO de l'arène est volatile (mémoire processus) — une
  persistance sera ajoutée quand les combats deviendront multi-utilisateurs.
- Le modèle thermique est un lumped model raisonné (pas un FEM) : les
  valeurs absolues sont indicatives, les *écarts* entre placements sont
  significatifs.
- Le parseur couvre la grammaire documentée dans le guide utilisateur ;
  l'extender reste bon marché (fonctions pures + table de verbes).
