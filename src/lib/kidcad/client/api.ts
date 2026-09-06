/**
 * client/api — Client REST vers l'API Next.js (rest-client.ts équivalent).
 */
import { Design, DrcReport, ErcReport, Violation } from '../shared/types'

export interface ProjectSummary {
  id: string
  name: string
  description: string
  template: string
  boardWidth: number
  boardHeight: number
  components: number
  tracks: number
  vias: number
  updatedAt: string
  createdAt: string
}

export interface ProjectFull {
  id: string
  name: string
  description: string
  template: string
  design: Design
}

async function unwrap<T>(res: Response): Promise<T> {
  const data = (await res.json().catch(() => ({}))) as T & { error?: string }
  if (!res.ok) throw new Error(data?.error ?? `Erreur HTTP ${res.status}`)
  return data
}

const get = async <T>(url: string) => unwrap<T>(await fetch(url))
const send = async <T>(url: string, method: string, body?: unknown) =>
  unwrap<T>(
    await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  )

export const api = {
  listProjects: () => get<{ projects: ProjectSummary[] }>('/api/projects'),
  createProject: (name: string, description: string, template: string) =>
    send<{ project: ProjectFull }>('/api/projects', 'POST', { name, description, template }),
  getProject: (id: string) => get<{ project: ProjectFull }>(`/api/projects/${id}`),
  saveProject: (id: string, design: Design) =>
    send<{ project: ProjectFull }>(`/api/projects/${id}`, 'PUT', { name: design.name, description: design.description, design }),
  deleteProject: (id: string) => send<{ ok: boolean }>(`/api/projects/${id}`, 'DELETE'),
  drc: (id: string) => send<{ report: DrcReport }>(`/api/projects/${id}/drc`, 'POST'),
  erc: (id: string) => send<{ report: ErcReport }>(`/api/projects/${id}/erc`, 'POST'),
  importNetlist: (id: string, format: 'kicad' | 'json', content: string) =>
    send<{ ok: boolean; nets: number; components?: number; warnings: string[] }>(`/api/projects/${id}/import`, 'POST', { format, content }),
  listFootprints: () =>
    get<{ footprints: { name: string; category: string; bodyW: number; bodyH: number; pads: unknown[] }[] }>('/api/footprints'),
}

export type { DrcReport, ErcReport, Violation }
