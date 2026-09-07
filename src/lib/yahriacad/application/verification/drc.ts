/**
 * application/verification/drc — Design Rule Check.
 * Vérifie largeurs de pistes, perçages, isolements (hachage spatial),
 * distance au bord et nets non routés.
 */
import { Design, DrcReport, Layer, PlacedPad, Track, Via, Violation } from '../../shared/types'
import { ConstraintSet } from '../../domain/constraints/constraint_set'
import { distPointSeg, distSegSeg } from '../../pkg/geometry'

interface Item {
  kind: 'track' | 'pad' | 'via'
  id: string
  netId: string | null
  layer: Layer
  /** piste : segment [a,b] + width ; pad/via : centre + rayon */
  a?: { x: number; y: number }
  b?: { x: number; y: number }
  cx?: number
  cy?: number
  radius: number // rayon équivalent (track : width/2, pad : (w+h)/4, via : diameter/2)
  through: boolean // traverse les 2 couches (via, pastille traversante)
  label: string
}

const BUCKET = 12 // mm

class SpatialHash {
  private map = new Map<number, Item[]>()
  private key(bx: number, by: number) {
    return (bx + 512) * 4096 + (by + 512)
  }
  insert(item: Item, bbox: { minX: number; minY: number; maxX: number; maxY: number }) {
    const x0 = Math.floor(bbox.minX / BUCKET)
    const x1 = Math.floor(bbox.maxX / BUCKET)
    const y0 = Math.floor(bbox.minY / BUCKET)
    const y1 = Math.floor(bbox.maxY / BUCKET)
    for (let bx = x0; bx <= x1; bx++) {
      for (let by = y0; by <= y1; by++) {
        const k = this.key(bx, by)
        if (!this.map.has(k)) this.map.set(k, [])
        this.map.get(k)!.push(item)
      }
    }
  }
  query(bbox: { minX: number; minY: number; maxX: number; maxY: number }): Item[] {
    const out = new Set<Item>()
    const x0 = Math.floor(bbox.minX / BUCKET)
    const x1 = Math.floor(bbox.maxX / BUCKET)
    const y0 = Math.floor(bbox.minY / BUCKET)
    const y1 = Math.floor(bbox.maxY / BUCKET)
    for (let bx = x0; bx <= x1; bx++) {
      for (let by = y0; by <= y1; by++) {
        const list = this.map.get(this.key(bx, by))
        if (list) for (const it of list) out.add(it)
      }
    }
    return [...out]
  }
}

function layerCompatible(a: Item, b: Item): boolean {
  if (a.through || b.through) return true
  return a.layer === b.layer
}

function itemBBox(it: Item) {
  if (it.kind === 'track' && it.a && it.b) {
    const r = it.radius
    return {
      minX: Math.min(it.a.x, it.b.x) - r,
      minY: Math.min(it.a.y, it.b.y) - r,
      maxX: Math.max(it.a.x, it.b.x) + r,
      maxY: Math.max(it.a.y, it.b.y) + r,
    }
  }
  const r = it.radius
  return { minX: (it.cx ?? 0) - r, minY: (it.cy ?? 0) - r, maxX: (it.cx ?? 0) + r, maxY: (it.cy ?? 0) + r }
}

/** Distance entre deux items (approximation cohérente avec la DRC). */
function itemDistance(a: Item, b: Item): number {
  if (a.kind === 'track' && b.kind === 'track' && a.a && a.b && b.a && b.b) {
    return distSegSeg(a.a, a.b, b.a, b.b) - a.radius - b.radius
  }
  if (a.kind === 'track' && a.a && a.b) {
    const d = distPointSeg({ x: b.cx ?? 0, y: b.cy ?? 0 }, a.a, a.b)
    return d - a.radius - b.radius
  }
  if (b.kind === 'track' && b.a && b.b) {
    const d = distPointSeg({ x: a.cx ?? 0, y: a.cy ?? 0 }, b.a, b.b)
    return d - a.radius - b.radius
  }
  return Math.hypot((a.cx ?? 0) - (b.cx ?? 0), (a.cy ?? 0) - (b.cy ?? 0)) - a.radius - b.radius
}

