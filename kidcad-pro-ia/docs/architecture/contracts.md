# Contrats d'interface — KidCAD-Pro-IA (v1.0)

> **SOURCE DE VÉRITÉ inter-modules.** Tout agent/collaborateur doit lire ce document AVANT de coder.
> Il est interdit de modifier ce fichier, `shared/types/pcb.proto`, `shared/types/pcb.d.ts`
> ou la couche `backend/internal/domain/` sans validation de l'orchestrateur.
> Unités : **toutes les coordonnées sont en millimètres (mm)**, origine en coin haut-gauche,
> indice de couche 0 = F.Cu, `layer_count - 1` = B.Cu.

---

## 1. Architecture et import paths Go

- Module Go : `github.com/kidcad/kidcad-pro-ia` (go.mod à la racine du monorepo).
- Aliases d'import recommandés :
  - `domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"`
  - `domainschematic "…/domain/schematic"`, `domainlayout "…/domain/layout"`, `domainconstraints "…/domain/constraints"`
  - Packages application (nom de package **différent du dossier**, pour éviter les collisions) :
    - `backend/internal/application/schematic` → `package schematicapp`
    - `backend/internal/application/layout` → `package layoutapp`
    - `backend/internal/application/export` → `package exportapp`
    - `backend/internal/application/verification` → `package verificationapp`
  - Infrastructure : `package memory`, `package sql` (dossier `sql`), `package reader`, `package writer`,
    `package rest`, `package websocket` (dossier `websocket`), `package ai` (dossier `ai`), `package config`.
- Dépendances autorisées : `infrastructure → application → domain → pkg`.
  Interdiction : `domain → application/infrastructure`.

## 2. API REST du backend (base `/api/v1`)

| Méthode | Chemin | Corps / Query | Réponse |
|---|---|---|---|
| GET | `/healthz` | — | `{"status":"ok","version":"…","ai_engine":"ok\|unreachable","database":"memory\|postgres"}` |
| GET | `/api/v1/projects` | `?limit=20&offset=0` | `{"projects":[Project],"total":n}` |
| POST | `/api/v1/projects` | `ProjectCreate` | `201 Project` |
| GET | `/api/v1/projects/{id}` | — | `Project` |
| PUT | `/api/v1/projects/{id}` | `{name?, description?}` | `Project` |
| DELETE | `/api/v1/projects/{id}` | — | `204` |
| POST | `/api/v1/projects/{id}/import` | multipart, champ `file` | `ImportResult` |
| GET | `/api/v1/projects/{id}/layout` | — | `LayoutData` |
| PUT | `/api/v1/projects/{id}/layout` | `LayoutData` | `LayoutData` |
| POST | `/api/v1/projects/{id}/place` | `{"strategy":"rl"\|"heuristic"}` | `LayoutData` (synchrone) |
| POST | `/api/v1/projects/{id}/route` | `{"strategy":"rl"\|"astar","nets":[]}` | `202 JobStarted` |
| POST | `/api/v1/projects/{id}/optimize` | `{}` | `202 JobStarted` |
| GET | `/api/v1/projects/{id}/jobs/{jobID}` | — | `JobStatus` |
| POST | `/api/v1/projects/{id}/drc` | — | `DRCResult` |
| POST | `/api/v1/projects/{id}/erc` | — | `ERCResult` |
| GET | `/api/v1/projects/{id}/export/gerber` | — | `application/zip` |
| GET | `/api/v1/projects/{id}/export/bom` | — | `text/csv` |
| GET | `/api/v1/projects/{id}/export/step` | — | `application/step` |
| WS | `/ws/v1/progress` | `?project_id=…` | voir §3 |

Erreurs JSON : `{"error":{"code":"not_found|invalid|conflict|ai_unreachable|internal","message":"…"}}`.
Types Go des DTO REST (mappés 1:1 sur `shared/types/pcb.d.ts`, tags JSON snake_case) :
`Project`, `ProjectCreate`, `LayoutData`, `DRCResult`, `ERCViolation`, `JobStarted`, `JobStatus`,
`ImportResult`, `ExportFile`. Ils vivent dans les fichiers handler correspondants (`rest/dto.go` autorisé).

