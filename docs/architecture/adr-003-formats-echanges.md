# ADR-003 — gRPC/protobuf remplacé par socket.io + JSON typé partagé ; formats d'échange PCB

- **Statut :** Accepté
- **Date :** 2025
- **Décideurs :** équipe YahriaCad

## Contexte

L'architecture d'origine prévoyait un **contrat gRPC/protobuf** entre le frontend, le
backend et le service IA, plus des formats d'échange à définir pour les designs PCB
(placement, routage, exports de fabrication).

Contraintes réelles :

- le frontend est un navigateur : gRPC-web impose un proxy dédié et une toolchain de
  génération de stubs ;
- le service IA doit diffuser une **progression en streaming** (`stage`, `progress` 0-100,
  `message`) pendant les jobs de placement/routage — naturel en WebSocket, pénible en gRPC-web ;
- les types du domaine sont **déjà partagés** dans un monorepo TypeScript unique
  (voir [ADR-001](./adr-001-monorepo-nextjs.md)) ;
- les formats de fabrication industriels (Gerber, Excellon, STEP, KiCad) sont des formats
  texte/zip bien normalisés qu'il faut produire, pas remplacer.

## Décision

### 1. Transport temps réel : socket.io + JSON typé

Le contrat d'échange est porté par **socket.io 4.8** avec des charges **JSON typées** par
le module unique `src/lib/yahriacad/shared/types.ts` (importé par le client, l'API REST et le
service IA — une seule source de vérité, vérifiée par le compilateur TypeScript à chaque
build, là où protobuf aurait été vérifié par `protoc`).

Contrat événements (port 3010 via gateway Caddy, paramètre `?XTransformPort=3010`,
client : `io("/?XTransformPort=3010", { path: "/" })`) :

| Direction | Événement | Charge |
|---|---|---|
| client → serveur | `ai:place` | `{ design, options }` — `AiJobOptions` |
| client → serveur | `ai:route` | `{ design, options }` — `AiJobOptions` |
| client → serveur | `ai:optimize` | `{ design }` — réduction de vias |
| serveur → client | `ai:progress` | `{ stage, progress (0-100), message }` |
| serveur → client | `ai:result` | `{ design, stats }` |
| serveur → client | `ai:error` | `{ code, message }` |

### 2. Format de design natif : JSON

Le design complet (schematicJson, layoutJson, rulesJson) est sérialisé en **JSON natif**,
persisté tel quel par Prisma/SQLite, et disponible via
`GET /api/projects/{id}` et `GET /api/projects/{id}/export/json`.

### 3. Formats d'entrée (import)

| Format | Détail |
|---|---|
| Netlist **KiCad** | S-expression `(export (version "E") … (components …) (nets …))`, endpoint `POST /api/projects/{id}/import` avec `{format: "kicad", content}` |
| Netlist **JSON** | Format interne simple `{format: "json", content}` |

### 4. Formats de sortie (export) — `GET /api/projects/{id}/export/{format}`

| `format` | Contenu | Détail |
|---|---|---|
| `gerber` | **ZIP** | Gerber **RS-274X** : F.Cu, B.Cu, F.Mask, B.Mask, F.Silk, B.Silk, Edge.Cuts + fichier **Excellon** `.drl` (perçages) |
| `bom` | CSV | Nomenclature : référence, valeur, empreinte, quantité |
| `step` | STEP AP214 simplifié | Solides « boîtes » de la carte et des composants (pour insertion mécanique) |
| `stl` | STL | Maillage triangulé de la plaque (impression 3D / vérification) |
| `json` | JSON natif | Design complet YahriaCad |
| `netlist-kicad` | S-expression | Netlist KiCad ré-exportée depuis le schéma |

## Conséquences

### Positives

- Aucune toolchain protobuf ; le typage est vérifié par `tsc` à chaque build.
- Streaming de progression natif (WebSocket) avec reconnexion automatique.
- Gerber RS-274X / Excellon / STEP / KiCad restent **des standards de l'industrie**,
  exploitables par tout fabricant (JLCPCB, PCBWay, Aisler…).

### Négatives

- JSON est plus verbeux et moins compressé que protobuf (acceptable : designs de
  quelques Mo maximum, gzip côté proxy).
- Pas de contrat binaire multi-langages : si un jour un service Python doit consommer le
  design, il parse le JSON (ou utilise socket.io-python, voir `ai-engine/src/service.py`).
- socket.io introduit un protocole applicatif au-dessus de WebSocket à documenter
  (fait ici et dans `docs/api/openapi.yaml` pour la partie REST).
