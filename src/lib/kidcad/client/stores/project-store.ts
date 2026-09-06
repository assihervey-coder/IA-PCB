/**
 * client/stores/project-store — État global du projet (Zustand).
 */
import { create } from 'zustand'
import { Design } from '../../shared/types'
import { api, ProjectSummary } from '../api'

interface ProjectState {
  projects: ProjectSummary[]
  projectId: string | null
  design: Design | null
  loading: boolean
  saving: boolean
  dirty: boolean
  error: string | null

  fetchProjects: () => Promise<void>
  createProject: (name: string, description: string, template: string) => Promise<string | null>
  openProject: (id: string) => Promise<void>
  saveProject: () => Promise<void>
  deleteProject: (id: string) => Promise<void>
  /** Clone superficiel du design + mutation (marque dirty). */
  update: (mutator: (d: Design) => void) => void
  closeProject: () => void
  setError: (e: string | null) => void
}

export const useProjectStore = create<ProjectState>((set, get) => ({
  projects: [],
  projectId: null,
  design: null,
  loading: false,
  saving: false,
  dirty: false,
  error: null,

  fetchProjects: async () => {
    set({ loading: true, error: null })
    try {
      const { projects } = await api.listProjects()
      set({ projects, loading: false })
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : 'Erreur' })
    }
  },

  createProject: async (name, description, template) => {
    set({ error: null })
    try {
      const { project } = await api.createProject(name, description, template)
      set((s) => ({ projects: [toSummary(project), ...s.projects] }))
      return project.id
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Création impossible' })
      return null
    }
  },

  openProject: async (id) => {
    set({ loading: true, error: null })
    try {
      const { project } = await api.getProject(id)
      set({ projectId: project.id, design: project.design, loading: false, dirty: false })
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : 'Ouverture impossible' })
    }
  },

  saveProject: async () => {
    const { projectId, design } = get()
    if (!projectId || !design) return
    set({ saving: true, error: null })
    try {
      await api.saveProject(projectId, design)
      set({ saving: false, dirty: false })
      get().fetchProjects().catch(() => {})
    } catch (e) {
      set({ saving: false, error: e instanceof Error ? e.message : 'Sauvegarde impossible' })
    }
  },

  deleteProject: async (id) => {
    try {
      await api.deleteProject(id)
      set((s) => ({
        projects: s.projects.filter((p) => p.id !== id),
        projectId: s.projectId === id ? null : s.projectId,
        design: s.projectId === id ? null : s.design,
      }))
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Suppression impossible' })
    }
  },

  update: (mutator) => {
    const { design } = get()
    if (!design) return
    const next: Design = structuredClone(design)
    mutator(next)
    set({ design: next, dirty: true })
  },

  closeProject: () => set({ projectId: null, design: null, dirty: false }),
  setError: (e) => set({ error: e }),
}))

function toSummary(p: { id: string; name: string; description: string; template: string; design: Design }): ProjectSummary {
  return {
    id: p.id,
    name: p.name,
    description: p.description,
    template: p.template,
    boardWidth: p.design.board.width,
    boardHeight: p.design.board.height,
    components: p.design.layout.footprints.length,
    tracks: p.design.layout.tracks.length,
    vias: p.design.layout.vias.length,
    updatedAt: new Date().toISOString(),
    createdAt: new Date().toISOString(),
  }
}
