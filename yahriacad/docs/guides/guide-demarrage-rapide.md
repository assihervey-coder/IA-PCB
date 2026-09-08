# Guide de démarrage rapide — YahriaCad

> De zéro à un PCB routé et exporté en une quinzaine de minutes.
> Ce tutoriel fait tourner les **trois modules** du monorepo : le backend Go
> (REST + WebSocket), le moteur IA Python (gRPC) et le frontend Next.js.

## 1. Prérequis

| Outil | Version minimale | Vérification |
|---|---|---|
| Go | 1.22 | `go version` |
| Python | 3.10 | `python3 --version` |
| Node.js + npm | 20 | `node --version` |
| make | n'importe laquelle | `make --version` |

Optionnels : Docker + Docker Compose (pour la stack conteneurisée),
`grpcio-tools` (généré automatiquement par `make proto`).

## 2. Installation des dépendances

Depuis la racine du monorepo :

```bash
bash scripts/setup-dev.sh
```

Le script vérifie la toolchain, crée l'environnement virtuel Python
(`.venv/`), installe les dépendances Go, Python et Node, puis génère les
stubs gRPC (`make proto`). Alternative manuelle :

```bash
go mod download
python3 -m pip install -r requirements.txt grpcio-tools
cd frontend && npm install && cd ..
make proto
```

## 3. Lancer les trois services

Ouvrez trois terminaux (ou utilisez `make` cible par cible) :

```bash
# Terminal 1 — moteur IA gRPC (port 50051)
make run-ai

# Terminal 2 — backend Go, REST + WebSocket (port 8080)
make run-backend

# Terminal 3 — frontend Next.js (port 3000)
make run-frontend
```

Vérifications rapides :

```bash
curl http://localhost:8080/healthz
# {"status":"ok","version":"0.1.0","ai_engine":"ok","database":"memory"}
```

> **Remarque** : sans `YAHRIACAD_DB_URL`, le backend utilise l'adaptateur
> mémoire (`"database":"memory"`), parfait pour la découverte. Avec Docker
> (`docker compose -f docker/docker-compose.yml up -d --build`), PostgreSQL
> 16 est démarré automatiquement.

## 4. Créer un projet