### JobStatus (JSON)
```json
{"job_id":"…","project_id":"…","kind":"route","state":"pending|running|done|failed|cancelled",
 "percent":42.0,"current_net":"GND","message":"…","error":"","nets_done":3,"nets_total":26,
 "started_at":"RFC3339","finished_at":null}
```

## 3. WebSocket `/ws/v1/progress`

- Client → serveur : `{"type":"subscribe","project_id":"…"}` ou `{"type":"ping"}`.
- Serveur → client :
  - `{"type":"progress","job_id","project_id","stage","current_net","percent","message","done","error"}`
  - `{"type":"pong"}`.
- Filtrage : par `project_id` (query OU message subscribe). Ping applicatif toutes les 30 s.

## 4. Ports de la couche application (signatures Go EXACTES)

### `application/layout/place.go` + `route.go` (package `layoutapp`)

```go
// Port vers le moteur IA (implémenté par infrastructure/ai/grpc_client.go)
type AIService interface {
    Health(ctx context.Context) error
    PlanPlacement(ctx context.Context, b *layout.Board, comps []schematic.Component,
        strategy string) ([]layout.PlacedComponent, error)
    RouteBoard(ctx context.Context, b *layout.Board, nets []schematic.Net,
        cs *constraints.ConstraintSet, strategy string,
        onProgress func(RouteProgress)) (*RouteOutcome, error)
    OptimizeRoutes(ctx context.Context, b *layout.Board, outcome *RouteOutcome,
        cs *constraints.ConstraintSet,
        onProgress func(RouteProgress)) (*RouteOutcome, error)
}

type RouteProgress struct {
    JobID      string
    Stage      string  // "route" | "optimize"
    CurrentNet string
    Message    string
    Percent    float64 // 0..100
    Done       bool
    Err        string
}

type NetRoute struct {
    Net       string
    Tracks    []layout.Track
    Vias      []layout.Via
    LengthMM  float64
    Completed bool
}

type RouteOutcome struct {
    Strategy   string
    Nets       []NetRoute
    DurationMS int64
}

// Port de publication temps réel (implémenté par api/websocket.Hub)
type ProgressPublisher interface {
    Publish(jobID, projectID string, p RouteProgress)
}

// Registre de jobs partagé (route + optimize)
func NewJobRegistry() *JobRegistry
type JobRegistry struct{ /* sync.Map */ }
func (r *JobRegistry) Create(projectID, kind string, netsTotal int) *JobHandle
func (r *JobRegistry) Get(jobID string) (*JobHandle, bool)
func (r *JobRegistry) List() []JobInfo
func (h *JobHandle) Info() JobInfo
func (h *JobHandle) SetRunning()
func (h *JobHandle) SetProgress(percent float64, currentNet, message string)
func (h *JobHandle) IncNetsDone(n int)
func (h *JobHandle) SetState(state string) // "done" | "failed" | "cancelled"

type PlaceService struct{ /* projects, ai, log */ }
func NewPlaceService(projects project.Repository, ai AIService, log *slog.Logger) *PlaceService
func (s *PlaceService) Place(ctx context.Context, projectID, strategy string) (*layout.Board, error)

type RouteService struct{ /* projects, ai, registry, publisher, log */ }
func NewRouteService(projects project.Repository, ai AIService, reg *JobRegistry,
    pub ProgressPublisher, log *slog.Logger) *RouteService
func (s *RouteService) Start(ctx context.Context, projectID, strategy string,
    netFilter []string) (jobID string, err error) // asynchrone (goroutine)
func (s *RouteService) JobStatus(jobID string) (JobInfo, bool)
func (s *RouteService) ListJobs() []JobInfo
```

`OptimizeService` (dans `optimize.go`) : mêmes dépendances que `RouteService`,
`func (s *OptimizeService) Start(ctx, projectID string) (string, error)`, `JobStatus`, `ListJobs`.
Comportement attendu du job : charger le projet, copier le board, appeler le port `AIService`,
publier chaque progression via `ProgressPublisher`, mettre à jour le board
(`ReplaceRoutesForNet`), persister via `Repository.Update`, état final `done`/`failed`.

