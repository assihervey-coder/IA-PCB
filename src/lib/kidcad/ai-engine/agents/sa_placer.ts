/**
 * ai-engine/agents/sa_placer — Placement automatique par recuit simulé
 * (Simulated Annealing). Minimise le HPWL total des nets + pénalités de
 * chevauchement + hors-carte. Déterministe (RNG seedé).
 */
import { BoardSpec, DesignRules, FootprintInst, Net, Point } from '../../shared/types'
import { BaseAgent } from './base_agent'
import { mulberry32 } from '../models/graph_net'
import { rotate, hpwl } from '../../pkg/geometry'
import { snap } from '../../pkg/utils'

export interface PlaceInput {
  footprints: FootprintInst[] // copie de travail (mutée par l'agent)
  nets: Net[]
  board: BoardSpec
  rules: DesignRules
  iterations: number
  shouldCancel?: () => boolean
  onIter?: (iter: number, currentCost: number, bestCost: number) => void
}

export interface PlaceOutput {
  footprints: FootprintInst[]
  initialCost: number
  finalCost: number
  iterations: number
}

interface LightFp {
  ref: string
  x: number
  y: number
  rotation: number
  bodyW: number
  bodyH: number
  localPads: { number: string; dx: number; dy: number; netId: string | null; w: number; h: number }[]
}

/** Dérive la définition locale (non tournée) des pads d'une empreinte placée. */
function toLocal(fp: FootprintInst): LightFp {
  const localPads = fp.pads.map((p) => {
    const rel = rotate({ x: p.x - fp.x, y: p.y - fp.y }, -fp.rotation)
    return { number: p.number, dx: rel.x, dy: rel.y, netId: p.netId, w: p.w, h: p.h }
  })
  return { ref: fp.ref, x: fp.x, y: fp.y, rotation: fp.rotation, bodyW: fp.bodyW, bodyH: fp.bodyH, localPads }
}

function padWorld(fp: LightFp, pad: LightFp['localPads'][number]): Point {
  const r = rotate({ x: pad.dx, y: pad.dy }, fp.rotation)
  return { x: fp.x + r.x, y: fp.y + r.y }
}

function bodyRect(fp: LightFp, margin: number) {
  const half = Math.max(fp.bodyW, fp.bodyH) / 2 + margin
  return { minX: fp.x - half, minY: fp.y - half, maxX: fp.x + half, maxY: fp.y + half }
}

export class PlaceAgent extends BaseAgent<PlaceInput, PlaceOutput> {
  readonly name = 'SimulatedAnnealing-Placer'
  readonly strategy =
    'Recuit simulé — HPWL des nets + pénalités chevauchement/bord, mouvements : translation, échange, rotation 90°'

  private netsRef: { net: Net; fpRefs: { fpIdx: number; pad: string }[] }[] = []

