/**
 * KidCAD-Pro-IA — types de l'API (frontend).
 *
 * Re-déclaration fidèle de `shared/types/pcb.d.ts` (sections 1 et 2) :
 * le frontend est une application npm séparée du monorepo, il ne peut pas
 * importer le fichier partagé directement. Toute évolution du contrat doit
 * être répercutée ici à l'identique (DTO JSON snake_case).
 */

// ==============================================================
// 1. Reflet de shared/types/pcb.proto
// ==============================================================

/** Point en millimètres, origine coin haut-gauche. */
export interface Point {
  x: number;
  y: number;
}

export interface BBox {
  min_x: number;
  min_y: number;
  max_x: number;
  max_y: number;
}

export interface PadRef {
  component_ref: string;
  pad_name: string;
  position: Point;
  layer: number;
  width_mm: number;
  height_mm: number;
  rotation_deg: number;
}

export interface TrackPoint {
  position: Point;
  layer: number;
}

export interface TrackSegment {
  a: TrackPoint;
  b: TrackPoint;
  width_mm: number;
}

export interface ViaProto {
  position: Point;
  from_layer: number;
  to_layer: number;
  diameter_mm: number;
  drill_mm: number;
}

export interface BoardSpec {
  width_mm: number;
  height_mm: number;
  layer_count: number;
  grid_resolution_mm: number;
  layer_names: string[];
}

export interface ComponentSpec {
  ref: string;
  footprint: string;
  position: Point;
  rotation_deg: number;
  fixed: boolean;
  bbox_mm: BBox;
  height_mm: number;
}

export interface NetSpec {
  name: string;
  net_class: string;
  pads: PadRef[];
  min_track_width_mm: number;
  clearance_mm: number;
}

export type AIStrategy = "rl" | "heuristic" | "astar";

export interface PlacementRequest {
  board: BoardSpec;
  components: ComponentSpec[];
  strategy: AIStrategy;
}

export interface PlacementResult {
  placed: ComponentSpec[];
  total_wirelength_mm: number;
  score: number;
  strategy: string;
}

export interface RouteRequest {
  board: BoardSpec;
  nets: NetSpec[];
  placed: ComponentSpec[];
  strategy: AIStrategy;
  net_filter: string[];
}

export interface RouteNetResult {
  net: string;
  segments: TrackSegment[];
  vias: ViaProto[];
  length_mm: number;
  completed: boolean;
  drc_violations: number;
}

export interface ProgressEvent {
  job_id: string;
  stage: "route" | "optimize";
  current_net: string;
  percent: number;
  message: string;
  partial: RouteNetResult | null;
  done: boolean;
  error: string;
}

export interface HealthResponse {
  status: "ok" | "degraded";
  version: string;
  device: string;
  model_loaded: boolean;
}

// ==============================================================
// 2. DTO de l'API REST du backend (cf. docs/architecture/contracts.md §2)
// ==============================================================

export type ProjectStatus = "active" | "archived";

export interface Project {
  id: string;
  name: string;
  description: string;
  layer_count: number;
  status: ProjectStatus;
  created_at: string; // RFC 3339
  updated_at: string; // RFC 3339
}

export interface ProjectCreate {
  name: string;
  description?: string;
  layer_count?: number; // défaut 2
}

export interface ProjectPatch {
  name?: string;
  description?: string;
}

/** Layout complet d'un projet (sérialisation JSON du board). */
export interface LayoutData {
  board: {
    width_mm: number;
    height_mm: number;
    layer_count: number;
    layer_names: string[];
  };
  components: LayoutComponent[];
  nets: LayoutNet[];
  tracks: LayoutTrack[];
  vias: LayoutVia[];
}

export interface LayoutComponent {
  ref: string;
  footprint: string;
  x: number;
  y: number;
  rotation: number;
  fixed: boolean;
}

export interface LayoutNet {
  name: string;
  net_class: string;
  pad_count: number;
}

export interface LayoutTrack {
  net: string;
  layer: number;
  width: number;
  points: Array<{ x: number; y: number }>;
}

export interface LayoutVia {
  net: string;
  x: number;
  y: number;
  from_layer: number;
  to_layer: number;
  diameter: number;
  drill: number;
}

