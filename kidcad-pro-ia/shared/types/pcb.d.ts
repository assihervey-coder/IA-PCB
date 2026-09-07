/**
 * KidCAD-Pro-IA — types TypeScript partagés.
 *
 * Ce fichier reflète :
 *   1. le contrat gRPC `pcb.proto` (échanges backend <-> moteur IA) ;
 *   2. les DTO de l'API REST du backend Go (échanges frontend <-> backend).
 *
 * Dans une chaîne outillée, les sections 1 sont générées par `ts-proto` ;
 * elles sont ici vérifiées manuellement pour rester synchrones de pcb.proto.
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

export interface Via {
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

export type AIStrategy = 'rl' | 'heuristic' | 'astar';

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
  vias: Via[];
  length_mm: number;
  completed: boolean;
  drc_violations: number;
}

export interface ProgressEvent {
  job_id: string;
  stage: 'route' | 'optimize';
  current_net: string;
  percent: number;
  message: string;
  partial: RouteNetResult | null;
  done: boolean;
  error: string;
}

export interface HealthResponse {
  status: 'ok' | 'degraded';
  version: string;
  device: string;
  model_loaded: boolean;
}

// ==============================================================
// 2. DTO de l'API REST du backend (cf. docs/architecture/contracts.md)
// ==============================================================

export type ProjectStatus = 'active' | 'archived';

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

/** Layout complet d'un projet (sérialisation JSON du board). */
export interface LayoutData {
  board: {
    width_mm: number;
    height_mm: number;
    layer_count: number;
    layer_names: string[];
  };
  components: Array<{
    ref: string;
    footprint: string;
    x: number;
    y: number;
    rotation: number;
    fixed: boolean;
  }>;
  nets: Array<{ name: string; net_class: string; pad_count: number }>;
  tracks: Array<{
    net: string;
    layer: number;
    width: number;
    points: Array<{ x: number; y: number }>;
  }>;
  vias: Array<{
    net: string;
    x: number;
    y: number;
    from_layer: number;
    to_layer: number;
    diameter: number;
    drill: number;
  }>;
}

export interface DRCViolation {
  code: string;
  severity: 'error' | 'warning';
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
  severity: 'error' | 'warning';
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

export type JobState = 'pending' | 'running' | 'done' | 'failed' | 'cancelled';

export interface JobStatus {
  job_id: string;
  project_id: string;
  kind: 'route' | 'optimize' | 'place';
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
