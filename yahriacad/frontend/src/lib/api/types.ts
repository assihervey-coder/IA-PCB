/**
 * YahriaCad — types de l'API (frontend).
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

/** État détaillé du modèle RL PyTorch embarqué dans le moteur IA (additif). */
export interface AIModelInfo {
  loaded: boolean;
  device: string; // "cpu" | "cuda" | "none"
  checkpoint_path: string;
  checkpoint_mtime: string;
  size_bytes: number;
  in_channels: number;
  n_actions: number;
  param_count: number;
  torch_available: boolean;
  strategy: "rl" | "astar" | string;
}

export interface AIModelReloadResult {
  loaded: boolean;
  message: string;
  info: AIModelInfo;
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

/** Benchmark A* (local) contre RL (moteur IA) — même carnet de nets. */
export interface ArenaBenchmark {
  project_id: string;
  nets: string[];
  astar: ArenaFighterCard;
  rl: ArenaFighterCard;
  winner: "astar" | "rl" | "draw";
  margin: number;
  elo: [ArenaStanding, ArenaStanding];
  log: string[];
  at: string;
}

// --------------------------------------------------------------
// Pack WOW (endpoints additifs)
// --------------------------------------------------------------

// --- DRC Auto-Healer -------------------------------------------

export type AutoFixAction =
  | "widen_track"
  | "enlarge_via"
  | "nudge_edge"
  | "ripup_reroute";

export interface AutoFix {
  code: string;
  action: AutoFixAction;
  message: string;
  confidence: number;
  applied: boolean;
  target: string;
}

export interface AutoFixResult {
  dry_run: boolean;
  passed_before: boolean;
  passed_after: boolean;
  violations_before: number;
  violations_after: number;
  fixed: number;
  remaining: string[];
  fixes: AutoFix[];
  board_changed: boolean;
  duration_ms: number;
}

// --- Design Doctor ----------------------------------------------

export interface DoctorAxis {
  axe: string;
  score: number;
  max: number;
}

export interface DoctorPrescription {
  priority: number;
  axe: string;
  title: string;
  detail: string;
  gain_pts: number;
}

export interface DoctorMetrics {
  components: number;
  nets: number;
  unrouted_nets: number;
  tracks: number;
  total_length_mm: number;
  vias: number;
  utilization_pct: number;
  max_temp_c: number;
  si_score_pct: number;
  drc_violations: number;
  erc_violations: number;
  min_track_mm: number;
  min_drill_mm: number;
}

export type DoctorGrade = "A+" | "A" | "B" | "C" | "D";

export interface DoctorReport {
  score: number;
  grade: DoctorGrade;
  verdict: string;
  axes: DoctorAxis[];
  prescriptions: DoctorPrescription[];
  metrics: DoctorMetrics;
  duration_ms: number;
}

// --- Oracle DFM --------------------------------------------------

export interface DFMUnitPrice {
  qty: number;
  label: string;
  unit_eur: number;
  total_eur: number;
}

export interface DFMRiskFlag {
  code: string;
  message: string;
  impact_pct: number;
}

export interface DFMSurcharge {
  label: string;
  pct: number;
}

export interface DFMEstimate {
  currency: string;
  area_dm2: number;
  layers: number;
  via_count: number;
  via_density_per_cm2: number;
  min_track_mm: number;
  min_drill_mm: number;
  bom_lines: number;
  components: number;
  unit_prices: DFMUnitPrice[];
  first_pass_yield_pct: number;
  defect_risk: "faible" | "moyen" | "élevé";
  risk_flags: DFMRiskFlag[];
  surcharges: DFMSurcharge[];
  advice: string[];
  duration_ms: number;
}

// --- Time Machine -------------------------------------------------

export interface SnapshotBoardMeta {
  width_mm: number;
  height_mm: number;
  components: number;
  tracks: number;
  vias: number;
  length_mm: number;
  min_track_mm: number;
}

export interface SnapshotMeta {
  id: string;
  label: string;
  at: string; // RFC 3339
  board: SnapshotBoardMeta;
  rule_count: number;
}

export interface SnapshotList {
  snapshots: SnapshotMeta[];
}

export type DiffChange = "added" | "removed" | "moved" | "changed";

export interface DiffEntry {
  kind: "component" | "track" | "via" | "rule";
  change: DiffChange;
  target: string;
  detail: string;
}

export interface SnapshotDiff {
  snapshot_id: string;
  snapshot_label: string;
  entries: DiffEntry[];
  summary: string;
  changed: boolean;
}

// --- Stats live ---------------------------------------------------

export interface StatsBoard {
  width_mm: number;
  height_mm: number;
  area_cm2: number;
  layer_count: number;
}

export interface StatsLayerUsage {
  layer: number;
  name: string;
  tracks: number;
  length_mm: number;
  vias_touching: number;
}

export interface StatsNet {
  name: string;
  class: string;
  tracks: number;
  length_mm: number;
  vias: number;
  routed: boolean;
  pad_count: number;
}

export interface StatsNetClass {
  class: string;
  nets: number;
  length_mm: number;
}

export interface StatsReport {
  board: StatsBoard;
  components: number;
  pads: number;
  nets: number;
  tracked_nets: number;
  tracks: number;
  vias: number;
  total_length_mm: number;
  utilization_pct: number;
  layer_usage: StatsLayerUsage[];
  top_nets: StatsNet[];
  net_classes: StatsNetClass[];
  duration_ms: number;
}

// --- Présence collaborative (WebSocket) ----------------------------

export type PresenceEvent = "join" | "move" | "leave";

export interface PresenceCursor {
  x: number;
  y: number;
}

export interface PresenceMessage {
  type: "presence";
  event: PresenceEvent;
  project_id: string;
  user: string;
  cursor: PresenceCursor;
  tool: string;
}

// --- Collaboration CRDT (REST + WebSocket, additif) ----------------

export type CollabOpKind =
  | "component.move"
  | "component.rotate"
  | "track.add"
  | "track.remove"
  | "via.add"
  | "via.remove"
  | "constraint.width";

export interface CollabPoint {
  x: number;
  y: number;
}

/** Union plate des payloads (seuls les champs utiles au kind sont lus). */
export interface CollabPayload {
  x?: number;
  y?: number;
  rotation?: number;
  net?: string;
  layer?: number;
  width?: number;
  points?: CollabPoint[];
  from_layer?: number;
  to_layer?: number;
  diameter?: number;
  drill?: number;
  mm?: number;
  net_class?: string;
}

export interface CollabOp {
  id: string;
  actor: string;
  lamport: number;
  kind: CollabOpKind;
  target?: string;
  payload: CollabPayload;
  at: string;
}

export interface CollabOpRequest {
  client_id?: string;
  lamport?: number;
  kind: CollabOpKind;
  target?: string;
  payload: CollabPayload;
}

export interface CollabApplyResult {
  project_id: string;
  actor: string;
  applied: number;
  seq: number;
  ops: CollabOp[];
  rejected: Array<{ client_id?: string; kind: string; reason: string }>;
}

/** Entrée du journal renvoyée par GET .../collab/state?since=N. */
export interface CollabLogEntry {
  seq: number;
  op: CollabOp;
  applied: boolean;
}

export interface CollabState {
  project_id: string;
  seq: number;
  clock: Record<string, number>;
  ops?: CollabLogEntry[];
}

export interface CollabUndoResult {
  actor: string;
  undone: boolean;
  op?: CollabOp;
  reason?: string;
}

/** Événement « collab » diffusé sur /ws/v1/progress. */
export interface CollabWsMessage {
  type: "collab";
  project_id: string;
  op: CollabOp;
  seq: number;
}

// --- Impédance différentielle (additif) ----------------------------

export interface DiffPairReport {
  net_plus: string;
  net_minus: string;
  net_class: string;
  routed: boolean;
  width_mm: number;
  gap_mm: number;
  length_plus_mm: number;
  length_minus_mm: number;
  skew_mm: number;
  skew_ps: number;
  z0_ohms: number;
  zodd_ohms: number;
  zeven_ohms: number;
  zdiff_ohms: number;
  zcom_ohms: number;
  target_ohms: number;
  error_pct: number;
  in_tolerance: boolean;
  recommended_width_mm: number;
  recommended_gap_mm: number;
  criticality: SICriticality;
  advices: string[];
}

export interface DiffClassReport {
  class: string;
  target_ohms: number;
  pairs: DiffPairReport[];
}

export interface DiffImpedanceRequest {
  targets?: Record<string, number>;
  gap_mm?: number;
  tolerance_pct?: number;
}

export interface DiffImpedanceResult {
  stackup: {
    er_relative: number;
    dielectric_height_mm: number;
    copper_thickness_mm: number;
    propagation_mm_per_ps: number;
  };
  tolerance_pct: number;
  analyzed_pairs: number;
  in_tolerance_count: number;
  worst_pair: string;
  summary: string;
  classes: DiffClassReport[];
  duration_ms: number;
}
