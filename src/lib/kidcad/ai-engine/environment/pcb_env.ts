/**
 * ai-engine/environment/pcb_env — Environnement de routage (grille discrète).
 * Analogue TypeScript du simulateur `pcb_env.py` : grille 2 couches,
 * occupation par net, obstacles (pads, pistes, vias, bord de carte).
 * Pure TypeScript, sans dépendance Next — partagé avec le mini-service IA.
 */
import { DesignRules, FootprintInst, Layer, Net, Point } from '../../shared/types'
import { PlacedPad } from '../../shared/types'

export const LAYERS: Layer[] = ['F.Cu', 'B.Cu']

export interface GridSpec {
  cols: number
  rows: number
  cell: number // pas de grille (mm)
  originX: number
  originY: number
}

/**
 * Environnement PCB discrétisé.
 * occupancy[l][idx] : 0 = libre ; NET_BASE + netIdx = cellule appartenant au net (atterrissable
 * par ce net uniquement) ; OBSTACLE = interdit à tous.
 */
export class PcbEnvironment {
  readonly spec: GridSpec
  readonly rules: DesignRules
  occupancy: Int32Array[] = []
  /** cellules des pads par net → pad → liste [c, r, layer] */
  padCells: Map<number, number[][][]> = new Map()
  /** centres réels des pads par net → pad (aligné avec padCells) */
  padCenters: Map<number, Point[]> = new Map()

  static readonly NET_BASE = 10 // 1..9 réservés
  static readonly OBSTACLE = -1

  constructor(board: { width: number; height: number }, rules: DesignRules) {
    this.rules = rules
    const cell = Math.max(0.2, rules.gridStep)
    const margin = rules.edgeClearance + rules.trackWidth / 2 + cell / 2
    const cols = Math.max(4, Math.floor((board.width - 2 * margin) / cell) + 1)
    const rows = Math.max(4, Math.floor((board.height - 2 * margin) / cell) + 1)
    this.spec = { cols, rows, cell, originX: margin, originY: margin }
    for (let l = 0; l < LAYERS.length; l++) {
      this.occupancy.push(new Int32Array(cols * rows))
    }
    this.blockEdges(board, margin)
  }

  get cols() { return this.spec.cols }
  get rows() { return this.spec.rows }
  get cell() { return this.spec.cell }

  /** Bord de carte : bande interdite autour de la zone utile. */
  private blockEdges(board: { width: number; height: number }, margin: number) {
    for (let l = 0; l < LAYERS.length; l++) {
      const occ = this.occupancy[l]
      for (let r = 0; r < this.rows; r++) {
        for (let c = 0; c < this.cols; c++) {
          const p = this.toPoint(c, r)
          if (
            p.x < margin - 1e-9 || p.y < margin - 1e-9 ||
            p.x > board.width - margin + 1e-9 || p.y > board.height - margin + 1e-9
          ) {
            occ[r * this.cols + c] = PcbEnvironment.OBSTACLE
          }
        }
      }
    }
  }

  inBounds(c: number, r: number): boolean {
    return c >= 0 && r >= 0 && c < this.cols && r < this.rows
  }

  idx(c: number, r: number, layer: number): number {
    return layer * this.cols * this.rows + r * this.cols + c
  }

  toCell(p: Point): { c: number; r: number } {
    return {
      c: Math.round((p.x - this.spec.originX) / this.cell),
      r: Math.round((p.y - this.spec.originY) / this.cell),
    }
  }

  toPoint(c: number, r: number): Point {
    return { x: this.spec.originX + c * this.cell, y: this.spec.originY + r * this.cell }
  }

  occAt(layer: number, c: number, r: number): number {
    if (!this.inBounds(c, r)) return PcbEnvironment.OBSTACLE
    return this.occupancy[layer][r * this.cols + c]
  }

  netCodeAt(layer: number, c: number, r: number): number {
    const v = this.occAt(layer, c, r)
    return v >= PcbEnvironment.NET_BASE ? v - PcbEnvironment.NET_BASE : -1
  }

  setOcc(layer: number, c: number, r: number, value: number) {
    if (!this.inBounds(c, r)) return
    this.occupancy[layer][r * this.cols + c] = value
  }

  /** Occupe une cellule pour un net (après routage). */
  claim(netIdx: number, layer: number, c: number, r: number) {
    this.setOcc(layer, c, r, PcbEnvironment.NET_BASE + netIdx)
  }

  /** Libère toutes les cellules d'un net (rip-up). */
  releaseNet(netIdx: number) {
    const code = PcbEnvironment.NET_BASE + netIdx
    for (let l = 0; l < this.occupancy.length; l++) {
      const occ = this.occupancy[l]
      for (let i = 0; i < occ.length; i++) if (occ[i] === code) occ[i] = 0
    }
  }

  /** Marque les obstacles : empreintes (pads + corps) pour tous les nets. */
  stampFootprints(footprints: FootprintInst[], nets: Net[]) {
    const netIdxById = new Map<string, number>()
    nets.forEach((net, idx) => netIdxById.set(net.id, idx))
    const clearance = this.rules.minClearance
    for (const fp of footprints) {
      // Corps : routage au-dessus autorisé (2 couches classique), seuls les pads bloquent.
      for (const pad of fp.pads) {
        const netIdx = pad.netId ? netIdxById.get(pad.netId) ?? -1 : -1
        this.stampPad(pad, netIdx, clearance)
      }
    }
  }

