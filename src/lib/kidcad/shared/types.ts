/**
 * KidCAD-Pro-IA — Types partagés (shared/types)
 * Unités : millimètres partout. Angles en degrés. Couches : 2 (F.Cu / B.Cu).
 * Ce module est pur TypeScript, sans dépendance : il est importé à la fois par
 * l'application Next.js et par le mini-service IA (socket.io, port 3010).
 */

export type Layer = 'F.Cu' | 'B.Cu'
export type PadShape = 'circle' | 'rect' | 'oval'
export type Side = 'top' | 'bottom'

export interface Point {
  x: number
  y: number
}

// ---------------------------------------------------------------------------
// Schéma électrique
// ---------------------------------------------------------------------------

export type SymbolKind = 'R' | 'C' | 'CP' | 'D' | 'LED' | 'IC' | 'J' | 'Y' | 'Q'

export interface SchematicPin {
  id: string // numéro de borne ("1", "2", "A"…)
  name: string
  x: number // coordonnée relative au symbole (mm)
  y: number
}

export interface SchematicComponent {
  id: string
  ref: string // R1, C3, U1…
  value: string // 10k, 100nF, NE555…
  symbol: SymbolKind
  pins: SchematicPin[]
  x: number
  y: number
  rotation: 0 | 90 | 180 | 270
  footprintName: string // clé dans la bibliothèque d'empreintes
}

export interface SchematicWire {
  id: string
  pts: Point[]
}

export interface SchematicData {
  components: SchematicComponent[]
  wires: SchematicWire[]
}

// ---------------------------------------------------------------------------
// Layout PCB
// ---------------------------------------------------------------------------

/** Pad placé — coordonnées ABSOLUES (recalculées lors du déplacement/rotation). */
export interface PlacedPad {
  number: string // numéro de borne liée au symbole
  x: number
  y: number
  w: number
  h: number
  shape: PadShape
  layer: Layer
  netId: string | null
}

export interface FootprintInst {
  id: string
  componentId: string // lien vers le composant du schéma
  ref: string
  value: string
  footprintName: string
  x: number
  y: number
  rotation: number // degrés, sens horaire
  side: Side
  bodyW: number // encombrement du corps (mm)
  bodyH: number
  pads: PlacedPad[]
}

export interface Track {
  id: string
  netId: string
  layer: Layer
  width: number
  pts: Point[]
}

export interface Via {
  id: string
  netId: string
  x: number
  y: number
  drill: number
  diameter: number
}

export interface Net {
  id: string
  name: string
  pins: { componentId: string; pad: string }[] // pad = numéro de borne
}

export interface LayoutData {
  footprints: FootprintInst[]
  tracks: Track[]
  vias: Via[]
}

// ---------------------------------------------------------------------------
// Règles de conception (contraintes)
// ---------------------------------------------------------------------------

export interface DesignRules {
  /** Largeur de piste utilisée par le routeur (mm) */
  trackWidth: number
  /** Largeur de piste minimale autorisée (DRC, mm) */
  minTrackWidth: number
  /** Isolation minimale cuivre/cuivre (mm) */
  minClearance: number
  /** Perçage minimal d'un via (mm) */
  minViaDrill: number
  /** Diamètre extérieur minimal d'un via (mm) */
  minViaDiameter: number
  /** Distance minimale cuivre / bord de carte (mm) */
  edgeClearance: number
  /** Pas de la grille de routage IA (mm) */
  gridStep: number
  /** Via par défaut du routeur */
  viaDrill: number
  viaDiameter: number
}

export const DEFAULT_RULES: DesignRules = {
  trackWidth: 0.3,
  minTrackWidth: 0.25,
  minClearance: 0.2,
  minViaDrill: 0.35,
  minViaDiameter: 0.7,
  edgeClearance: 0.3,
  gridStep: 0.635, // 25 mil
  viaDrill: 0.35,
  viaDiameter: 0.8,
}

export interface BoardSpec {
  width: number
  height: number
  layers: 2
}

// ---------------------------------------------------------------------------
// Design complet (agrégat racine)
// ---------------------------------------------------------------------------

export interface Design {
  id: string
  name: string
  description: string
  board: BoardSpec
  rules: DesignRules
  schematic: SchematicData
  /** Netlist du projet (dérivée du schéma ou importée) — source de vérité du routage. */
  nets: Net[]
  layout: LayoutData
}

export interface DesignStats {
  components: number
  nets: number
  tracks: number
  vias: number
  routedNets: number
  totalNets: number
  wireLength: number // mm de piste
}

// ---------------------------------------------------------------------------
// Rapports de vérification
// ---------------------------------------------------------------------------

export type DrcViolationType =
  | 'CLEARANCE_TRACK'
  | 'CLEARANCE_PAD'
  | 'CLEARANCE_VIA'
  | 'TRACK_WIDTH'
  | 'VIA_DRILL'
  | 'EDGE_CLEARANCE'
  | 'UNROUTED'

export type ErcViolationType =
  | 'UNCONNECTED_PIN'
  | 'SINGLE_PIN_NET'
  | 'POWER_SHORT'
  | 'DUPLICATE_REF'

export interface Violation {
  type: DrcViolationType | ErcViolationType
  severity: 'error' | 'warning'
  message: string
  at: Point
  items?: string[] // refs/netIds concernés
}

export interface DrcReport {
  ok: boolean
  violations: Violation[]
  checkedAt: string
  rules: DesignRules
}

export interface ErcReport {
  ok: boolean
  violations: Violation[]
  checkedAt: string
  netCount: number
}

// ---------------------------------------------------------------------------
// Moteur IA — contrats (analogue de shared/pcb.proto)
// ---------------------------------------------------------------------------

export type AiTask = 'place' | 'route' | 'optimize'

export interface AiProgress {
  stage: string
  progress: number // 0..100
  message: string
}

export interface AiPlaceOptions {
  iterations?: number // itérations de recuit simulé
  keepAlignment?: boolean
}

export interface AiRouteOptions {
  viaCost?: number
  allowRipup?: boolean
  maxPasses?: number
}

export interface AiResultStats {
  routedNets: number
  totalNets: number
  tracks: number
  vias: number
  wireLength: number
  durationMs: number
  passes: number
}

export interface SocketSchemas {
  'ai:place': (p: { design: Design; options?: AiPlaceOptions }) => void
  'ai:route': (p: { design: Design; options?: AiRouteOptions }) => void
  'ai:optimize': (p: { design: Design; options?: AiRouteOptions }) => void
  'ai:cancel': () => void
  'ai:progress': (p: AiProgress) => void
  'ai:result': (p: { task: AiTask; design: Design; stats: AiResultStats }) => void
  'ai:error': (p: { message: string }) => void
}
