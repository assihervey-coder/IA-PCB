# YahriaCad

![Licence](https://img.shields.io/badge/licence-AGPL--3.0-blue)
![Next.js](https://img.shields.io/badge/Next.js-16-black)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6)
![Prisma](https://img.shields.io/badge/Prisma-SQLite-2D3748)
![Socket.io](https://img.shields.io/badge/socket.io-4.8-white)
![Three.js](https://img.shields.io/badge/Three.js-0.185-049EF4)

**YahriaCad** est une application web complète de CAO électronique (EDA) : éditeur de
schémas électriques, éditeur de PCB 2D, **placement et routage assistés par IA**, vérification
**DRC/ERC**, visualisation **3D temps réel** et exports industriels (**Gerber RS-274X**, Excellon,
BOM CSV, STEP, STL, netlist KiCad).

L'objectif : permettre à un électronicien de passer d'une idée (schéma) à un dossier de
fabrication (Gerbers) entièrement dans le navigateur, avec un assistant IA intégré qui place
les composants et route les pistes en quelques secondes.

---

## ✨ Fonctionnalités

| Domaine | Détail |
|---|---|
| 📐 Éditeur de schéma | Placement de composants, câblage clic-borne-à-borne, renommage des nets, bibliothèque de symboles |
| 🔲 Éditeur PCB (canvas 2D) | Multi-couches (F.Cu / B.Cu / mask / silk / edge), ratsnest live, grille 0.635 mm (25 mil), pistes, vias, pads |
| 🤖 Placement IA | Recuit simulé (*simulated annealing*) minimisant HPWL + pénalités de chevauchement |
| 🛣️ Routage IA | A\* maze-routing multi-couches 2 couches (F.Cu/B.Cu), coût de via, **rip-up & reroute**, optimiseur de réduction de vias |
| 🧊 Visualisation 3D | Rendu Three.js de la carte (plaque, pistes, composants) |
| ✅ Vérifications | DRC (largeur de piste, isolation, vias, distance au bord) et ERC (courts-circuits, nets flottants, alim) |
| 📤 Exports | Gerber RS-274X + Excellon (.drl) en ZIP, BOM CSV, STEP AP214 simplifié, STL, JSON natif, netlist KiCad |
| 📥 Import | Netlist KiCad (s-expression), netlist JSON |
| ⚡ Temps réel | socket.io : jobs IA (`ai:place`, `ai:route`, `ai:optimize`) avec logs et progression live (`ai:progress`) |

---

## 🗂️ Structure du dépôt

```text
YahriaCad/
├── src/
│   ├── app/                        # Next.js App Router
│   │   ├── page.tsx                # SPA mono-route (éditeur complet)
│   │   └── api/                    # Route handlers REST (monolithe modulaire DDD)
│   │       ├── projects/           # CRUD, drc, erc, import, export
│   │       ├── footprints/         # bibliothèque d'empreintes
│   │       └── health/             # état des services
│   ├── components/                 # UI (shadcn/ui + composants yahriacad)
│   └── lib/
│       ├── db.ts                   # client Prisma
│       └── yahriacad/                 # ⭐ cœur métier
│           ├── domain/             # entités : project, schematic, layout, constraints
│           ├── application/        # cas d'usage : import, validate, place, route, optimize, export, drc, erc
│           ├── infrastructure/     # adaptateurs (persistance, sérialisation, exporters)
│           ├── ai-engine/          # moteur d'inférence TS (A*, recuit simulé, rip-up & reroute)
│           ├── pkg/                # utilitaires : geometry, logger, utils
│           ├── shared/             # types.ts partagés client/serveur
│           └── library/            # bibliothèques : empreintes, symboles
├── mini-services/
│   └── ai-engine/                  # service socket.io Bun autonome (port 3010)
├── ai-engine/                      # pipeline R&D Python (RL : PPO + GNN) — NON requis
│   ├── src/                        # environment, agents, models
│   ├── training/                   # configs d'entraînement (router, placer, optimizer)
│   ├── evaluation/                 # métriques + benchmark CLI
│   └── data/                       # raw / processed / augmentations (gitignorés)
├── docs/
│   ├── architecture/               # architecture.md + ADR
│   ├── api/                        # openapi.yaml
│   └── guides/                     # guides utilisateur et démarrage rapide
├── docker/                         # Dockerfile, Dockerfile.frontend, docker-compose.yml
├── configs/                        # production/ et development/ (nginx, appsettings)
├── scripts/                        # setup-dev.sh, build-all.sh, generate-models.sh
├── tests/                          # tests bun + fixtures (netlist KiCad démo)
├── prisma/                         # schema.prisma
├── db/                             # custom.db (SQLite)
└── Caddyfile                       # gateway (paramètre ?XTransformPort=3010)
```

> **Mapping avec l'arborescence cible d'origine (Go + Python)** — le projet avait été
> spécifié à l'origine avec un backend Go (`backend/internal/{domain,application,infrastructure,pkg}`),
> un microservice IA gRPC en Python et un frontend séparé. L'implémentation réelle conserve
> **exactement le même découpage logique**, transposé en TypeScript dans le monorepo Next.js :
> `backend/internal/domain` → `src/lib/yahriacad/domain/`, `backend/internal/application` →
> `src/lib/yahriacad/application/`, `backend/internal/pkg` → `src/lib/yahriacad/pkg/`,
> `ai-engine` (gRPC) → `mini-services/ai-engine` (socket.io, port 3010), `ai-engine`
> (RL Python) → `ai-engine/` à la racine. Ce choix simplifie radicalement le déploiement :
> un seul artefact web temps réel, un langage de bout en bout, des types partagés
> client/serveur (`src/lib/yahriacad/shared/types.ts`). Voir `docs/architecture/adr-001-monorepo-nextjs.md`.

---

## 🚀 Démarrage rapide

Prérequis : [Bun](https://bun.sh) ≥ 1.1 (ou Node.js ≥ 20 avec npm), aucun GPU requis.

```bash
# 1. Dépendances
bun install

# 2. Base de données (SQLite)
bun run db:push

# 3. Lancer l'application (http://localhost:3000)
bun run dev
```

### Service IA (socket.io, port 3010)

Dans un **second terminal** :

```bash
cd mini-services/ai-engine
bun install
bun run dev
```

Le client se connecte via la gateway : `io("/?XTransformPort=3010", { path: "/" })`.

Ou tout-en-un : `make dev-all` (voir note dans le Makefile) / `docker compose -f docker/docker-compose.yml up --build`.

---

## ⚙️ Variables d'environnement

| Variable | Obligatoire | Valeur par défaut | Description |
|---|---|---|---|
| `DATABASE_URL` | ✅ | `file:../db/custom.db` | Chemin de la base SQLite (Prisma) |
| `PORT` | ❌ | `3000` | Port du serveur Next.js |
| `NEXT_PUBLIC_AI_PORT` | ❌ | `3010` | Port du mini-service IA (via gateway) |
| `NODE_ENV` | ❌ | `development` | Environnement d'exécution |

Exemple de `.env` :

```env
DATABASE_URL="file:../db/custom.db"
```

---

## 🛠️ Makefile

| Cible | Rôle |
|---|---|
| `make help` | Affiche l'aide |
| `make install` | Installe les dépendances (racine + service IA) |
| `make db-push` | Applique le schéma Prisma à SQLite |
| `make dev` | Lance Next.js en développement (port 3000) |
| `make ai-service` | Lance le mini-service IA (port 3010) |
| `make dev-all` | Note + instructions pour lancer les deux |
| `make lint` | ESLint |
| `make test` | Tests (`bun test tests/`) |
| `make docker` | `docker compose -f docker/docker-compose.yml up --build` |
| `make clean` | Nettoie les artefacts de build |

---

## 🐳 Docker

```bash
docker compose -f docker/docker-compose.yml up --build
# app       → http://localhost:3000  (Next.js + Prisma/SQLite, volume ./db)
# ai-engine → http://localhost:3010  (socket.io Bun, volume ./mini-services/ai-engine)
```

Un reverse proxy nginx de production est fourni dans `configs/production/nginx.conf`
(proxy `/` → app:3000, `/socket.io/` → ai-engine:3010 avec upgrade WebSocket, gzip).

---

## 🗺️ Roadmap

- [x] Éditeur de schéma + PCB 2D, placement IA (recuit simulé), routage IA (A\* 2 couches + rip-up & reroute)
- [x] DRC/ERC, exports Gerber ZIP / Excellon / BOM CSV / STEP / STL / netlist KiCad
- [x] Temps réel socket.io avec logs IA live, visualisation 3D Three.js
- [ ] Routage multi-couches > 2 couches + plans de masse (copper pour)
- [ ] Autoroutage interactif (sélection de nets) et contraintes par net (classe de nets)
- [ ] Intégration optionnelle des modèles RL PyTorch entraînés (voir `ai-engine/README.md`)
- [ ] Collaboratif multi-utilisateurs (CRDT) et historique undo/redo persistant

---

## 📄 Licence

Ce projet est distribué sous licence **GNU Affero General Public License v3.0 (AGPL-3.0)** —
voir [LICENSE](./LICENSE). Toute instance exposée publiquement doit offrir le code source à
ses utilisateurs.