### `application/schematic/import.go` + `validate.go` (package `schematicapp`)

```go
type ImportResult struct {
    Format      string                        // "kicad", "eagle", "interchange", "kicad-netlist", "protel-netlist"
    Schematic   *schematic.Schematic
    Board       *layout.Board
    Constraints *constraints.ConstraintSet
    Warnings    []string
}

type ReaderRegistry interface { Read(path string) (ImportResult, error) }

type ImportService struct{ /* projects, registry, log */ }
func NewImportService(projects project.Repository, registry ReaderRegistry, log *slog.Logger) *ImportService
func (s *ImportService) ImportFromFile(ctx context.Context, projectID, path string) (*ImportResult, error)

type ValidationService struct{ /* projects, log */ }
func NewValidationService(projects project.Repository, log *slog.Logger) *ValidationService
func (s *ValidationService) ValidateSchematic(ctx context.Context, projectID string) ([]schematic.Issue, error)
```

### `application/export/` (package `exportapp`)

```go
type GerberWriter interface { Write(b *layout.Board, projectName, outDir string) ([]string, error) }
type GerberService struct{ /* … */ }
func NewGerberService(projects project.Repository, w GerberWriter, dataDir string, log *slog.Logger) *GerberService
func (s *GerberService) ExportToDir(ctx, projectID, outDir string) ([]string, error)
func (s *GerberService) ExportToZip(ctx, projectID string) (zipPath string, files []string, err error)

type BOMLine struct { Refs []string; Qty int; Value, Footprint string }
type BOMService struct{ /* … */ }
func NewBOMService(projects project.Repository, log *slog.Logger) *BOMService
func (s *BOMService) ExportCSV(ctx, projectID string) ([]byte, error) // en-tête: Ref;Qty;Value;Footprint

type STEPWriter interface { Write(b *layout.Board, projectName, outDir string) (string, error) }
type STEPService struct{ /* … */ }
func NewSTEPService(projects project.Repository, w STEPWriter, dataDir string, log *slog.Logger) *STEPService
func (s *STEPService) Export(ctx, projectID string) (string, error)
```

### `application/verification/` (package `verificationapp`)

```go
type DRCViolation struct { Code, Severity, Message string; X, Y float64; Layer int; Net string }
type DRCResult struct { Passed bool; CheckedRules int; Violations []DRCViolation; DurationMS int64 }
type DRCChecker struct{ /* projects */ }
func NewDRCChecker(projects project.Repository) *DRCChecker
func (c *DRCChecker) Run(ctx context.Context, projectID string) (*DRCResult, error)

type ERCViolation struct { Code, Severity, Message, ComponentRef string }
type ERCResult struct { Passed bool; Violations []ERCViolation }
type ERCChecker struct{ /* projects */ }
func NewERCChecker(projects project.Repository) *ERCChecker
func (c *ERCChecker) Run(ctx context.Context, projectID string) (*ERCResult, error)
```

### DRC — algorithme attendu
Hachage spatial (cellule ≈ 2 mm). Vérifications : clearance piste↔piste (couches identiques,
nets différents), piste↔pad, piste↔via, via↔via, piste↔bord (rule `edge_clearance`),
largeur de piste (`min_track_width`), diamètre via / drill / anneau annulaire
(`min_via_diameter`, `min_drill`, `min_annular_ring`). Codes de violation :
`DRC_CLEARANCE`, `DRC_TRACK_WIDTH`, `DRC_VIA_DIAMETER`, `DRC_DRILL`, `DRC_ANNULAR`,
`DRC_EDGE_CLEARANCE`. Gravité issue des règles (`error` par défaut).

### ERC — codes
`ERC_DUPLICATE_REF`, `ERC_SINGLE_PIN_NET`, `ERC_EMPTY_NET_NAME`, `ERC_MISSING_FOOTPRINT`,
`ERC_UNCONNECTED_COMPONENT` (composant sans aucune connexion).

## 5. Contrat des adaptateurs infrastructure

