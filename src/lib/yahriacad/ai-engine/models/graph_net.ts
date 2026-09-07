/**
 * ai-engine/models/graph_net — Modèle graphe du PCB : ordering des nets,
 * arbre couvrant minimal (Prim) par net, RNG déterministe (mulberry32).
 * Analogue TypeScript de `graph_net.py` (le graphe de nets guide le routeur).
 */
import { Net, Point } from '../../shared/types'

/** RNG déterministe (reproductibilité des runs IA). */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** Arbre couvrant minimal (Prim) — ordonnance les pads d'un net. */
export function primMST(points: Point[]): [number, number][] {
  const n = points.length
  if (n <= 1) return []
  const inTree = new Array<boolean>(n).fill(false)
  const edges: [number, number][] = []
  inTree[0] = true
  for (let k = 1; k < n; k++) {
    let bestI = -1
    let bestJ = -1
    let bestD = Infinity
    for (let i = 0; i < n; i++) {
      if (!inTree[i]) continue
      for (let j = 0; j < n; j++) {
        if (inTree[j]) continue
        const d = Math.hypot(points[i].x - points[j].x, points[i].y - points[j].y)
        if (d < bestD) {
          bestD = d
          bestI = i
          bestJ = j
        }
      }
    }
    if (bestJ < 0) break
    inTree[bestJ] = true
    edges.push([bestI, bestJ])
  }
  return edges
}

export interface NetOrderEntry {
  netIdx: number
  priority: number
}

/**
 * Ordonnancement des nets : les nets compacts d'abord (diagonale de boîte
 * englobante croissante), égalité départagée par le nombre de pads.
 */
export function orderNets(netPadPositions: Point[][]): NetOrderEntry[] {
  return netPadPositions
    .map((pts, netIdx) => {
      if (pts.length === 0) return { netIdx, priority: Infinity }
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
      for (const p of pts) {
        minX = Math.min(minX, p.x)
        minY = Math.min(minY, p.y)
        maxX = Math.max(maxX, p.x)
        maxY = Math.max(maxY, p.y)
      }
      return { netIdx, priority: Math.hypot(maxX - minX, maxY - minY) + pts.length * 0.001 }
    })
    .sort((a, b) => a.priority - b.priority)
}

/** Statistiques par net (diagnostic congestion). */
export function netGraphStats(nets: Net[]): { avgFanout: number; maxFanout: number; twoPin: number } {
  let sum = 0
  let max = 0
  let twoPin = 0
  for (const n of nets) {
    const f = n.pins.length
    sum += f
    max = Math.max(max, f)
    if (f === 2) twoPin++
  }
  return { avgFanout: nets.length ? sum / nets.length : 0, maxFanout: max, twoPin }
}
