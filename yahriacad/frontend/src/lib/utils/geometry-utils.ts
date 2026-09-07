/**
 * Utilitaires géométriques pour les éditeurs (transformations canvas,
 * distances, empreintes estimées).
 */
import type { LayoutTrack } from "@/lib/api/types";

/** État de vue pan/zoom d'un canvas : offsets en px écran et échelle px/mm. */
export interface PanZoom {
  offsetX: number;
  offsetY: number;
  scale: number;
}

export interface WorldPoint {
  x: number;
  y: number;
}

export function screenToWorld(px: number, py: number, t: PanZoom): WorldPoint {
  return { x: (px - t.offsetX) / t.scale, y: (py - t.offsetY) / t.scale };
}

export function worldToScreen(x: number, y: number, t: PanZoom): WorldPoint {
  return { x: x * t.scale + t.offsetX, y: y * t.scale + t.offsetY };
}

/** Distance euclidienne entre un point et un segment [A, B]. */
export function pointToSegmentDistance(
  px: number,
  py: number,
  ax: number,
  ay: number,
  bx: number,
  by: number,
): number {
  const dx = bx - ax;
  const dy = by - ay;
  const lengthSq = dx * dx + dy * dy;
  if (lengthSq === 0) return Math.hypot(px - ax, py - ay);
  let t = ((px - ax) * dx + (py - ay) * dy) / lengthSq;
  t = Math.max(0, Math.min(1, t));
  return Math.hypot(px - (ax + t * dx), py - (ay + t * dy));
}

export interface BBox {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

/** Rectangle englobant une liste de points (null si vide). */
export function bboxOfPoints(points: Array<{ x: number; y: number }>): BBox | null {
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const p of points) {
    if (p.x < minX) minX = p.x;
    if (p.y < minY) minY = p.y;
    if (p.x > maxX) maxX = p.x;
    if (p.y > maxY) maxY = p.y;
  }
  if (!Number.isFinite(minX)) return null;
  return { minX, minY, maxX, maxY };
}

/** Rectangle englobant tous les points des pistes (null si vide). */
export function bboxOfTracks(tracks: LayoutTrack[]): BBox | null {
  return bboxOfPoints(tracks.flatMap((t) => t.points));
}

/** Aimante une coordonnée à la grille (en mm). */
export function snapToGrid(value: number, grid: number): number {
  if (grid <= 0) return value;
  return Math.round(value / grid) * grid;
}

/** Convertit une polyligne en attribut `d` SVG. */
export function trackPointsToPath(points: Array<{ x: number; y: number }>): string {
  if (points.length === 0) return "";
  return points
    .map((p, i) => `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`)
    .join(" ");
}

// ============================================================
// Estimation des empreintes (les DTO LayoutData ne portent pas
// de bbox : on déduit une taille plausible du nom d'empreinte).
// ============================================================

export interface FootprintSize {
  /** Largeur du corps en mm (axe X local). */
  w: number;
  /** Hauteur du corps en mm (axe Y local). */
  h: number;
  /** Nombre de broches estimé. */
  pins: number;
}

const PRESETS: Array<{ test: RegExp; size: FootprintSize }> = [
  { test: /0402/, size: { w: 1.0, h: 0.5, pins: 2 } },
  { test: /0603/, size: { w: 1.6, h: 0.8, pins: 2 } },
  { test: /0805/, size: { w: 2.0, h: 1.25, pins: 2 } },
  { test: /1206/, size: { w: 3.2, h: 1.6, pins: 2 } },
  { test: /SOT[-_]?223/, size: { w: 6.5, h: 3.5, pins: 4 } },
  { test: /SOT[-_]?23/, size: { w: 2.9, h: 2.4, pins: 3 } },
];

function matchCount(value: string, re: RegExp): number | null {
  const m = re.exec(value);
  return m ? parseInt(m[1], 10) : null;
}

export function estimateFootprintSize(footprint: string): FootprintSize {
  const f = (footprint ?? "").toUpperCase();
  for (const preset of PRESETS) {
    if (preset.test.test(f)) return preset.size;
  }
  const soic = matchCount(f, /SOIC[-_]?(\d+)/) ?? matchCount(f, /SSOP[-_]?(\d+)/);
  if (soic) {
    const perSide = Math.ceil(soic / 2);
    return { w: Math.max(5.4, perSide * 1.27 + 1.6), h: 4, pins: soic };
  }
  const dip = matchCount(f, /DIP[-_]?(\d+)/);
  if (dip) {
    const perSide = Math.ceil(dip / 2);
    return { w: Math.max(8.6, perSide * 2.54 + 1), h: 6, pins: dip };
  }
  const qfp = matchCount(f, /[TQV]QFP[-_]?(\d+)/);
  if (qfp) return { w: 8, h: 8, pins: qfp };
  if (f.includes("TO-220") || f.includes("TO220")) return { w: 10, h: 4.5, pins: 3 };
  if (f.includes("HDR") || f.includes("HEADER") || f.includes("CONN")) {
    return { w: 5.08, h: 2.54, pins: 3 };
  }
  if (f.includes("LED")) return { w: 2.0, h: 1.25, pins: 2 };
  return { w: 4, h: 3, pins: 4 };
}

export interface Offset {
  x: number;
  y: number;
}

/** Positions locales des pastilles : réparties sur les flancs gauche/droit. */
export function footprintPadOffsets(size: FootprintSize): Offset[] {
  const left = Math.ceil(size.pins / 2);
  const right = size.pins - left;
  const span = size.h * 0.68;
  const offsets: Offset[] = [];
  const column = (count: number, x: number): void => {
    for (let i = 0; i < count; i++) {
      const y = count === 1 ? 0 : -span / 2 + (span * i) / (count - 1);
      offsets.push({ x, y });
    }
  };
  column(left, -size.w / 2 + 0.35);
  column(right, size.w / 2 - 0.35);
  return offsets;
}