| Fichier | API publique |
|---|---|
| `persistence/memory/project_repo.go` | `func NewProjectRepository() project.Repository` |
| `persistence/sql/project_repo.go` | `func NewProjectRepository(dsn string) (project.Repository, error)` ; stockage `schematic/layout/constraints` en JSONB via `pkg/pcb-format` |
| `persistence/sql/migrations/001_init.sql` | table `projects(id uuid pk, name, description, layer_count, status, created_at, updated_at, schematic jsonb, layout jsonb, constraints jsonb)` + index |
| `persistence/sql/migrations/002_indexes.sql` | index `status`, `created_at` |
| `fileio/reader/reader.go` | `func NewRegistry(log *slog.Logger) *reader.Registry` ; `func (r *Registry) Read(path string) (schematicapp.ImportResult, error)` |
| `fileio/reader/{kicad,eagle,altium,cadence,generic}.go` | lecteurs ; KiCad **s-expr complet** (subset §7), Eagle **XML** (.brd/.sch), générique = `.kidcad.json` + netlist KiCad `.net` + netlist Protel/Altium texte ; Altium (.SchDoc/.PcbDoc OLE2) et Cadence (.brd binaire) : **détection** + erreur typée claire (roadmap), pas de crash |
| `fileio/writer/gerber_writer.go` | `func NewGerberWriter() *writer.GerberWriter` — RS-274X : `<name>-F_Cu.gbr`, `-B_Cu.gbr`, `-In<N>_Cu.gbr`, `-F_Mask.gbr`, `-B_Mask.gbr`, `-F_Silkscreen.gbr`, `-B_Silkscreen.gbr`, `-Edge_Cuts.gbr` ; `%FSLAX46Y46*%`, `%MOMM*%` |
| `fileio/writer/step_writer.go` | `func NewStepWriter() *writer.StepWriter` — STEP AP214 simplifié (plaque + composants en boîtes, BREP correct) |
| `ai/grpc_client.go` | `func NewClient(addr string, timeout time.Duration, log *slog.Logger) (*ai.Client, error)` implémente `layoutapp.AIService` ; `func (c *Client) Close() error` |
| `api/rest/router.go` | `func NewRouter(d Deps) http.Handler` (voir struct `Deps` ci-dessous) |
| `api/websocket/progress.go` | `func NewHub(log *slog.Logger) *websocket.Hub` ; `func (h *Hub) Publish(jobID, projectID string, p layoutapp.RouteProgress)` ; `func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request)` |
| `config/config.go` | `type Config struct { HTTPPort, DBURL, AIAddr, LogLevel, DataDir, AppEnv, ConfigPath string; AllowedOrigins []string }` ; `func Load() Config` |

```go
// rest/router.go
type Deps struct {
    Projects   project.Repository
    Import     *schematicapp.ImportService
    Validation *schematicapp.ValidationService
    Place      *layoutapp.PlaceService
    Route      *layoutapp.RouteService
    Optimize   *layoutapp.OptimizeService
    DRC        *verificationapp.DRCChecker
    ERC        *verificationapp.ERCChecker
    Gerber     *exportapp.GerberService
    BOM        *exportapp.BOMService
    STEP       *exportapp.STEPService
    Hub        *websocket.Hub
    Logger     *slog.Logger
    Version    string
}
```

### Variables d'environnement (config.go)
`KIDCAD_HTTP_PORT` (8080), `KIDCAD_DB_URL` ("" → adaptateur mémoire),
`KIDCAD_AI_ADDR` (localhost:50051), `KIDCAD_LOG_LEVEL` (info), `KIDCAD_DATA_DIR` (./data),
`KIDCAD_ALLOWED_ORIGINS` (*), `KIDCAD_APP_ENV` (development), `KIDCAD_CONFIG` (fichier JSON optionnel
fusionné sous les env). Le serveur doit démarrer **même si le moteur IA est injoignable**
(warning + réponse `ai_unreachable` sur les routes IA) et **même sans PostgreSQL** (adaptateur mémoire).

## 6. Contrat du moteur IA Python (`ai-engine/`)

- Stub gRPC importé depuis `shared/gen/python` (`pcb_pb2`, `pcb_pb2_grpc`) — `sys.path` ajusté,
  **jamais** de stubs dupliqués.
