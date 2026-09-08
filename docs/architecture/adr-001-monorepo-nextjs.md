# ADR-001 — Monolithe modulaire Next.js au lieu d'un backend Go + frontend séparé

- **Statut :** Accepté
- **Date :** 2025
- **Décideurs :** équipe YahriaCad

## Contexte

L'architecture cible d'origine spécifiait :

- un backend **Go** (`backend/internal/{domain,application,infrastructure,pkg}`) exposant une API REST ;
- un **frontend séparé** (SPA) communiquant en REST + gRPC ;
- un **microservice IA** (gRPC/protobuf) pour le placement et le routage.

L'application est une CAO électronique interactive : éditeur de schéma, éditeur PCB
canvas 2D, vue 3D, jobs IA longs avec progression temps réel, exports de fichiers.
L'environnement de déploiement visé est un monorepo web simple à opérer (sandbox,
portail unique, gateway Caddy avec transformation de port).

## Décision

Nous remplaçons le duo backend Go + frontend séparé par un **monolithe modulaire
Next.js 16 (App Router, TypeScript)** :

- Les route handlers REST vivent sous `src/app/api` (Next.js API Routes) et délèguent
  au cœur métier organisé en **DDD** sous `src/lib/yahriacad/{domain,application,infrastructure,pkg,shared,library}`.
- Le mapping 1:1 avec l'arborescence Go est conservé (voir
  [architecture.md](./architecture.md#4-mapping-arborescence-dorigine--implémentation)).
- Le microservice IA gRPC est remplacé par un **service socket.io Bun autonome**
  (`mini-services/ai-engine/`, port 3010), atteint via la gateway Caddy avec le
  paramètre `?XTransformPort=3010` (voir [ADR-003](./adr-003-formats-echanges.md)).
- La persistance passe par **Prisma + SQLite** (`db/custom.db`).

## Conséquences

### Positives

- **Un seul langage** (TypeScript) de la base de données au canvas : zéro duplication de
  modèles, un contrat unique `src/lib/yahriacad/shared/types.ts` partagé client/serveur.
- **Build et déploiement simples** : un artefact Next.js (mode standalone) + un petit
  service Bun ; pas de registry protobuf ni de génération de stubs multi-langages.
- **Temps réel natif** : socket.io couvre la progression des jobs IA (événements
  `ai:progress`, `ai:result`) mieux qu'un simple REST, avec reconnexion automatique.
- **Le découpage DDD reste intact** : la logique métier est isolée de Next.js et pourrait
  être extraite vers un service séparé sans réécriture des domaines.
- Iteration rapide (hot reload sur le frontend **et** l'API), tests `bun test` unifiés.

### Négatives

- Le backend Next.js est mono-processus Node/Bun : pas de parallélisme Go (goroutines)
  pour les calculs lourds ; ceux-ci sont délégués au service IA dédié.
- Couplage framework : le code sous `src/app/api` dépend de Next.js (mitigé par le
  placement du métier dans `src/lib/yahriacad`, agnostique).
- SQLite n'est pas adapté à un déploiement multi-instances haute charge (suffisant pour
  une application mono-serveur ; PostgreSQL resterait possible via Prisma sans changer
  le métier).
- L'écosystème EDA en Go/Python (Kicad parseurs natifs, etc.) n'est pas directement
  exploitable côté serveur ; les formats sont réimplémentés en TS (import KiCad,
  Gerber RS-274X, Excellon, STEP simplifié).