export function runDrc(design: Design): DrcReport {
  const rules = new ConstraintSet(design.rules).value
  const violations: Violation[] = []
  const { width, height } = design.board
  const eps = 1e-4

  const padIsThrough = (p: PlacedPad) => p.layer === 'B.Cu' // convention lib : THT → pastille B.Cu

  // ------------------------------------------------------------------
  // Collecte des items
  // ------------------------------------------------------------------
  const items: Item[] = []
  for (const t of design.layout.tracks) {
    for (let i = 1; i < t.pts.length; i++) {
      items.push({
        kind: 'track', id: t.id, netId: t.netId, layer: t.layer,
        a: t.pts[i - 1], b: t.pts[i], radius: t.width / 2, through: false,
        label: `piste ${t.id.slice(0, 6)}`,
      })
    }
  }
  for (const fp of design.layout.footprints) {
    for (const pad of fp.pads) {
      items.push({
        kind: 'pad', id: `${fp.id}:${pad.number}`, netId: pad.netId,
        layer: pad.layer, cx: pad.x, cy: pad.y,
        radius: (pad.w + pad.h) / 4, through: padIsThrough(pad),
        label: `${fp.ref}.${pad.number}`,
      })
    }
  }
  for (const v of design.layout.vias) {
    items.push({
      kind: 'via', id: v.id, netId: v.netId, layer: 'F.Cu',
      cx: v.x, cy: v.y, radius: v.diameter / 2, through: true,
      label: `via ${v.id.slice(0, 6)}`,
    })
  }

  // ------------------------------------------------------------------
  // 1) Largeur de piste / perçage via
  // ------------------------------------------------------------------
  for (const t of design.layout.tracks) {
    if (t.width < rules.minTrackWidth - eps) {
      violations.push({
        type: 'TRACK_WIDTH', severity: 'error',
        message: `Piste trop fine : ${t.width.toFixed(3)} mm < ${rules.minTrackWidth} mm`,
        at: t.pts[0] ?? { x: 0, y: 0 }, items: [t.netId ?? ''],
      })
    }
  }
  for (const v of design.layout.vias) {
    if (v.drill < rules.minViaDrill - eps) {
      violations.push({
        type: 'VIA_DRILL', severity: 'error',
        message: `Perçage via trop petit : ${v.drill} mm < ${rules.minViaDrill} mm`,
        at: { x: v.x, y: v.y },
      })
    }
  }

  // ------------------------------------------------------------------
  // 2) Distance au bord
  // ------------------------------------------------------------------
  const edgeSegs: { a: { x: number; y: number }; b: { x: number; y: number } }[] = [
    { a: { x: 0, y: 0 }, b: { x: width, y: 0 } },
    { a: { x: width, y: 0 }, b: { x: width, y: height } },
    { a: { x: width, y: height }, b: { x: 0, y: height } },
    { a: { x: 0, y: height }, b: { x: 0, y: 0 } },
  ]
  const edgeDist = (x: number, y: number) =>
    Math.min(...edgeSegs.map((s) => distPointSeg({ x, y }, s.a, s.b)))
  for (const t of design.layout.tracks) {
    for (let i = 1; i < t.pts.length; i++) {
      const d = Math.min(edgeDist(t.pts[i - 1].x, t.pts[i - 1].y), edgeDist(t.pts[i].x, t.pts[i].y))
      if (d < rules.edgeClearance + t.width / 2 - eps) {
        violations.push({
          type: 'EDGE_CLEARANCE', severity: 'error',
          message: `Piste trop proche du bord (${d.toFixed(2)} mm)`,
          at: t.pts[i],
        })
        break
      }
    }
  }
  for (const v of design.layout.vias) {
    const d = edgeDist(v.x, v.y)
    if (d < rules.edgeClearance + v.diameter / 2 - eps) {
      violations.push({
        type: 'EDGE_CLEARANCE', severity: 'error',
        message: `Via trop proche du bord (${d.toFixed(2)} mm)`,
        at: { x: v.x, y: v.y },
      })
    }
  }

  // ------------------------------------------------------------------
  // 3) Isolements (hachage spatial)
  // ------------------------------------------------------------------
  const hash = new SpatialHash()
  for (const it of items) hash.insert(it, itemBBox(it))

  const seen = new Set<string>()
  for (const it of items) {
    const bbox = itemBBox(it)
    const margin = rules.minClearance + it.radius + 1
    const near = hash.query({
      minX: bbox.minX - margin, minY: bbox.minY - margin,
      maxX: bbox.maxX + margin, maxY: bbox.maxY + margin,
    })
    for (const other of near) {
      if (other === it) continue
      const pairKey = [it.id, other.id].sort().join('|')
      if (seen.has(pairKey)) continue
      seen.add(pairKey)
      if (it.netId && it.netId === other.netId) continue // même net : OK
      if (!layerCompatible(it, other)) continue
      const d = itemDistance(it, other)
      if (d < rules.minClearance - eps) {
        violations.push({
          type: it.kind === 'pad' || other.kind === 'pad' ? 'CLEARANCE_PAD' : it.kind === 'via' || other.kind === 'via' ? 'CLEARANCE_VIA' : 'CLEARANCE_TRACK',
          severity: 'error',
          message: `Isolement insuffisant (${d.toFixed(3)} mm < ${rules.minClearance} mm) entre ${it.label} et ${other.label}`,
          at: { x: (it.cx ?? it.a?.x ?? 0), y: (it.cy ?? it.a?.y ?? 0) },
          items: [it.netId ?? '?', other.netId ?? '?'],
        })
        if (violations.length > 400) break
      }
    }
    if (violations.length > 400) break
  }

  // ------------------------------------------------------------------
  // 4) Nets non routés
  // ------------------------------------------------------------------
  if (design.layout.tracks.length > 0 || design.layout.vias.length > 0) {
    const routed = new Set(design.layout.tracks.map((t) => t.netId))
    for (const net of design.nets) {
      if (net.pins.length >= 2 && !routed.has(net.id)) {
        const at = firstPadPos(design, net.id)
        violations.push({
          type: 'UNROUTED', severity: 'warning',
          message: `Net « ${net.name} » non routé (${net.pins.length} bornes)`,
          at: at ?? { x: 0, y: 0 }, items: [net.id],
        })
      }
    }
  }

  return {
    ok: violations.filter((v) => v.severity === 'error').length === 0,
    violations,
    checkedAt: new Date().toISOString(),
    rules,
  }
}

function firstPadPos(design: Design, netId: string): { x: number; y: number } | null {
  for (const fp of design.layout.footprints) {
    for (const pad of fp.pads) {
      if (pad.netId === netId) return { x: pad.x, y: pad.y }
    }
  }
  return null
}

/** Type utilitaire pour les autres modules. */
export type { Track, Via }