- Aucun `import torch` au niveau module (imports paresseux dans les fonctions/classes concernées) :
  le service fonctionne **sans torch** grâce au repli A*.

| Fichier | Contenu |
|---|---|
| `cmd/ai-server/main.py` | point d'entrée : `sys.path` (racine ai-engine + shared/gen/python), charge `training/router/config.yaml`, lance `grpc.server(ThreadPoolExecutor(max_workers=8))`, port `KIDCAD_AI_PORT` (défaut 50051), logs structurés, arrêt propre SIGTERM/SIGINT |
| `src/service.py` | `class AIRouterServicer(pcb_pb2_grpc.AIRouterServiceServicer)` : `GetHealth`, `PlanPlacement` (recuit simulé ou modèle RL si dispo), `RouteBoard` (1 `ProgressEvent` par net terminé, final `done=True` ; routage RL si modèle chargé sinon **A\***), `OptimizeRoutes` (rip-up & reroute des pires nets) |
| `src/environment/pcb_env.py` | `class PCBRouteEnv(board, nets, config=None)` — gym-like : `reset(net_index) -> obs`, `step(action) -> (obs, reward, terminated, truncated, info)`, `astar_route(net_index) -> dict` (segments/vias/length_mm/completed), `observation_shape` |
| `src/environment/action_space.py` | `class ActionSpace` : `n = 12` (4 directions × {stay, via-up, via-down}), `decode(a) -> (dx, dy, dlayer)`, `sample(rng)`, `describe()` |
| `src/environment/reward.py` | `@dataclass RewardConfig` + `class RewardShaper` (progression, pénalité via/collision/longueur, bonus succès) |
| `src/agents/base_agent.py` | `class BaseAgent(abc.ABC)` : `select_action`, `update`, `save`, `load`, `name` |
| `src/agents/ppo_agent.py` | `PPOConfig`, `PPOAgent(BaseAgent)` (actor-critic CNN, GAE, clipping), `PPOTrainer` |
| `src/agents/a3c_agent.py` | `A3CAgent(BaseAgent)`, `A3CWorker`, `A3CTrainer` (threads + réseau global, n-step) |
| `src/models/graph_net.py` | GNN message passing **pur torch** (sans torch_geometric) + `build_graph(nets)` |
| `src/models/transformer.py` | `BoardViT` (patch embedding conv + TransformerEncoder, heads) |
| `src/{__init__,environment/__init__,agents/__init__,models/__init__}.py` | packages |
| `evaluation/metrics.py` | `route_length_mm`, `via_count`, `completion_rate`, `drc_penalty`, `summarize` |
| `evaluation/bench.py` | CLI `--grids N --agent random|greedy` (+ `ppo` si checkpoint), tableau ASCII + JSON |
| `training/router/config.yaml` | hyperparamètres PPO (référence pour main.py/service.py) |
| `training/router/tokenizer/` | `tokenizer.py` (`NetTokenizer`) + `vocab.json` |
| `training/{placer,optimizer}/` | `config.yaml` + `README.md` |

Fichiers `.pt` : générés par `scripts/generate-models.sh` (torch requis), **jamais commités à la main**.

## 7. Subset KiCad supporté par `fileio/reader/kicad.go` (et fixture de test)

```lisp
(kicad_pcb (version 20221018) (generator demo)
  (general (thickness 1.6))
  (layers (0 "F.Cu" signal) (31 "B.Cu" signal))
  (net 0 "") (net 1 "GND") (net 2 "VCC")
  (footprint "Lib:Name" (layer "F.Cu") (at X Y ROT)
    (fp_text reference "R1" (at X Y ROT) (layer "F.SilkS"))
    (fp_text value "10k" (at X Y ROT) (layer "F.Fab"))
    (pad "1" smd rect (at X Y ROT) (size W H) (layers "F.Cu" "F.Mask") (net N "NAME"))
    (pad "" thru_hole circle (at X Y) (size W H) (drill D) (layers "*.Cu" "*.Mask") (net N "NAME")))
  (segment (start X Y) (end X Y) (width W) (layer "F.Cu") (net N))
  (via (at X Y) (size D) (drill H) (layers "F.Cu" "B.Cu") (net N))
  (gr_rect (start X Y) (end X Y) (layer "Edge.Cuts")))
```