  async run(input: PlaceInput): Promise<PlaceOutput> {
    const { board, rules, nets } = input
    const grid = Math.max(0.2, rules.gridStep)
    const rng = mulberry32(1337)
    let fps = input.footprints.map(toLocal)
    const idxByRef = new Map<string, number>()
    fps.forEach((fp, i) => idxByRef.set(fp.ref, i))

    // Index nets → (empreinte, pad)
    this.netsRef = []
    for (const net of nets) {
      const entries: { fpIdx: number; pad: string }[] = []
      for (const pin of net.pins) {
        const fpIdx = fps.findIndex((f) => f.ref === pin.componentId)
        if (fpIdx >= 0) entries.push({ fpIdx, pad: pin.pad })
      }
      if (entries.length >= 2) this.netsRef.push({ net, fpRefs: entries })
    }

    // Normalisation initiale : tout ce qui est hors carte est rangé en lignes
    this.normalize(fps, board, grid)

    const overlapMargin = 0.8 // mm autour du corps pour l'anti-chevauchement
    const edgePad = rules.edgeClearance + 0.5

    const cost = (): number => {
      let c = 0
      for (const { fpRefs } of this.netsRef) {
        const pts: Point[] = []
        for (const { fpIdx, pad } of fpRefs) {
          const fp = fps[fpIdx]
          const lp = fp.localPads.find((p) => p.number === pad)
          if (lp) pts.push(padWorld(fp, lp))
        }
        c += hpwl(pts)
      }
      // Chevauchements de corps
      for (let i = 0; i < fps.length; i++) {
        for (let j = i + 1; j < fps.length; j++) {
          const a = bodyRect(fps[i], overlapMargin)
          const b = bodyRect(fps[j], overlapMargin)
          const ox = Math.min(a.maxX, b.maxX) - Math.max(a.minX, b.minX)
          const oy = Math.min(a.maxY, b.maxY) - Math.max(a.minY, b.minY)
          if (ox > 0 && oy > 0) c += (ox * oy) * 4
        }
      }
      // Hors carte (les pads comptent, le corps aussi)
      for (const fp of fps) {
        const r = bodyRect(fp, 0)
        if (
          r.minX < edgePad || r.minY < edgePad ||
          r.maxX > board.width - edgePad || r.maxY > board.height - edgePad
        ) {
          c += 800
        }
      }
      return c
    }

    const insideBoard = (fp: LightFp): boolean => {
      const r = bodyRect(fp, 0)
      return (
        r.minX >= edgePad && r.minY >= edgePad &&
        r.maxX <= board.width - edgePad && r.maxY <= board.height - edgePad
      )
    }

    let current = cost()
    const initialCost = current
    let best = current
    let bestSnapshot = fps.map((f) => ({ ...f, localPads: [...f.localPads] }))

    const iterations = Math.max(200, input.iterations ?? 3000)
    const t0 = Math.max(1e-6, initialCost * 0.25)
    const tEnd = Math.max(1e-9, initialCost * 0.0008)
    const alpha = Math.pow(tEnd / t0, 1 / iterations)
    let T = t0

    for (let it = 0; it < iterations; it++) {
      if (input.shouldCancel?.()) break
      T *= alpha
      const kind = rng()
      if (kind < 0.6 || fps.length < 2) {
        // Translation
        const i = Math.floor(rng() * fps.length)
        const fp = fps[i]
        const span = 2 + Math.floor(rng() * 6)
        const nx = snap(fp.x + (rng() * 2 - 1) * span * grid, grid)
        const ny = snap(fp.y + (rng() * 2 - 1) * span * grid, grid)
        const old = { x: fp.x, y: fp.y }
        fp.x = nx
        fp.y = ny
        if (!insideBoard(fp)) {
          fp.x = old.x
          fp.y = old.y
          continue
        }
        const nc = cost()
        if (nc < current || rng() < Math.exp((current - nc) / T)) {
          current = nc
        } else {
          fp.x = old.x
          fp.y = old.y
        }
      } else if (kind < 0.85) {
        // Échange de positions
        const i = Math.floor(rng() * fps.length)
        let j = Math.floor(rng() * fps.length)
        if (i === j) j = (j + 1) % fps.length
        const a = fps[i]
        const b = fps[j]
        ;[a.x, b.x] = [b.x, a.x]
        ;[a.y, b.y] = [b.y, a.y]
        if (!insideBoard(a) || !insideBoard(b)) {
          ;[a.x, b.x] = [b.x, a.x]
          ;[a.y, b.y] = [b.y, a.y]
          continue
        }
        const nc = cost()
        if (nc < current || rng() < Math.exp((current - nc) / T)) {
          current = nc
        } else {
          ;[a.x, b.x] = [b.x, a.x]
          ;[a.y, b.y] = [b.y, a.y]
        }
      } else {
        // Rotation 90°
        const i = Math.floor(rng() * fps.length)
        const fp = fps[i]
        const oldRot = fp.rotation
        fp.rotation = (fp.rotation + 90) % 360
        if (!insideBoard(fp)) {
          fp.rotation = oldRot
          continue
        }
        const nc = cost()
        if (nc < current || rng() < Math.exp((current - nc) / T)) {
          current = nc
        } else {
          fp.rotation = oldRot
        }
      }

      if (current < best) {
        best = current
        bestSnapshot = fps.map((f) => ({ ...f, localPads: [...f.localPads] }))
      }
      if (it % 100 === 0) input.onIter?.(it, current, best)
    }

    // Restaure la meilleure solution et matérialise les pads absolus
    const result = input.footprints
    for (let i = 0; i < result.length; i++) {
      const src = bestSnapshot[i]
      const fp = result[i]
      fp.x = src.x
      fp.y = src.y
      fp.rotation = src.rotation
      fp.pads = src.localPads.map((lp) => {
        const w = padWorld(src, lp)
        return {
          number: lp.number,
          x: w.x,
          y: w.y,
          w: lp.w,
          h: lp.h,
          shape: fp.pads.find((p) => p.number === lp.number)?.shape ?? 'circle',
          layer: fp.pads.find((p) => p.number === lp.number)?.layer ?? 'F.Cu',
          netId: lp.netId,
        }
      })
    }

    input.onIter?.(iterations, best, best)
    return { footprints: result, initialCost, finalCost: best, iterations }
  }

  /** Range en lignes compactes les empreintes hors carte. */
  private normalize(fps: LightFp[], board: BoardSpec, grid: number) {
    const margin = 6
    let cursorX = margin
    let cursorY = margin
    let rowH = 0
    for (const fp of fps) {
      const half = Math.max(fp.bodyW, fp.bodyH) / 2
      const outside =
        fp.x < half || fp.y < half || fp.x > board.width - half || fp.y > board.height - half
      if (!outside) continue
      const w = Math.max(fp.bodyW, fp.bodyH) + 4
      if (cursorX + w > board.width - margin) {
        cursorX = margin
        cursorY += rowH + 4
        rowH = 0
      }
      fp.x = snap(cursorX + Math.max(fp.bodyW, fp.bodyH) / 2, grid)
      fp.y = snap(cursorY + Math.max(fp.bodyW, fp.bodyH) / 2, grid)
      fp.rotation = 0
      cursorX += w
      rowH = Math.max(rowH, Math.max(fp.bodyW, fp.bodyH))
    }
  }
}
