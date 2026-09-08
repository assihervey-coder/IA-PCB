/**
 * client/stores/ai-store — Pilotage des jobs IA (socket.io) et journal temps réel.
 */
import { create } from 'zustand'
import type { Socket } from 'socket.io-client'
import { AiResultStats, AiTask, Design } from '../../shared/types'
import { waitForAiSocket } from '../socket'
import { useProjectStore } from './project-store'

interface AiState {
  running: boolean
  task: AiTask | null
  stage: string
  progress: number
  message: string
  logs: string[]
  lastStats: AiResultStats | null
  error: string | null

  run: (task: AiTask, options?: { iterations?: number; viaCost?: number; maxPasses?: number }) => Promise<void>
  cancel: () => void
  clearLogs: () => void
}

let cancelRequested = false

export const useAiStore = create<AiState>((set, get) => ({
  running: false,
  task: null,
  stage: '',
  progress: 0,
  message: '',
  logs: [],
  lastStats: null,
  error: null,

  run: async (task, options = {}) => {
    if (get().running) return
    const { projectId, design, update } = useProjectStore.getState()
    if (!projectId || !design) return
    set({ running: true, task, error: null, progress: 0, stage: 'connexion', message: 'Connexion au moteur IA…', logs: [] })
    cancelRequested = false

    try {
      const socket = await waitForAiSocket()
      const pushLog = (msg: string) =>
        set((s) => ({ logs: [...s.logs.slice(-200), msg] }))

      socket.on('ai:progress', (p: { stage: string; progress: number; message: string }) => {
        set({ stage: p.stage, progress: p.progress, message: p.message })
        pushLog(p.message)
      })

      socket.once('ai:error', (p: { message: string }) => {
        set((s) => ({
          running: false,
          error: p.message,
          logs: [...s.logs, `ERREUR : ${p.message}`],
        }))
        cleanup(socket)
      })

      socket.once('ai:result', (p: { task: AiTask; design: Design; stats: AiResultStats }) => {
        // Remplace le design local par le résultat du moteur (muté côté service)
        update((d) => {
          d.schematic = p.design.schematic
          d.nets = p.design.nets
          d.layout = p.design.layout
          d.board = p.design.board
        })
        set((s) => ({
          running: false,
          progress: 100,
          lastStats: p.stats,
          logs: [
            ...s.logs,
            `Terminé en ${Math.round(p.stats.durationMs)} ms — ${p.stats.routedNets}/${p.stats.totalNets} nets, ${p.stats.vias} vias`,
          ],
        }))
        cleanup(socket)
      })

      const payload = { design, options }
      socket.emit(task === 'place' ? 'ai:place' : task === 'route' ? 'ai:route' : 'ai:optimize', payload)
      pushLog(`Job « ${task} » envoyé au moteur IA (port 3010).`)
    } catch (e) {
      set({ running: false, error: e instanceof Error ? e.message : 'Erreur moteur IA' })
    }

    function cleanup(s: Socket) {
      s.removeAllListeners('ai:progress')
      s.removeAllListeners('ai:result')
      s.removeAllListeners('ai:error')
    }
  },

  cancel: () => {
    cancelRequested = true
    set((s) => ({ logs: [...s.logs, 'Annulation demandée (prise en compte à la prochaine étape).'] }))
  },

  clearLogs: () => set({ logs: [] }),
}))

export { cancelRequested }