Règles : `*.Cu`/`*.Mask` = pastille traversante (dupliquée sur couche 0 et dernière) ;
`(net N "NAME)` optionnel dans `pad` ; parser s-expression générique (tokenizer + arbre).
Fixture de test : `tests/fixtures/sample.kicad_pcb` (doit se parser sans erreur, ≥ 2 nets, ≥ 4 pads,
1 via, des segments, un `gr_rect` Edge.Cuts).

## 8. Contrat frontend (Next.js App Router)

- Stack : Next 14.2.x, React 18.3, TypeScript 5 strict, SCSS (`sass`), Zustand 4, Axios, Three.js.
- Routes : `/` (redirect → `/pages/project-manager`), `/pages/project-manager`,
  `/pages/schematic-editor`, `/pages/pcb-layout`, `/pages/pcb-layout/viewer`, `/pages/pcb-layout/router`,
  `/pages/export`.
- Structure fidèle à l'arborescence : `src/app/layout/{AppShell,Header,Sidebar}.tsx`,
  `src/app/components/{buttons,modals,forms}/`, `src/lib/api/{rest-client,ws-client}.ts`,
  `src/lib/store/{project-store,ui-store}.ts`, `src/lib/utils/{converters,geometry-utils}.ts`,
  `src/styles/{globals.scss,_variables.scss,_mixins.scss}`.
- `rest-client.ts` : `baseURL = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"` ;
  fonctions `listProjects, createProject, getProject, updateProject, deleteProject, importFile,
  getLayout, putLayout, startPlace, startRoute, startOptimize, getJob, runDRC, runERC,
  exportGerber(blob), exportBOM(blob), exportSTEP(blob)`.
- `ws-client.ts` : `class ProgressSocket(projectId, onEvent, onStatus)` — URL
  `NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080"` + `/ws/v1/progress?project_id=…`, reconnexion expo.
- **Mode démo obligatoire** : si l'API est injoignable, charger `public/demo/demo-project.json`
  et `public/demo/demo-board.json`, afficher la bannière `[data-testid="demo-banner"]`.
- `data-testid` requis : `projects-grid`, `project-card`, `create-project-btn`, `project-form-name`,
  `project-form-submit`, `demo-banner`, `schematic-canvas`, `pcb-canvas`, `route-start-btn`,
  `route-strategy`, `progress-bar`, `progress-log`, `drc-run-btn`, `erc-run-btn`,
  `export-gerber-btn`, `export-bom-btn`, `export-step-btn`, `import-input`, `viewer-canvas`.
- UI en français, thème sombre pro (variables SCSS : fond `#0B1220`, panneau `#111A2C`,
  accent `#22D3EE`, cuivre `#F59E0B`).
- Viewer 3D : `three` importé dynamiquement (`ssr: false`) dans un composant client.

## 9. Tests

- `tests/integration/backend_pipeline_test.go` (`package integration_test`) :
  1) création projet (mémoire) → 2) import `tests/fixtures/sample.kicad_pcb` via `reader.NewRegistry`
  → 3) ERC ≥ 0 violation attendue 0 erreur → 4) board avec 2 pistes trop proches → DRC ≥ 1 violation
  → 5) `GerberService.ExportToZip` → zip contenant `*-F_Cu.gbr` → 6) mock `layoutapp.AIService`
  (routes en L) → `RouteService.Start` → attente état `done` → pistes présentes dans le board.
- `tests/e2e/` : Playwright, `data-testid` du §8, serveur `npm --prefix ../../frontend run dev`.
- `tests/fixtures/` : `sample.kicad_pcb`, `sample.netlist` (netlist KiCad `.net`), `sample-project.json`
  (format interchange), `README.md`.

## 10. Conventions de code

- Go : `gofmt`, erreurs wrappées `%w`, contexte en premier argument, `slog` pour les logs,
  commentaires en anglais, pas de panic en dehors des défauts de programmation.
