/**
 * ai-engine/agents/maze_router — Routeur automatique A* (maze routing)
 * multi-couches, 8-connexe, avec gestion des vias et rip-up & reroute.
 * Déterministe (RNG seedé) — analogue déployé des agents RL (`ppo_agent.py`).
 */
import { DesignRules, Layer, Net, Point, Track, Via } from '../../shared/types'
import { PcbEnvironment } from '../environment/pcb_env'
import { ACTIONS, COST, actionCost } from '../environment/action_space'
import { primMST, mulberry32 } from '../models/graph_net'
import { simplifyPolyline } from '../../pkg/geometry'
import { uid } from '../../pkg/utils'
import { BaseAgent, ProgressFn } from './base_agent'

export interface Cell3 {
  c: number
  r: number
  layer: number
}

export interface RouteNetResult {
  netIdx: number
  netId: string
  success: boolean
  connectedPads: number
  totalPads: number
  tracks: Track[]
  vias: Via[]
  cells: Cell3[]
}

export interface RouterInput {
  env: PcbEnvironment
  nets: Net[]
  netOrder: number[]
  rules: DesignRules
  viaCost: number
  maxPasses: number
  allowRipup: boolean
  shouldCancel?: () => boolean
  onNetDone?: (netIdx: number, done: number, total: number, ok: boolean, pass: number) => void
}

export interface RouterOutput {
  byNet: Map<number, RouteNetResult>
  routedCount: number
  totalCount: number
  tracks: Track[]
  vias: Via[]
  passes: number
}

const DIRS = 4
const VIA_ACT = ACTIONS.length - 1

/** Tas binaire minimal (sur f). */
class MinHeap {
  private a: { key: number; f: number }[] = []
  get size() {
    return this.a.length
  }
  push(n: { key: number; f: number }) {
    const a = this.a
    a.push(n)
    let i = a.length - 1
    while (i > 0) {
      const p = (i - 1) >> 1
      if (a[p].f <= a[i].f) break
      ;[a[p], a[i]] = [a[i], a[p]]
      i = p
    }
  }
  pop(): { key: number; f: number } | undefined {
    const a = this.a
    if (a.length === 0) return undefined
    const top = a[0]
    const last = a.pop()!
    if (a.length > 0) {
      a[0] = last
      let i = 0
      for (;;) {
        const l = i * 2 + 1
        const r = l + 1
        let m = i
        if (l < a.length && a[l].f < a[m].f) m = l
        if (r < a.length && a[r].f < a[m].f) m = r
        if (m === i) break
        ;[a[m], a[i]] = [a[i], a[m]]
        i = m
      }
    }
    return top
  }
}

interface AStarResult {
  cells: Cell3[]
  viaSpots: Point[]
}

/**
 * Agent de routage — A* par connexion, arbre couvrant par net,
 * passes de rip-up & reroute globales tant que des nets échouent.
 */
export class RouterAgent extends BaseAgent<RouterInput, RouterOutput> {
  readonly name = 'MazeRouter-A*'
  readonly strategy =
    'A* 8-connexe 2 couches, arbre couvrant minimal par net, rip-up & reroute multi-passes'

  private gScore!: Float64Array
  private prev!: Int32Array
  private closed!: Uint8Array
  private heap = new MinHeap()

  withProgressFn(fn: ProgressFn): this {
    return this.withProgress(fn)
  }

  async run(input: RouterInput): Promise<RouterOutput> {
    const { env, nets, rules } = input
    const stateSize = env.cols * env.rows * 2 * (DIRS + 1)
    this.gScore = new Float64Array(stateSize)
    this.prev = new Int32Array(stateSize)
    this.closed = new Uint8Array(stateSize)

    let best: RouterOutput | null = null
    let viaCost = input.viaCost
    let rng = mulberry32(42)

    for (let pass = 1; pass <= Math.max(1, input.maxPasses); pass++) {
      // Passe ≥ 2 : libère TOUTES les cellules revendiquées à la passe
      // précédente (y compris les routages partiels des nets échoués).
      if (best) {
        for (const [netIdx] of best.byNet) env.releaseNet(netIdx)
      }

      const byNet = new Map<number, RouteNetResult>()
      let routed = 0
      const order = pass === 1 ? input.netOrder : this.shuffle(input.netOrder, rng)

      for (let i = 0; i < order.length; i++) {
        if (input.shouldCancel?.()) break
        const netIdx = order[i]
        const net = nets[netIdx]
        const res = this.routeNet(env, net, netIdx, rules, viaCost)
        byNet.set(netIdx, res)
        if (res.success) routed++
        input.onNetDone?.(netIdx, routed, order.length, res.success, pass)
      }

      const output: RouterOutput = {
        byNet,
        routedCount: routed,
        totalCount: order.length,
        tracks: [...byNet.values()].flatMap((r) => r.tracks),
        vias: [...byNet.values()].flatMap((r) => r.vias),
        passes: pass,
      }
      if (!best || routed > best.routedCount) best = output
      if (routed === order.length) break
      if (!input.allowRipup) break
      // Passe suivante : vias moins chers (plus de liberté) + ordre aléatoire
      viaCost = Math.max(2, viaCost * 0.5)
      rng = mulberry32(42 + pass * 1000)
    }

    return best!
  }