export interface DRCViolation {
  code: string;
  severity: "error" | "warning";
  message: string;
  x: number;
  y: number;
  layer: number;
  net: string;
}

export interface DRCResult {
  passed: boolean;
  checked_rules: number;
  violations: DRCViolation[];
  duration_ms: number;
}

export interface ERCViolation {
  code: string;
  severity: "error" | "warning";
  message: string;
  component_ref: string;
}

export interface ERCResult {
  passed: boolean;
  violations: ERCViolation[];
}

export interface RouteJobStart {
  strategy?: AIStrategy;
  nets?: string[];
}

export interface JobStarted {
  job_id: string;
  project_id: string;
}

export type JobState = "pending" | "running" | "done" | "failed" | "cancelled";

export interface JobStatus {
  job_id: string;
  project_id: string;
  kind: "route" | "optimize" | "place";
  state: JobState;
  percent: number;
  current_net: string;
  message: string;
  error: string;
  nets_done: number;
  nets_total: number;
  started_at: string;
  finished_at: string | null;
}

export interface ImportResult {
  format: string;
  components: number;
  nets: number;
  tracks: number;
  vias: number;
  warnings: string[];
}

export interface ExportFile {
  name: string;
  size_bytes: number;
}

export interface ErrorResponse {
  error: {
    code: string;
    message: string;
  };
}

// --------------------------------------------------------------
// Magic Pack (endpoints additifs)
// --------------------------------------------------------------

export type MagicActionKind =
  | "place_component"
  | "move_component"
  | "delete_component"
  | "set_track_width"
  | "add_net_class"
  | "route"
  | "optimize"
  | "run_drc"
  | "run_erc"
  | "export";

export interface MagicAction {
  kind: MagicActionKind;
  params: Record<string, unknown>;
  summary: string;
  executable: boolean;
}

export interface MagicInterpretation {
  utterance: string;
  language: "fr" | "en";
  actions: MagicAction[];
  confidence: number;
  reply: string;
}

export interface MagicResult {
  interpretation: MagicInterpretation;
  applied: string[];
  skipped: string[];
  board_changed: boolean;
  rules_changed: boolean;
  mode: "interpret" | "apply";
}

export interface ThermalHotspot {
  rank: number;
  x: number;
  y: number;
  temp_c: number;
  above_ambient_c: number;
  likely_ref: string;
}

export interface ThermalResult {
  grid_w: number;
  grid_h: number;
  cell_mm: number;
  ambient_c: number;
  max_temp_c: number;
  mean_temp_c: number;
  min_temp_c: number;
  max_gradient_c_per_mm: number;
  hotspots: ThermalHotspot[];
  grid: number[];
  warnings: string[];
}

export type SICriticality = "ok" | "warning" | "critical";

export interface SINetReport {
  net: string;
  length_mm: number;
  via_count: number;
  width_mm: number;
  z0_ohms: number;
  delay_ps: number;
  reflection_budget_ps: number;
  eye_height_pct: number;
  eye_width_ps: number;
  jitter_ps: number;
  criticality: SICriticality;
  advices: string[];
}

export interface SIResult {
  driver_rise_time_ps: number;
  bit_period_ps: number;
  analyzed: number;
  ok_count: number;
  warning_count: number;
  critical_count: number;
  nets: SINetReport[];
  worst_eye_net: string;
  si_score_pct: number;
  summary: string;
}

export interface ArenaStanding {
  strategy: string;
  rating: number;
  matches: number;
  wins: number;
  losses: number;
  draws: number;
}

export interface ArenaFighterCard {
  strategy: string;
  completed: number;
  failed: number;
  total_length_mm: number;
  via_count: number;
  collisions: number;
  duration_ms: number;
  score: number;
  net_log: string[];
}

export interface ArenaReport {
  project_id: string;
  nets: string[];
  greedy: ArenaFighterCard;
  astar: ArenaFighterCard;
  winner: "greedy" | "astar" | "draw";
  margin: number;
  elo: [ArenaStanding, ArenaStanding];
  log: string[];
  at: string;
}
