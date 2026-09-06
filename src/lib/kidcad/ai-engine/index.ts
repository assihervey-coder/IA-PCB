/**
 * ai-engine/index — Orchestrateur du moteur d'inférence IA.
 * Points d'entrée : runPlacement, runRouting, runOptimization.
 * Utilisé par l'API Next.js ET par le mini-service socket.io (port 3010).
 */
import {
  AiPlaceOptions,
  AiResultStats,
  AiRouteOptions,
  Design,
  Net,
  Point,
} from '../shared/types'
import { ConstraintSet } from '../domain/constraints/constraint_set'
import { extractNetlist } from '../domain/schematic/schematic'
import { totalWireLength } from '../domain/layout/layout'
import { PcbEnvironment } from './environment/pcb_env'
import { COST } from './environment/action_space'
import { orderNets } from './models/graph_net'
import { RouterAgent } from './agents/maze_router'
import { PlaceAgent } from './agents/sa_placer'

export { PcbEnvironment } from './environment/pcb_env'
export { RouterAgent } from './agents/maze_router'
export { PlaceAgent } from './agents/sa_placer'

/** Options par défaut du placement IA. */
const PLACE_ITERATIONS = 3500

/**
 * Synchronise la netlist du design : dérivée du schéma s'il contient des
 * composants, sinon conserve la netlist importée. Assigne les netId aux pads.
 */
export function syncNetlist(design: Design): Net[] {
  let nets: Net[]
  if (design.schematic.components.length > 0) {
    const extracted = extractNetlist(design.schematic)
    // Le schéma ne remplace la netlist que s'il produit des nets (fils posés)
    // ou si aucune netlist n'existe — sinon la netlist importée reste source.
    if (extracted.nets.length > 0 || !design.nets || design.nets.length === 0) {
      nets = extracted.nets
    } else {
      nets = design.nets
    }
  } else {
    nets = design.nets ?? []
  }
  for (const fp of design.layout.footprints) {
    for (const pad of fp.pads) pad.netId = null
  }
  const fpByComp = new Map<string, (typeof design.layout.footprints)[number]>()
  for (const fp of design.layout.footprints) fpByComp.set(fp.componentId, fp)
  for (const net of nets) {
    for (const pin of net.pins) {
      const fp = fpByComp.get(pin.componentId)
      if (!fp) continue
      const pad = fp.pads.find((p) => p.number === pin.pad)
      if (pad) pad.netId = net.id
    }
  }
  design.nets = nets
  return nets
}

/** Positions de pads par net (pour l'ordonnancement et le ratsnest). */
export function netPadPositions(design: Design, nets: Net[]): Point[][] {
  const fpByComp = new Map<string, (typeof design.layout.footprints)[number]>()
  for (const fp of design.layout.footprints) fpByComp.set(fp.componentId, fp)
  return nets.map((net) => {
    const pts: Point[] = []
    for (const pin of net.pins) {
      const fp = fpByComp.get(pin.componentId)
      const pad = fp?.pads.find((p) => p.number === pin.pad)
      if (pad) pts.push({ x: pad.x, y: pad.y })
    }
    return pts
  })
}

/** Statistiques d'un design. */
export function designStats(design: Design): AiResultStats & { components: number } {
  const nets = design.nets ?? []
  const routed = new Set(design.layout.tracks.map((t) => t.netId))
  return {
    components: design.layout.footprints.length,
    nets: nets.length,
    tracks: design.layout.tracks.length,
    vias: design.layout.vias.length,
    routedNets: routed.size,
    totalNets: nets.length,
    wireLength: totalWireLength(design.layout.tracks),
    durationMs: 0,
    passes: 0,
  }
}

/**
 * Placement automatique (recuit simulé). Retourne les stats ; le design est
 * muté (footprints repositionnés, pistes/vias effacées).
 */
export async function runPlacement(
  design: Design,
  options: AiPlaceOptions = {},
  onProgress?: (stage: string, progress: number, message: string) => void,
): Promise<AiResultStats> {
  const started = Date.now()
  const nets = syncNetlist(design)
  if (design.layout.footprints.length === 0) {
    throw new Error('Aucune empreinte dans le layout — placez des composants ou importez une netlist.')
  }
  const rules = new ConstraintSet(design.rules).value
  const agent = new PlaceAgent()
  agent.withProgress((stage, p, msg) => onProgress?.(stage, p, msg))

  onProgress?.('placement', 0, 'Recuit simulé : normalisation initiale…')
  const result = await agent.run({
    footprints: design.layout.footprints,
    nets,
    board: design.board,
    rules,
    iterations: options.iterations ?? PLACE_ITERATIONS,
  })

  // Le placement invalide le routage existant
  design.layout.tracks = []
  design.layout.vias = []

  onProgress?.('placement', 100, `Recuit terminé — longueur nets : ${Math.round(result.finalCost)} mm`)
  return {
    routedNets: 0,
    totalNets: nets.length,
    tracks: 0,
    vias: 0,
    wireLength: 0,
    durationMs: Date.now() - started,
    passes: 1,
  }
}