  private shuffle<T>(arr: T[], rng: () => number): T[] {
    const a = [...arr]
    for (let i = a.length - 1; i > 0; i--) {
      const j = Math.floor(rng() * (i + 1))
      ;[a[i], a[j]] = [a[j], a[i]]
    }
    return a
  }

  /** Route un net complet (arbre couvrant : connecte les pads 2 à 2). */
  routeNet(
    env: PcbEnvironment,
    net: Net,
    netIdx: number,
    rules: DesignRules,
    viaCost: number,
  ): RouteNetResult {
    const padGroups = env.padCells.get(netIdx) ?? []
    const centers = env.padCenters.get(netIdx) ?? []
    const empty: RouteNetResult = {
      netIdx, netId: net.id, success: padGroups.length <= 1, connectedPads: Math.min(1, padGroups.length),
      totalPads: padGroups.length, tracks: [], vias: [], cells: [],
    }
    if (padGroups.length <= 1) return empty

    const mst = primMST(centers)
    const connected = new Set<number>([0])
    const tracks: Track[] = []
    const vias: Via[] = []
    const allCells: Cell3[] = []

    for (const [i, j] of mst) {
      // Prim produit les arêtes arbre → nouveau nœud, mais par prudence on
      // oriente toujours depuis le côté déjà connecté.
      const src = connected.has(i) ? i : j
      const dst = src === i ? j : i
      if (connected.has(src) && connected.has(dst)) continue
      const path = this.astar(env, netIdx, padGroups[src], padGroups[dst], viaCost)
      if (!path) {
        return {
          netIdx, netId: net.id, success: false,
          connectedPads: connected.size, totalPads: padGroups.length,
          tracks, vias, cells: allCells,
        }
      }
      // Géométrie : pistes par couche + vias aux changements de couche
      this.claimPath(env, netIdx, path.cells, path.viaSpots, rules)
      const geo = this.cellsToGeometry(env, net, netIdx, path, padGroups[src], padGroups[dst], rules)
      tracks.push(...geo.tracks)
      vias.push(...geo.vias)
      allCells.push(...path.cells)
      connected.add(dst)
    }

    return {
      netIdx, netId: net.id, success: true,
      connectedPads: padGroups.length, totalPads: padGroups.length,
      tracks, vias, cells: allCells,
    }
  }

  private passable(env: PcbEnvironment, layer: number, c: number, r: number, netIdx: number): boolean {
    const v = env.occAt(layer, c, r)
    return v === 0 || v === PcbEnvironment.NET_BASE + netIdx
  }

  /** A* multi-source (cellules du pad source) → cellules du pad cible (8-connexe + vias). */
  private astar(
    env: PcbEnvironment,
    netIdx: number,
    startCells: number[][],
    targetCells: number[][],
    viaCost: number,
  ): AStarResult | null {
    const { cols, rows, cell } = env.spec
    const cellKey = (c: number, r: number, l: number) => l * cols * rows + r * cols + c
    const stateKey = (cell: number, dir: number) => cell * (DIRS + 1) + dir

    const targets = new Set<number>()
    for (const [c, r, l] of targetCells) targets.add(cellKey(c, r, l))
    const targetPts = targetCells.map(([c, r]) => env.toPoint(c, r))

    this.gScore.fill(Infinity)
    this.closed.fill(0)
    this.heap = new MinHeap()

    const own = PcbEnvironment.NET_BASE + netIdx
    // Heuristique admissible 4-connexe : distance de Manhattan (en cellules)
    const hCells = (c: number, r: number): number => {
      let best = Infinity
      for (const t of targetPts) {
        const dx = Math.abs(c - (t.x - env.spec.originX) / cell)
        const dy = Math.abs(r - (t.y - env.spec.originY) / cell)
        best = Math.min(best, dx + dy)
      }
      return best
    }

    for (const [c, r, l] of startCells) {
      const ck = cellKey(c, r, l)
      const sk = stateKey(ck, DIRS) // dir "aucune"
      this.gScore[sk] = 0
      this.prev[sk] = -1
      this.heap.push({ key: sk, f: hCells(c, r) })
    }

    while (this.heap.size > 0) {
      const node = this.heap.pop()!
      const sk = node.key
      if (this.closed[sk]) continue
      this.closed[sk] = 1
      const cell = Math.floor(sk / (DIRS + 1))
      const dir = sk % (DIRS + 1)
      if (targets.has(cell)) return this.reconstruct(env, sk, cellKey)

      const l = Math.floor(cell / (cols * rows))
      const rem = cell - l * cols * rows
      const r = Math.floor(rem / cols)
      const c = rem - r * cols

      for (let a = 0; a < ACTIONS.length; a++) {
        const act = ACTIONS[a]
        if (act.via) {
          const nl = 1 - l
          const ck = cellKey(c, r, nl)
          if (!this.passable(env, nl, c, r, netIdx)) continue
          const center = env.toPoint(c, r)
          if (!env.viaFits(center.x, center.y, netIdx)) continue
          const nsk = stateKey(ck, dir === DIRS ? DIRS : dir)
          const ng = this.gScore[sk] + viaCost
          if (ng < this.gScore[nsk] - 1e-9) {
            this.gScore[nsk] = ng
            this.prev[nsk] = sk
            this.heap.push({ key: nsk, f: ng + hCells(c, r) })
          }
          continue
        }
        const nc = c + act.dx
        const nr = r + act.dy
        if (!env.inBounds(nc, nr)) continue
        if (!this.passable(env, l, nc, nr, netIdx)) continue
        // Diagonale : les deux cases adjacentes doivent être franchissables
        if (act.dx !== 0 && act.dy !== 0) {
          const v1 = env.occAt(l, c + act.dx, r)
          const v2 = env.occAt(l, c, r + act.dy)
          const ok1 = v1 === 0 || v1 === own
          const ok2 = v2 === 0 || v2 === own
          if (!ok1 || !ok2) continue
        }
        const turn = dir !== DIRS && dir !== a ? COST.turn : 0
        const ng = this.gScore[sk] + actionCost(act) + turn
        const ck = cellKey(nc, nr, l)
        const nsk = stateKey(ck, a)
        if (ng < this.gScore[nsk] - 1e-9) {
          this.gScore[nsk] = ng
          this.prev[nsk] = sk
          this.heap.push({ key: nsk, f: ng + hCells(nc, nr) })
        }
      }
    }
    return null
  }

