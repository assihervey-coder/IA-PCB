/**
 * YahriaCad — Microservice IA (socket.io, port 3010)
 * Équivalent du service gRPC `ai-engine/cmd/ai-server/main.py` :
 * expose les tâches placement / routage / optimisation avec diffusion
 * temps réel de la progression.
 *
 * Communication : socket.io via la gateway Caddy (path "/", port 3010).
 * Le client envoie le design complet (service sans état) ; le service
 * exécute le moteur IA (TypeScript pur) et renvoie le design muté + stats.
 */
import { createServer } from 'http'
import { Server } from 'socket.io'
import {
  AiPlaceOptions,
  AiRouteOptions,
  Design,
} from '../../src/lib/yahriacad/shared/types'
import {
  runOptimization,
  runPlacement,
  runRouting,
} from '../../src/lib/yahriacad/ai-engine'

const PORT = Number(process.env.AI_ENGINE_PORT ?? 3010)

const httpServer = createServer()
const io = new Server(httpServer, {
  // NE PAS changer le path : utilisé par la gateway Caddy
  path: '/',
  cors: { origin: '*', methods: ['GET', 'POST'] },
  pingTimeout: 60000,
  pingInterval: 25000,
})

type Progress = (stage: string, progress: number, message: string) => void

const clone = (d: Design): Design => structuredClone(d)

io.on('connection', (socket) => {
  console.log(`[ai-engine] Client connecté : ${socket.id}`)

  socket.emit('ai:hello', {
    agent: 'YahriaCad-Engine v1',
    tasks: ['place', 'route', 'optimize'],
    algorithms: {
      place: 'Recuit simulé (HPWL + anti-chevauchement)',
      route: 'A* maze multi-couches + rip-up & reroute',
      optimize: 'Re-routage à coût de via élevé',
    },
  })

  socket.on('ping-ai', (cb: (t: number) => void) => {
    if (typeof cb === 'function') cb(Date.now())
  })

  socket.on('ai:place', async (payload: { design: Design; options?: AiPlaceOptions }) => {
    try {
      if (!payload?.design) throw new Error('design manquant')
      const design = clone(payload.design)
      const progress: Progress = (stage, p, message) =>
        socket.emit('ai:progress', { stage, progress: p, message })
      const stats = await runPlacement(design, payload.options ?? {}, progress)
      socket.emit('ai:result', { task: 'place', design, stats })
      console.log(`[ai-engine] place terminé (${stats.durationMs} ms)`)
    } catch (e) {
      socket.emit('ai:error', { message: e instanceof Error ? e.message : String(e) })
    }
  })

  socket.on('ai:route', async (payload: { design: Design; options?: AiRouteOptions }) => {
    try {
      if (!payload?.design) throw new Error('design manquant')
      const design = clone(payload.design)
      const progress: Progress = (stage, p, message) =>
        socket.emit('ai:progress', { stage, progress: p, message })
      const stats = await runRouting(design, payload.options ?? {}, progress)
      socket.emit('ai:result', { task: 'route', design, stats })
      console.log(`[ai-engine] route terminé : ${stats.routedNets}/${stats.totalNets} nets (${stats.durationMs} ms)`)
    } catch (e) {
      socket.emit('ai:error', { message: e instanceof Error ? e.message : String(e) })
    }
  })

  socket.on('ai:optimize', async (payload: { design: Design; options?: AiRouteOptions }) => {
    try {
      if (!payload?.design) throw new Error('design manquant')
      const design = clone(payload.design)
      const progress: Progress = (stage, p, message) =>
        socket.emit('ai:progress', { stage, progress: p, message })
      const stats = await runOptimization(design, payload.options ?? {}, progress)
      socket.emit('ai:result', { task: 'optimize', design, stats })
      console.log(`[ai-engine] optimize terminé : ${stats.vias} vias (${stats.durationMs} ms)`)
    } catch (e) {
      socket.emit('ai:error', { message: e instanceof Error ? e.message : String(e) })
    }
  })

  socket.on('disconnect', () => {
    console.log(`[ai-engine] Client déconnecté : ${socket.id}`)
  })
})

httpServer.listen(PORT, () => {
  console.log(`[ai-engine] YahriaCad moteur IA prêt sur le port ${PORT}`)
})