Ouvrez [http://localhost:3000](http://localhost:3000) : vous arrivez sur le
**gestionnaire de projets**. Cliquez sur **Nouveau projet** puis saisissez :

- Nom : `Carte démo 60x40`
- Description : `Import de la fixture sample.kicad_pcb`
- Couches : `2`

Vous pouvez également le créer en ligne de commande :

```bash
curl -s -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"Carte démo 60x40","description":"Fixture sample","layer_count":2}'
```

Notez l'`id` renvoyé ; il servira dans les appels suivants (`<ID>`).

## 5. Importer une carte existante

Importez la fixture fournie (`tests/fixtures/sample.kicad_pcb`, carte 50x40 mm
avec 4 empreintes, 4 nets, segments, via et contour) :

- **Via l'interface** : ouvrez le projet, page **Import**, sélectionnez le
  fichier `tests/fixtures/sample.kicad_pcb`, validez.
- **Via curl** :

```bash
curl -s -X POST http://localhost:8080/api/v1/projects/<ID>/import \
  -F "file=@tests/fixtures/sample.kicad_pcb"
```

La réponse récapitule le format détecté (`kicad`) et le nombre de composants,
nets, pistes et vias importés. Ouvrez la page **PCB Layout** : les empreintes
R1, C1, U1 et J1 apparaissent sur la carte.

## 6. Placement automatique (IA)

Page **PCB Layout**, cliquez sur **Placement IA** (stratégie `heuristic` par
défaut — recuit simulé déterministe ; `rl` utilise le modèle entraîné si un
checkpoint `.pt` a été généré via `make models`). Les composants sont
repositionnés pour minimiser la longueur de câblage (HPWL).

En REST :

```bash
curl -s -X POST http://localhost:8080/api/v1/projects/<ID>/place \
  -H "Content-Type: application/json" -d '{"strategy":"heuristic"}'
```

## 7. Routage automatique avec progression en direct

Page **Routage IA**, choisissez la stratégie (`astar` par défaut) et cliquez
sur **Lancer le routage**. Le journal en bas de page affiche la progression
net par net (barre de progression + messages temps réel via WebSocket) :

```
[route] net VCC terminé (3 pts, 12,5 mm, 1 via)
[route] net GND terminé (4 pts, 18,75 mm, 0 via)
[route] terminé : 4/4 nets, 100 %
```

En REST (le job est asynchrone) :

```bash
JOB=$(curl -s -X POST http://localhost:8080/api/v1/projects/<ID>/route \
  -H "Content-Type: application/json" -d '{"strategy":"astar"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["job_id"])')

curl -s http://localhost:8080/api/v1/projects/<ID>/jobs/$JOB
```

Une fois l'état `"state":"done"` atteint, les pistes apparaissent sur le
canvas PCB (bleu = F.Cu, rouge = B.Cu).

## 8. Vérification DRC / ERC

Page **Vérification**, lancez **DRC** (règles de conception : clearances,
largeur de piste, vias, bord de carte) puis **ERC** (règles électriques :
nets à une broche, références dupliquées, etc.). Le rapport liste chaque
violation avec son code, sa position et sa gravité.

```bash
curl -s -X POST http://localhost:8080/api/v1/projects/<ID>/drc
curl -s -X POST http://localhost:8080/api/v1/projects/<ID>/erc
```

## 9. Exporter pour la fabrication

Page **Export**, trois boutons :

| Export | Contenu | Format |
|---|---|---|
| **Gerber** | Couches cuivre, masques, sérigraphie, contour (RS-274X) | ZIP |
| **BOM** | Nomenclature groupée (`Ref;Qty;Value;Footprint`) | CSV |
| **STEP** | Plaque + composants 3D (AP214 simplifié) | .step |

```bash
curl -s -o gerber.zip http://localhost:8080/api/v1/projects/<ID>/export/gerber
curl -s -o bom.csv    http://localhost:8080/api/v1/projects/<ID>/export/bom
curl -s -o carte.step http://localhost:8080/api/v1/projects/<ID>/export/step
```

Le ZIP Gerber contient notamment `*-F_Cu.gbr`, `*-B_Cu.gbr`, `*-F_Mask.gbr`
et `*-Edge_Cuts.gbr`, directement importables dans un visualiseur Gerber ou
envoyables à un fabricant.

## 10. Dépannage

| Symptôme | Cause probable | Solution |
|---|---|---|
| `bind: address already in use` sur le port 3000/8080/50051 | Un autre service occupe le port | Arrêtez le processus (`lsof -i :8080`) ou changez `YAHRIACAD_HTTP_PORT` / le port du moteur via `YAHRIACAD_AI_PORT` |
| `502` + code `ai_unreachable` sur /place, /route, /optimize | Moteur IA non démarré ou mauvaise adresse | Vérifiez `make run-ai` et `YAHRIACAD_AI_ADDR` (défaut `localhost:50051`) ; le backend démarre quand même, seul le port 50051 est requis |
| `"database":"memory"` alors que PostgreSQL est lancé | `YAHRIACAD_DB_URL` vide ou mal formée | Renseignez `postgres://yahriacad:yahriacad@localhost:5432/yahriacad?sslmode=disable` ou utilisez docker-compose |
| `grpcio-tools manquant` pendant `make proto` | Outil de génération absent | `pip install grpcio-tools` puis relancez `make proto` |
| Le routage RL n'est jamais utilisé | Aucun checkpoint `.pt` | `make models` (torch requis) — sinon le repli A* est utilisé automatiquement, sans erreur |
| Frontend affiche la bannière « mode démo » | API backend injoignable au chargement | Vérifiez le port 8080 et `NEXT_PUBLIC_API_URL` (défaut `http://localhost:8080/api/v1`) |

## Aller plus loin

- [Guide utilisateur](./guide-utilisateur.md) — pages, concepts, interactions.
- [Architecture](../architecture/architecture.md) — vue technique, ADR.
- [Contrats d'interface](../architecture/contracts.md) — API REST, WebSocket, gRPC.