  private reconstruct(
    env: PcbEnvironment,
    endKey: number,
    cellKey: (c: number, r: number, l: number) => number,
  ): AStarResult {
    const keys: number[] = []
    let k = endKey
    while (k >= 0) {
      keys.push(k)
      k = this.prev[k]
      if (keys.length > 1e6) break
    }
    keys.reverse()
    const cells: Cell3[] = []
    const viaSpots: Point[] = []
    const { cols, rows } = env.spec
    let lastLayer = -1
    for (const sk of keys) {
      const cell = Math.floor(sk / (DIRS + 1))
      const l = Math.floor(cell / (cols * rows))
      const rem = cell - l * cols * rows
      const r = Math.floor(rem / cols)
      const c = rem - r * cols
      if (lastLayer >= 0 && l !== lastLayer) {
        viaSpots.push(env.toPoint(c, r))
      }
      lastLayer = l
      cells.push({ c, r, layer: l })
    }
    return { cells, viaSpots }
  }

  /** Marque l'occupation du chemin (cellules + vias) une fois le routage accepté. */
  private claimPath(env: PcbEnvironment, netIdx: number, cells: Cell3[], viaSpots: Point[], rules: DesignRules) {
    for (const cell of cells) {
      if (env.occAt(cell.layer, cell.c, cell.r) === 0) env.claim(netIdx, cell.layer, cell.c, cell.r)
    }
    for (const p of viaSpots) env.stampVia(p.x, p.y, netIdx)
  }

  /** Convertit un chemin de cellules en pistes + vias géométriques. */
  private cellsToGeometry(
    env: PcbEnvironment,
    net: Net,
    netIdx: number,
    path: AStarResult,
    srcPad: number[][],
    dstPad: number[][],
    rules: DesignRules,
  ): { tracks: Track[]; vias: Via[] } {
    const { cell } = env.spec
    const LAYERS: Layer[] = ['F.Cu', 'B.Cu']
    const tracks: Track[] = []
    const vias: Via[] = []

    // Points d'amarrage exacts : centres des pads source / cible
    const srcCenter = env.toPoint(srcPad[Math.floor(srcPad.length / 2)][0], srcPad[Math.floor(srcPad.length / 2)][1])
    const dstCenter = env.toPoint(dstPad[Math.floor(dstPad.length / 2)][0], dstPad[Math.floor(dstPad.length / 2)][1])

    // Segments par couche (les changements de couche coïncident avec les viaSpots)
    const segs: { layer: number; pts: Point[] }[] = []
    for (const cl of path.cells) {
      const p = env.toPoint(cl.c, cl.r)
      const last = segs[segs.length - 1]
      if (!last || last.layer !== cl.layer) segs.push({ layer: cl.layer, pts: [p] })
      else last.pts.push(p)
    }
    if (segs.length > 0) {
      segs[0].pts.unshift(srcCenter)
      segs[segs.length - 1].pts.push(dstCenter)
    }

    for (const seg of segs) {
      const pts = simplifyPolyline(seg.pts)
      if (pts.length >= 2) {
        tracks.push({ id: uid('t'), netId: net.id, layer: LAYERS[seg.layer], width: rules.trackWidth, pts })
      }
    }
    for (const p of path.viaSpots) {
      vias.push({ id: uid('v'), netId: net.id, x: p.x, y: p.y, drill: rules.viaDrill, diameter: rules.viaDiameter })
    }
    return { tracks, vias }
  }
}
