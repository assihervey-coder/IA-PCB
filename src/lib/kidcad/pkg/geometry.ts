/**
 * pkg/geometry — Primitives géométriques 2D (mm).
 * Utilisé par le moteur IA, la DRC, les exports et les canvas.
 */
import { Point } from '../shared/types'

export const dist = (a: Point, b: Point): number => Math.hypot(a.x - b.x, a.y - b.y)

export const dist2 = (a: Point, b: Point): number => (a.x - b.x) ** 2 + (a.y - b.y) ** 2

export const add = (a: Point, b: Point): Point => ({ x: a.x + b.x, y: a.y + b.y })
export const sub = (a: Point, b: Point): Point => ({ x: a.x - b.x, y: a.y - b.y })
export const mid = (a: Point, b: Point): Point => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 })

/** Distance point → segment [a,b]. */
export function distPointSeg(p: Point, a: Point, b: Point): number {
  const abx = b.x - a.x
  const aby = b.y - a.y
  const len2 = abx * abx + aby * aby
  if (len2 === 0) return dist(p, a)
  let t = ((p.x - a.x) * abx + (p.y - a.y) * aby) / len2
  t = Math.max(0, Math.min(1, t))
  return dist(p, { x: a.x + t * abx, y: a.y + t * aby })
}

/** Distance minimale entre deux segments. */
export function distSegSeg(p1: Point, p2: Point, p3: Point, p4: Point): number {
  // Intersection propre ?
  const d1x = p2.x - p1.x, d1y = p2.y - p1.y
  const d2x = p4.x - p3.x, d2y = p4.y - p3.y
  const den = d1x * d2y - d1y * d2x
  if (den !== 0) {
    const t = ((p3.x - p1.x) * d2y - (p3.y - p1.y) * d2x) / den
    const u = ((p3.x - p1.x) * d1y - (p3.y - p1.y) * d1x) / den
    if (t >= 0 && t <= 1 && u >= 0 && u <= 1) return 0
  }
  return Math.min(
    distPointSeg(p1, p3, p4),
    distPointSeg(p2, p3, p4),
    distPointSeg(p3, p1, p2),
    distPointSeg(p4, p1, p2),
  )
}

/** Distance entre deux rectangles alignés aux axes. */
export function distRectRect(a: { x: number; y: number; w: number; h: number }, b: { x: number; y: number; w: number; h: number }): number {
  const dx = Math.max(0, Math.abs((a.x + a.w / 2) - (b.x + b.w / 2)) - (a.w + b.w) / 2)
  const dy = Math.max(0, Math.abs((a.y + a.h / 2) - (b.y + b.h / 2)) - (a.h + b.h) / 2)
  return Math.hypot(dx, dy)
}

/** Rotation d'un point autour de l'origine (sens horaire, degrés). */
export function rotate(p: Point, deg: number): Point {
  const r = (deg * Math.PI) / 180
  const c = Math.cos(r)
  const s = Math.sin(r)
  return { x: p.x * c + p.y * s, y: -p.x * s + p.y * c }
}

/** Transformation locale→monde d'un empreinte (position + rotation). */
export function toWorld(local: Point, pos: Point, rotation: number): Point {
  const r = rotate(local, rotation)
  return { x: pos.x + r.x, y: pos.y + r.y }
}

/** Longueur d'une polyligne. */
export function polyLen(pts: Point[]): number {
  let l = 0
  for (let i = 1; i < pts.length; i++) l += dist(pts[i - 1], pts[i])
  return l
}

/** Fusion des points collinéaires d'une polyligne (réduction du nombre de segments). */
export function simplifyPolyline(pts: Point[], eps = 1e-6): Point[] {
  if (pts.length <= 2) return pts
  const out: Point[] = [pts[0]]
  for (let i = 1; i < pts.length - 1; i++) {
    const a = out[out.length - 1]
    const b = pts[i]
    const c = pts[i + 1]
    const cross = (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x)
    const dot = (b.x - a.x) * (c.x - a.x) + (b.y - a.y) * (c.y - a.y)
    if (Math.abs(cross) > eps || dot < 0) out.push(b)
  }
  out.push(pts[pts.length - 1])
  return out
}

/** Boîte englobante d'un ensemble de points. */
export function bbox(points: Point[]): { minX: number; minY: number; maxX: number; maxY: number } {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
  for (const p of points) {
    if (p.x < minX) minX = p.x
    if (p.y < minY) minY = p.y
    if (p.x > maxX) maxX = p.x
    if (p.y > maxY) maxY = p.y
  }
  return { minX, minY, maxX, maxY }
}

/** HPWL (half-perimeter wire length) entre plusieurs points. */
export function hpwl(points: Point[]): number {
  if (points.length < 2) return 0
  const b = bbox(points)
  return b.maxX - b.minX + (b.maxY - b.minY)
}