- Python : type hints, docstrings anglaises, `ruff` clean, imports paresseux de torch.
- TypeScript : strict, composants clients `"use client"`, exports nommés.
- Tous les textes utilisateur (README, docs, UI) en **français**.
- Aucun TODO/placeholder : code complet et fonctionnel.

## 11. Endpoints additifs (hors contrat figé §2) — v0.2

Tous les endpoints ci-dessous sont **additifs** : ils n'altèrent ni les DTOs
ni les routes du contrat initial. Ils suivent les mêmes conventions (JSON
snake_case, `ErrorResponse` uniforme, erreurs `not_found` / `invalid` /
`ai_unreachable`).

### Plans de masse (copper pours)

| Route | Rôle |
|---|---|
| `POST /api/v1/projects/{id}/pours` | génère/re-remplit les plans d'un net (corps `PourGenerateRequest` : `net`, `layers`, `clearance_mm`, `hatch_mm`, `is_ground`, `edge_margin_mm`, `stitch`, `stitch_grid_mm`) |
| `GET /api/v1/projects/{id}/pours` | liste des pours |
| `DELETE /api/v1/projects/{id}/pours?net=GND` | suppression par net (sans paramètre : tout) |
| `DELETE /api/v1/projects/{id}/pours/{pourID}` | suppression unitaire |

Le remplissage est calculé par échantillonnage (pas 0.25–0.5 mm, budget
120 000 points) en respectant l'isolement autour de chaque piste/pad/via
étranger de la couche. La couture (vias GND) relie les couches du plan par
quinconce, hors zones de clearance, cap 400 vias. Les pours voyagent dans le
format interchange (`layout.pours[]`), la persistance SQL et le `LayoutData`
REST ; un `PUT /layout` qui ne les mentionne pas les **conserve**.

### Classes de nets (autoroutage interactif)

| Route | Rôle |
|---|---|
| `GET /api/v1/projects/{id}/netclasses` | vue d'ensemble : nets + classe + valeurs effectives (`effective`), classes connues (`default`, `power`, `signal`, `high-speed`) et règles scopées |
| `PUT /api/v1/projects/{id}/netclasses/{net}` | `{"net_class":"power"}` reclasse un net (schéma requis) |
| `PUT /api/v1/projects/{id}/netclasses/{class}/rules` | upsert des règles de classe (`min_track_width_mm`, `min_clearance_mm`, `min_via_diameter_mm`, `min_drill_mm`) |

La sélection de nets à router reste `POST .../route` avec `{"nets":["USB_D+"]}` ;
les valeurs `effective` sont exactement celles transmises au moteur IA
(`NetSpec.min_track_width_mm`, `clearance_mm`).

### Collaboration CRDT + undo/redo persistant

| Route | Rôle |
|---|---|
| `POST /api/v1/projects/{id}/collab/ops` | applique un lot d'ops `{actor, ops:[{client_id?, lamport?, kind, target?, payload}]}` |
| `GET /api/v1/projects/{id}/collab/state?since=N` | séquence, horloge vectorielle, rattrapage des ops > N |
| `POST /api/v1/projects/{id}/collab/undo` | `{"actor":"alice"}` — annule la dernière op annulable de l'acteur |
| `POST /api/v1/projects/{id}/collab/redo` | rétablit |

Vocabulaire d'ops : `component.move`, `component.rotate`, `track.add`,
`track.remove`, `via.add`, `via.remove`, `constraint.width`. Modèle : CRDT
op-based, registres LWW par (lamport, acteur) pour position/rotation/règles,
ensembles additifs dédupliqués pour le cuivre. Les ops appliquées sont
diffusées sur `/ws/v1/progress` (`type:"collab"`). Le journal JSONL sous
`$KIDCAD_DATA_DIR/collab/<project>.jsonl` rend l'historique undo/redo
**persistant** : au redémarrage il est rejoué en « bookkeeping seul » (la
carte du dépôt est déjà à jour) et les piles par acteur sont reconstruites.

### Démo « carte cauchemar »

