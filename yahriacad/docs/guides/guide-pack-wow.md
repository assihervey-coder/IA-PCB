# Pack WOW — fonctions qui surprennent les experts

Ce document décrit le « Pack WOW » ajouté à YahriaCad : les fonctions
« qui surprendront 99,9 % des experts » (wow), les « good to have » et les
« best to have ». Tous ces ajouts sont **additifs** et restent hors du
contrat figé `docs/architecture/contracts.md` (même logique que le Magic
Pack).

## 1. Fonctions WOW

### 1.1 DRC Auto-Healer — auto-réparation des violations

Là où les EDA classiques s'arrêtent à la liste des violations, l'Auto-Healer
propose un correctif concret pour chacune, applique les réparations sûres,
persiste la carte réparée puis **re-vérifie réellement** le DRC (delta
avant/après).

| Code violation | Réparation | Confiance |
|---|---|---|
| `DRC_TRACK_WIDTH` | élargit les pistes du net à la valeur de règle | 95 % |
| `DRC_VIA_DIAMETER` / `DRC_DRILL` / `DRC_ANNULAR` | agrandit via + perçage en respectant l'anneau annulaire | 90 % |
| `DRC_EDGE_CLEARANCE` | repositionne vias / points de piste à l'intérieur de la marge de bord | 85 % / 65 % |
| `DRC_CLEARANCE` | non réparable automatiquement → suggestion de rip-up & re-route (0 %, non appliqué) | — |

- Endpoint : `POST /api/v1/projects/{id}/drc/autofix` — corps
  `{"dry_run": true}` pour une simulation sans écriture.
- Sécurité : le service travaille sur un **clone profond** de la carte ; en
  dry-run rien n'est persisté (vérifié par test).
- UI : bouton **🩹 Auto-fix** dans la barre d'outils de l'éditeur PCB, le
  rapport s'affiche sous la carte avec les confiances par correctif.

### 1.2 Design Doctor — audit global noté

Fusionne tous les axes de vérification de la plateforme en une seule revue :

- **Note globale 0–100** pondérée : DRC 25 %, SI 25 %, Routage/DFM 25 %,
  Thermique 15 %, ERC 10 %.
