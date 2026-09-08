/**
 * domain/schematic/schematic — Agrégat schéma électrique.
 * Extraction de netlist par union-find (connectivité wires + bornes).
 */
import { Net, Point, SchematicComponent, SchematicData, SchematicPin } from '../../shared/types'
import { distPointSeg } from '../../pkg/geometry'

const JOIN_TOL = 0.15 // tolérance d'accroche borne/fil (mm)

/** Position absolue d'une borne de composant (avec rotation). */
export function pinWorldPos(comp: SchematicComponent, pin: SchematicPin): Point {
  const r = (comp.rotation * Math.PI) / 180
  const c = Math.cos(r)
  const s = Math.sin(r)
  return {
    x: comp.x + pin.x * c + pin.y * s,
    y: comp.y - pin.x * s + pin.y * c,
  }
}

/** Toutes les bornes en coordonnées monde. */
export function componentPinsWorld(comp: SchematicComponent): { pin: SchematicPin; pos: Point }[] {
  return comp.pins.map((pin) => ({ pin, pos: pinWorldPos(comp, pin) }))
}

interface DSU {
  find(i: number): number
  union(a: number, b: number): void
}

function makeDSU(n: number): DSU {
  const parent = Array.from({ length: n }, (_, i) => i)
  const find = (i: number): number => (parent[i] === i ? i : (parent[i] = find(parent[i])))
  return {
    find,
    union(a: number, b: number) {
      const ra = find(a)
      const rb = find(b)
      if (ra !== rb) parent[ra] = rb
    },
  }
}

export interface ExtractedNetlist {
  nets: Net[] // nets nommés automatiquement N$1, N$2… sauf wires nommés
  unconnectedPins: { componentId: string; pin: string }[]
}

/**
 * Extrait la netlist du schéma : les bornes reliées par des fils (ou
 * directement coïncidentes) appartiennent au même net.
 */
export function extractNetlist(sch: SchematicData): ExtractedNetlist {
  type PinKey = { compId: string; pinId: string; pos: Point }
  const pinKeys: PinKey[] = []
  for (const comp of sch.components) {
    for (const { pin, pos } of componentPinsWorld(comp)) {
      pinKeys.push({ compId: comp.id, pinId: pin.id, pos })
    }
  }

  const nPins = pinKeys.length
  const nWires = sch.wires.length
  const dsu = makeDSU(nPins + nWires)

  // Borne ↔ borne directement confondues
  for (let i = 0; i < nPins; i++) {
    for (let j = i + 1; j < nPins; j++) {
      if (Math.hypot(pinKeys[i].pos.x - pinKeys[j].pos.x, pinKeys[i].pos.y - pinKeys[j].pos.y) < JOIN_TOL) {
        dsu.union(i, j)
      }
    }
  }
  // Borne ↔ fil (chaque segment du fil)
  for (let w = 0; w < nWires; w++) {
    const pts = sch.wires[w].pts
    for (let s = 0; s < pts.length - 1; s++) {
      for (let p = 0; p < nPins; p++) {
        if (distPointSeg(pinKeys[p].pos, pts[s], pts[s + 1]) < JOIN_TOL) {
          dsu.union(p, nPins + w)
        }
      }
    }
  }
  // Fil ↔ fil (extrémités / intersections d'extrémités)
  for (let w1 = 0; w1 < nWires; w1++) {
    for (let w2 = w1 + 1; w2 < nWires; w2++) {
      const a = sch.wires[w1].pts
      const b = sch.wires[w2].pts
      const endsA = [a[0], a[a.length - 1]]
      const endsB = [b[0], b[b.length - 1]]
      let joined = false
      for (const p of endsA) {
        for (const q of endsB) {
          if (Math.hypot(p.x - q.x, p.y - q.y) < JOIN_TOL) {
            dsu.union(nPins + w1, nPins + w2)
            joined = true
            break
          }
        }
        if (joined) break
      }
    }
  }

  // Regroupement des bornes par racine union-find
  const groups = new Map<number, { compId: string; pin: string }[]>()
  for (let i = 0; i < nPins; i++) {
    const root = dsu.find(i)
    if (!groups.has(root)) groups.set(root, [])
    groups.get(root)!.push({ compId: pinKeys[i].compId, pin: pinKeys[i].pinId })
  }

  // Groupe ≥ 2 bornes → net. Borne isolée → non connectée.
  const nets: Net[] = []
  let counter = 1
  const unconnectedPins: { componentId: string; pin: string }[] = []
  for (const [root, pins] of groups) {
    if (pins.length >= 2) {
      nets.push({ id: `net_${root}`, name: `N$${counter++}`, pins })
    } else if (pins.length === 1) {
      unconnectedPins.push(pins[0])
    }
  }

  return { nets, unconnectedPins }
}

/** Validation structurelle du schéma. */
export function validateSchematic(sch: SchematicData): { ok: boolean; problems: string[] } {
  const problems: string[] = []
  const refs = new Set<string>()
  for (const c of sch.components) {
    if (refs.has(c.ref)) problems.push(`Référence dupliquée : ${c.ref}`)
    refs.add(c.ref)
    if (c.pins.length === 0) problems.push(`${c.ref} : symbole sans borne`)
    if (!c.footprintName) problems.push(`${c.ref} : aucune empreinte associée`)
  }
  return { ok: problems.length === 0, problems }
}