| Route | Rôle |
|---|---|
| `POST /api/v1/demo/nightmare` | crée un projet semé de fautes réelles (pistes fines, vias sous-dimensionnés, piste collée au bord, nets non routés, aucune règle) et renvoie la liste des fautes avec la séquence de réparation |

Scénario de démonstration complet :
`POST /demo/nightmare` → `GET .../doctor` (score D) → `POST .../drc/autofix`
→ `POST .../route` → `GET .../doctor` (score remonté).

### Design Doctor ↔ moteur RL

- Le port `layoutapp.AIService` gagne `EngineInfo(ctx) (EngineInfo, error)`
  (sonde `GetHealth` : `status`, `version`, `device`, `model_loaded`).
- `/healthz` expose additivement `ai_device`, `ai_model_loaded`, `ai_version`.
- `GET .../doctor` expose l'axe transversal `ai` (`reachable`, `device`,
  `model_loaded`, `strategy` = `rl` si un modèle est chargé, sinon `astar`)
  et, quand des nets restent non routés, une `rehearsal` : le moteur route
  ces nets en **sandbox** (rien n'est persisté) et le rapport prédit
  « X/Y nets routables, Z mm de cuivre » avec une ordonnance chiffrée.

### Éditeur CRDT côté frontend (v0.3)

Le frontend embarque maintenant l'**éditeur collaboratif** qui consomme les
endpoints ci-dessus :

- `frontend/src/lib/collab/crdt-client.ts` — `CrdtEditor` : outbox
  persistante (localStorage, par projet), envoi par lots idempotents
  (`client_id`), rattrapage `GET .../collab/state?since=seq` à l'ouverture
  et à chaque reconnexion WS, déduplication par op id (une op renvoyée par
  le broadcast n'est jamais ré-appliquée).
- `frontend/src/lib/collab/apply-op.ts` — fusion pure d'une op distante dans
  le `LayoutData` local (les 7 kinds du vocabulaire, ensembles additifs
  dédupliqués pour le cuivre).
- `frontend/src/lib/collab/use-crdt.ts` — hook React (`send`, `undo`, `redo`,
  statut, ops en attente, flux d'activité) ; désactivé en mode démo.
- `frontend/src/app/components/collab/CollabBar.tsx` — barre d'état
  (synchronisé / N ops en attente / hors ligne) + activité distante.
- Sur la page `pcb-layout` : les mutations de composants partent en ops CRDT
  (`component.move`, `component.rotate`) quand la collaboration est active ;
  sinon repli historique : PUT /layout. Undo/redo (Ctrl+Z / Ctrl+Maj+Z)
  délèguent aux piles serveur persistantes quand la collab est active.
- `frontend/src/lib/api/ws-client.ts` — `CollabSocket` : canal WS type
  `"collab"` + signal de rattrapage après reconnexion.

### Oracle d'impédance différentielle (v0.3)

| Route | Rôle |
|---|---|
| `POST /api/v1/projects/{id}/impedance` | analyse des paires différentielles **par classe de nets** |

Corps optionnel : `{"targets": {"high-speed": 90}, "gap_mm": 0.2,
"tolerance_pct": 10}` (défauts : cible 100 Ω, 90 Ω si la classe contient
« usb », tolérance ±10 %). Réponse : par classe, chaque paire détectée avec
`z0/zodd/zeven/zdiff/zcom` (microstrip couplé, forme close type Bogatin),
écart mesuré cuivre à cuivre, longueur + skew intra-paire (mm et ps),
conformité à la bande, **largeur recommandée** à écart constant et **écart
recommandé** à largeur constante pour atteindre la cible.

Détection des paires : conventions de nommage `X+/X-`, `X_P/X_N`, `XP/XN`
(dans une même classe). Sans schéma importé, les classes sont déduites des
pistes routées (classe `default`). La correction de la formule microstrip
IPC-2141 (division par √(Er+1.41) et logarithme népérien) et de la vitesse
de propagation (0.2998 mm/ps) s'applique aussi à l'Oracle d'œil : une piste
0.25 mm sur 0.2 mm de prépreg affiche désormais ≈ 60 Ω (valeur réaliste) et
les délais/skews sont en picosecondes exactes.