- **Grade** A+ (≥93) / A / B / C / D avec verdict en français.
- **Radar 5 axes** (SVG pur côté UI).
- **Ordonnances priorisées** avec gain de points estimé (ex. « Lancer
  l'Auto-Healer DRC — +12,5 pts »).
- Métriques brutes : occupation, longueur de cuivre, géométrie minimale,
  nets non routés, point chaud, score SI…

- Endpoint : `GET /api/v1/projects/{id}/doctor`.
- UI : bouton **🩺 Diagnostic**.

### 1.3 Oracle DFM — coût & rendement de fabrication

Estimateur de manufacturabilité **transparent** (chaque surtaxe est listée) :

- **Prix unitaire** à 3 volumes (10 / 100 / 1000 pièces) avec remises de
  volume et amortissement de l'outillage.
- Surtaxes détaillées : gravure fine (<0,15 mm), forage laser (<0,25 mm),
  densité de vias (>50/cm²), couches supplémentaires.
- **Rendement premier passage** (modèle d'Erlang par cause de défauts) et
  niveau de risque (faible / moyen / élevé).
- Conseils d'optimisation chiffrés (ex. « élargir à 0,25 mm ≈ −28 % »).

- Endpoint : `POST /api/v1/projects/{id}/dfm`.
- UI : bouton **💰 DFM** — cartes de prix, jauge de rendement, chips de
  risque.

### 1.4 Time Machine — snapshots, diff et restauration

Historique de voyage dans le temps du design :

- **Capture** d'un instantané (carte + règles) — anneau de 20 par projet.
- **Diff structurel** lisible : composants ajoutés/supprimés/déplacés,
  cuivre par net (pistes + longueur), vias, règles modifiées.
- **Restauration** en un clic — l'état actuel est capturé automatiquement
  avant restauration, donc le voyage est toujours **réversible**.

- Endpoints : `POST/GET /api/v1/projects/{id}/snapshots`,
  `GET .../snapshots/{sid}/diff`,
  `POST .../snapshots/{sid}/restore`.
- UI : bouton **🕰 Historique**.

### 1.5 Routage interactif au net — « cliquer et router »

Là où les EDA demandent de configurer un job de routage complet, YahriaCad
route **un seul net à la demande**, directement depuis l'éditeur :

- Liste des nets (panneau latéral de l'éditeur PCB) : bouton **⚡** par net
  → `POST /api/v1/projects/{id}/route` avec `{"strategy":"astar",
  "nets":["NET"]}`. Le moteur respecte les règles de **classe de nets**
  (largeur, dégagement) et le backend persiste la carte, en un job
  suivi (REST + WebSocket), suivi par sondage puis rafraîchissement
  automatique du layout.
- Le routage complet reste disponible dans la page **Routage IA** :
  sélection de la stratégie A*/RL + filtre multi-nets + journal temps réel.

### 1.6 Modèle RL (PyTorch) — inspection et rechargement à chaud

La plateforme embarque un routeur RL PPO (PyTorch) : tant qu'aucun
checkpoint n'est chargé, le moteur replie sur A* déterministe — mais
l'expert garde la main sur le modèle :

- `GET /api/v1/ai/model` — état détaillé du modèle : chargé ou non,
  device d'inférence (`cpu`/`cuda`/`none`), chemin + horodatage + taille du
  checkpoint, architecture (canaux d'observation, actions), nombre de
  paramètres, disponibilité de torch, stratégie effective (`rl`/`astar`).
- `POST /api/v1/ai/model/reload` — **rechargement à chaud** du checkpoint,
  sans redémarrer le moteur ni le backend. Corps optionnel
  `{"checkpoint_path": "…"}` pour basculer sur un autre `.pt` (flux nominal
  après `make train-router`). En cas d'échec, le modèle précédent est
  conservé et le repli A* reste actif — le moteur ne perd jamais la main.
- RPC gRPC correspondantes dans le contrat (additif) :
  `GetModelInfo` / `ReloadModel` (`yahriacad.pcb.v1`).
- UI : panneau **Modèle RL (PyTorch)** dans la page Routage IA — badge
  chargé/absent, device, paramètres, bouton **⟳ Recharger le modèle**.

## 2. Good to have

### 2.1 Statistiques live

`GET /api/v1/projects/{id}/stats` — compteurs (composants, pads, nets
routés), longueur de cuivre totale, occupation, cuivre par couche, top 5
des nets les plus longs, agrégat par classe. UI : bouton **📊 Stats**
(panneau latéral).

### 2.2 Raccourcis clavier — aide-mémoire

Touche **?** : overlay listant tous les raccourcis (⌘K copilot, Ctrl+Z,
zoom…). Composant global monté dans l'AppShell.

### 2.3 Commandes vocales

Bouton **🎙** dans la MagicBar (Web Speech API, `fr-FR`) : dictez
« place R1 près de U3 », la transcription remplit le champ puis s'exécute
comme une commande normale. Désactivé proprement si le navigateur ne
supporte pas l'API.

## 3. Best to have

### 3.1 Présence collaborative temps réel

Le hub WebSocket existant (`/ws/v1/progress`) est étendu (additif) :

- Client → serveur : `{"type":"presence","user":"Alice","cursor":{"x":12.4,"y":8.1},"tool":"layout"}`.
- Serveur → autres abonnés du projet : mêmes données + événements
  `join` / `leave` synthétisés.
- UI : **curseurs colorés nommés** des autres collaborateurs (superposés au
  canvas) + pile d'avatars ; diffusion locale throttlée à ~11 Hz ;
  nettoyage des pairs fantômes par garbage collector 15 s.

### 3.2 Undo / Redo global

- Pile d'historique locale (30 niveaux) dans le store Zustand.
- **Ctrl+Z / Ctrl+Maj+Z (Ctrl+Y)** + boutons ↶/↷ dans la barre d'outils.
- Les mutations historisées : placement IA, édition de propriétés,
  Auto-fix, restauration Time Machine. L'annulation réécrit le layout via
  `PUT /layout` (le backend reste la source de vérité).

## 4. Tests

Chaque fonction WOW possède ses tests Go (`go test ./...`) :

- `verification/autofix_test.go` — réparation par famille, isolation du
  dry-run, clone profond.
- `doctor/doctor_test.go` — bornes des scores, axes, ordonnances triées.
- `dfm/estimator_test.go` — prix cohérents, remises de volume, détection de
  géométrie agressive.
- `timemachine/timemachine_test.go` — capture/diff/restore aller-retour,
  réversibilité, anneau plafonné.
- `stats/service_test.go` — agrégats (longueurs, couches, classes).