/**
 * Routage automatique (A* maze, multi-passes rip-up & reroute).
 * Le design est muté (tracks + vias remplacés).
 */
export async function runRouting(
  design: Design,
  options: AiRouteOptions = {},
  onProgress?: (stage: string, progress: number, message: string) => void,
): Promise<AiResultStats> {
  const started = Date.now()
  const nets = syncNetlist(design)
  const rules = new ConstraintSet(design.rules).value
  const env = new PcbEnvironment(design.board, rules)
  env.stampFootprints(design.layout.footprints, nets)

  const positions = netPadPositions(design, nets)
  const order = orderNets(positions).map((e) => e.netIdx)

  onProgress?.('routage', 0, `Grille ${env.cols}×${env.rows} ×2 couches — ${order.length} nets à router`)

  const router = new RouterAgent()
  const output = await router.run({
    env,
    nets,
    netOrder: order,
    rules,
    viaCost: options.viaCost ?? COST.via,
    maxPasses: options.maxPasses ?? 3,
    allowRipup: options.allowRipup ?? true,
    onNetDone: (netIdx, routed, total, ok, pass) => {
      onProgress?.(
        'routage',
        Math.round((routed / Math.max(1, total)) * 100),
        `Passe ${pass} — ${nets[netIdx]?.name ?? '?'} : ${ok ? 'routé' : 'ÉCHEC'} (${routed}/${total})`,
      )
    },
  })

  design.layout.tracks = output.tracks
  design.layout.vias = output.vias

  const wireLength = totalWireLength(output.tracks)
  onProgress?.(
    'routage',
    100,
    `Terminé : ${output.routedCount}/${output.totalNets} nets, ${output.vias.length} vias, ${wireLength.toFixed(0)} mm de cuivre`,
  )
  return {
    routedNets: output.routedCount,
    totalNets: output.totalCount,
    tracks: output.tracks.length,
    vias: output.vias.length,
    wireLength,
    durationMs: Date.now() - started,
    passes: output.passes,
  }
}

/**
 * Optimisation post-routage : re-route l'ensemble avec un coût de via élevé
 * afin de réduire le nombre de vias ; le résultat n'est adopté que s'il ne
 * dégrade ni la complétion ni le nombre de vias.
 */
export async function runOptimization(
  design: Design,
  options: AiRouteOptions = {},
  onProgress?: (stage: string, progress: number, message: string) => void,
): Promise<AiResultStats> {
  const started = Date.now()
  const nets = syncNetlist(design)
  const rules = new ConstraintSet(design.rules).value
  const beforeVias = design.layout.vias.length
  const beforeRouted = new Set(design.layout.tracks.map((t) => t.netId)).size

  const env = new PcbEnvironment(design.board, rules)
  env.stampFootprints(design.layout.footprints, nets)
  const positions = netPadPositions(design, nets)
  const order = orderNets(positions).map((e) => e.netIdx)

  onProgress?.('optimisation', 0, `Optimisation vias — ${beforeVias} vias avant optimisation`)

  const router = new RouterAgent()
  const output = await router.run({
    env,
    nets,
    netOrder: order,
    rules,
    viaCost: 50,
    maxPasses: options.maxPasses ?? 2,
    allowRipup: false,
    onNetDone: (_n, routed, total) => {
      onProgress?.('optimisation', Math.round((routed / Math.max(1, total)) * 100), `Re-routage à faible coût de via… (${routed}/${total})`)
    },
  })

  const afterVias = output.vias.length
  const afterRouted = output.routedCount
  if (afterRouted >= beforeRouted && afterVias <= beforeVias) {
    design.layout.tracks = output.tracks
    design.layout.vias = output.vias
    onProgress?.('optimisation', 100, `Adopté : ${beforeVias} → ${afterVias} vias`)
  } else {
    onProgress?.('optimisation', 100, `Non adopté (${beforeVias} → ${afterVias} vias, routage ${beforeRouted} → ${afterRouted})`)
  }

  const wireLength = totalWireLength(design.layout.tracks)
  return {
    routedNets: Math.max(afterRouted, beforeRouted),
    totalNets: nets.length,
    tracks: design.layout.tracks.length,
    vias: design.layout.vias.length,
    wireLength,
    durationMs: Date.now() - started,
    passes: output.passes,
  }
}