  /** Marque un pad : cellules sous le pad → net ; anneau autour → obstacle.
   * Convention bibliothèque : pad sur B.Cu = traversant (THT) → les DEUX couches. */
  private stampPad(pad: PlacedPad, netIdx: number, clearance: number) {
    const isTHT = pad.layer === 'B.Cu'
    const layers = isTHT ? [0, 1] : [LAYERS.indexOf(pad.layer)]
    const rx = pad.w / 2
    const ry = pad.h / 2
    // Halo : isolement + demi-largeur de piste (garantie DRC tangentielle)
    const halo = clearance + this.rules.trackWidth / 2
    const c0r = this.toCell({ x: pad.x - rx - halo, y: pad.y - ry - halo })
    const c1r = this.toCell({ x: pad.x + rx + halo, y: pad.y + ry + halo })
    const cells: number[][] = []
    for (const layer of layers) {
      for (let r = c0r.r; r <= c1r.r; r++) {
        for (let c = c0r.c; c <= c1r.c; c++) {
          if (!this.inBounds(c, r)) continue
          const center = this.toPoint(c, r)
          const dx = Math.max(0, Math.abs(center.x - pad.x) - rx)
          const dy = Math.max(0, Math.abs(center.y - pad.y) - ry)
          const inside = dx === 0 && dy === 0
          const ring = Math.hypot(dx, dy) <= halo + 1e-6
          const i = r * this.cols + c
          if (inside) {
            if (netIdx >= 0) {
              this.occupancy[layer][i] = PcbEnvironment.NET_BASE + netIdx
              cells.push([c, r, layer])
            } else {
              this.occupancy[layer][i] = PcbEnvironment.OBSTACLE
            }
          } else if (ring && this.occupancy[layer][i] === 0) {
            this.occupancy[layer][i] = PcbEnvironment.OBSTACLE
          }
        }
      }
    }
    if (netIdx >= 0) {
      if (!this.padCells.has(netIdx)) this.padCells.set(netIdx, [])
      if (cells.length > 0) this.padCells.get(netIdx)!.push(cells)
      if (!this.padCenters.has(netIdx)) this.padCenters.set(netIdx, [])
      this.padCenters.get(netIdx)!.push({ x: pad.x, y: pad.y })
    }
  }

  /** Marque un via (obstacle anneau, cellule centrale = net). */
  stampVia(x: number, y: number, netIdx: number) {
    const { rules } = this
    const rad = rules.viaDiameter / 2 + rules.minClearance + rules.trackWidth / 2
    const c0 = this.toCell({ x: x - rad, y: y - rad })
    const c1 = this.toCell({ x: x + rad, y: y + rad })
    for (let l = 0; l < LAYERS.length; l++) {
      for (let r = c0.r; r <= c1.r; r++) {
        for (let c = c0.c; c <= c1.c; c++) {
          if (!this.inBounds(c, r)) continue
          const center = this.toPoint(c, r)
          const d = Math.hypot(center.x - x, center.y - y)
          const i = r * this.cols + c
          if (d <= rules.viaDiameter / 2 + 1e-6) {
            this.occupancy[l][i] = netIdx >= 0 ? PcbEnvironment.NET_BASE + netIdx : PcbEnvironment.OBSTACLE
          } else if (d <= rad && this.occupancy[l][i] === 0) {
            this.occupancy[l][i] = PcbEnvironment.OBSTACLE
          }
        }
      }
    }
  }

  /** Un via peut-il être posé ici sans empiéter sur un autre net ? */
  viaFits(x: number, y: number, netIdx: number): boolean {
    const { rules } = this
    const rad = rules.viaDiameter / 2 + rules.minClearance + rules.trackWidth / 2
    const own = PcbEnvironment.NET_BASE + netIdx
    const c0 = this.toCell({ x: x - rad, y: y - rad })
    const c1 = this.toCell({ x: x + rad, y: y + rad })
    for (let l = 0; l < LAYERS.length; l++) {
      for (let r = c0.r; r <= c1.r; r++) {
        for (let c = c0.c; c <= c1.c; c++) {
          if (!this.inBounds(c, r)) return false
          const center = this.toPoint(c, r)
          const d = Math.hypot(center.x - x, center.y - y)
          const v = this.occupancy[l][r * this.cols + c]
          const allowed = v === 0 || v === own
          if (d <= rules.viaDiameter / 2 + 1e-6) {
            if (!allowed) return false
          } else if (d <= rad + 1e-6 && !allowed) return false
        }
      }
    }
    return true
  }

  /** Libère les cellules occupées par la piste d'un net (path cells). */
  releasePathCells(cells: { c: number; r: number; layer: number }[], netIdx: number) {
    const code = PcbEnvironment.NET_BASE + netIdx
    for (const cell of cells) {
      const i = cell.r * this.cols + cell.c
      if (this.occupancy[cell.layer][i] === code) this.occupancy[cell.layer][i] = 0
    }
  }
}

/** Index : "footprintId:padNumber" → index de net. */
export function buildNetIndex(nets: Net[]): Map<string, number> {
  const m = new Map<string, number>()
  nets.forEach((net, idx) => {
    for (const pin of net.pins) m.set(`${pin.componentId}:${pin.pad}`, idx)
  })
  return m
}

/** Index : footprintId → composantId (lien schéma ↔ layout). */
export function componentIndex(footprints: FootprintInst[]): Map<string, string> {
  const m = new Map<string, string>()
  for (const fp of footprints) m.set(fp.id, fp.componentId)
  return m
}
