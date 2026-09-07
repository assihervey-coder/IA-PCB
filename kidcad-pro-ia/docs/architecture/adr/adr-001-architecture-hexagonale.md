# ADR-001 — Architecture hexagonale (DDD, ports & adaptateurs) pour le backend Go

| | |
|---|---|
| **Statut** | Accepté |
| **Date** | 2025 |
| **Décideurs** | Équipe KidCAD-Pro-IA |
| **Concerné** | `backend/` |

## Contexte

Le backend doit orchestrer des préoccupations hétérogènes : logique métier de
CAO (nets, pistes, règles de conception), persistance (mémoire pour le dev,
PostgreSQL en prod), lecture/écriture de formats d'échange (KiCad s-expr,
Eagle XML, Gerber), intégration d'un moteur IA externe et deux canaux
clients (REST + WebSocket).

Deux risques principaux guident la décision :

1. **Couplage** : si les cas d'usage appellent directement gRPC, SQL ou
   `net/http`, toute évolution (changement de SGBD, mock IA pour les tests,
   nouveau format) traverse toute la pile.
2. **Testabilité** : le pipeline import → placement → routage → DRC → export
   doit être testable sans réseau ni base (cf. tests d'intégration).

## Décision

Adopter une architecture **hexagonale** (ports & adaptateurs) combinée au
**Domain-Driven Design** :

- **Domaine** (`backend/internal/domain/`) — entités et invariants purs :
  `project` (agrégat racine + port `Repository`), `schematic`, `layout`,
  `constraints`. **Aucune dépendance** externe (stdlib uniquement) ; les
  invariants (via valide, net non vide, bornes de couches) vivent ici.
- **Application** (`backend/internal/application/`) — cas d'usage orchestre
  le domaine via des **ports** (interfaces Go) qu'elle définit :
  - `project.Repository` (persistance),
  - `layoutapp.AIService` (moteur IA : `Health`, `PlanPlacement`,
    `RouteBoard`, `OptimizeRoutes` avec callbacks de progression),
  - `layoutapp.ProgressPublisher` (temps réel),
  - `exportapp.GerberWriter`, `STEPWriter` (fabrication).
- **Infrastructure** (`backend/internal/infrastructure/`) — adaptateurs
  concrets : `persistence/memory` + `persistence/sql`, `fileio/reader`
  (KiCad/Eagle/natif), `fileio/writer` (Gerber RS-274X, STEP AP214),
  `ai` (client gRPC), `api/rest`, `api/websocket`, `config`.
- **Règle de dépendance** : `infrastructure → application → domain → pkg`.
  Interdiction inverse (`domain → application/infrastructure`), vérifiée en
  revue et par la compilation (le domaine n'importe rien d'interne).

Conventions Go associées : erreurs wrappées `%w`, `context.Context` en
premier argument, `log/slog` structuré, packages application renommés pour
éviter les collisions (`layoutapp`, `exportapp`, `schematicapp`,
`verificationapp`).

## Conséquences

**Positives**

- Cas d'usage testables avec des doubles en mémoire (repository mémoire,
  IA mockée, tempdir d'export) — cf. `tests/integration/backend_pipeline_test.go`.
- Le backend démarre sans PostgreSQL (adaptateur mémoire) ni moteur IA
  (port implémenté ou erreur `ai_unreachable` mappée en 502).
- Remplacement ponctuel d'un adaptateur (SQL → autre, gRPC → gRPC mock)
  sans toucher au métier.
- Frontières explicites : le domaine exprime le vocabulaire CAO, les
  adaptateurs traduisent les protocoles.

**Négatives / coûts**

- Plus de fichiers et d'indirection qu'un MVC linéaire ; DTO REST séparés
  des entités de domaine (mapping explicite).
- Discipline nécessaire : un développeur pressé pourrait importer un
  adaptateur depuis le domaine — la règle doit être surveillée en revue.
- Mappings répétitifs entre DTO REST (snake_case JSON) et objets de domaine.

## Alternatives écartées

- **Monolithe MVC simple** : plus rapide à démarrer, mais les tests
  d'intégration exigeraient PostgreSQL + gRPC réels et le domaine serait
  envahi par les détails techniques.
- **Clean Architecture à quatre anneaux** : équivalente en pratique ;
  l'hexagonal (3 couches + pkg) suffit et reste plus lisible pour une équipe
  réduite.
