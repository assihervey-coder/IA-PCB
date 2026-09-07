/**
 * Store Zustand : projets, layout courant, mode démo, vérifications DRC/ERC.
 * Logique de repli : si le backend est injoignable, on bascule en mode démo
 * et on charge les fixtures `public/demo/`.
 */
import { create } from "zustand";
import { api, isBackendDown } from "@/lib/api/rest-client";
import type {
  DRCResult,
  ERCResult,
  JobStatus,
  LayoutData,
  ProgressEvent,
  Project,
} from "@/lib/api/types";

interface ProjectStoreState {
  projects: Project[];
  currentProject: Project | null;
  layout: LayoutData | null;
  loading: boolean;
  error: string | null;
  demoMode: boolean;
  drc: DRCResult | null;
  erc: ERCResult | null;
  lastJob: JobStatus | null;
  percent: number;
  loadProjects: () => Promise<void>;
  openProject: (id: string) => Promise<void>;
  refreshLayout: (projectId?: string) => Promise<void>;
  applyProgressEvent: (event: ProgressEvent) => void;
  setLastJob: (job: JobStatus | null) => void;
  setDemoMode: (value: boolean) => void;
  loadDemoData: () => Promise<void>;
  setDRC: (result: DRCResult | null) => void;
  setERC: (result: ERCResult | null) => void;
  reset: () => void;
}

export const useProjectStore = create<ProjectStoreState>()((set, get) => ({
  projects: [],
  currentProject: null,
  layout: null,
  loading: false,
  error: null,
  demoMode: false,
  drc: null,
  erc: null,
  lastJob: null,
  percent: 0,

  loadProjects: async () => {
    set({ loading: true, error: null });
    try {
      const data = await api.listProjects(50, 0);
      set({ projects: data.projects, loading: false });
    } catch (err) {
      if (isBackendDown(err)) {
        set({ loading: false, demoMode: true, error: null });
        await get().loadDemoData();
      } else {
        set({ loading: false, error: "Impossible de récupérer la liste des projets." });
      }
    }
  },

  openProject: async (projectId) => {
    set({ loading: true, error: null });
    try {
      const project = await api.getProject(projectId);
      set({ currentProject: project, loading: false });
      await get().refreshLayout(projectId);
    } catch (err) {
      if (isBackendDown(err)) {
        set({ loading: false, demoMode: true, error: null });
        await get().loadDemoData();
      } else {
        set({ loading: false, error: "Impossible de charger le projet demandé." });
      }
    }
  },

  refreshLayout: async (projectId) => {
    const pid = projectId ?? get().currentProject?.id;
    if (!pid) return;
    try {
      const layout = await api.getLayout(pid);
      set({ layout });
    } catch (err) {
      if (isBackendDown(err)) {
        set({ demoMode: true });
        await get().loadDemoData();
      }
      // Les autres erreurs (layout absent, 404…) laissent l'état courant inchangé.
    }
  },

  applyProgressEvent: (event) => {
    const previous = get().lastJob;
    const base: JobStatus = previous ?? {
      job_id: event.job_id,
      project_id: "",
      kind: "route",
      state: "running",
      percent: 0,
      current_net: "",
      message: "",
      error: "",
      nets_done: 0,
      nets_total: 0,
      started_at: new Date().toISOString(),
      finished_at: null,
    };
    const state: JobStatus["state"] = event.error ? "failed" : event.done ? "done" : "running";
    set({
      lastJob: {
        ...base,
        job_id: event.job_id,
        state,
        percent: event.percent,
        current_net: event.current_net,
        message: event.message,
        error: event.error,
      },
      percent: event.percent,
    });
  },

  setLastJob: (job) => set({ lastJob: job }),

  setDemoMode: (value) => set({ demoMode: value }),

  loadDemoData: async () => {
    try {
      const [projectRes, boardRes] = await Promise.all([
        fetch("/demo/demo-project.json"),
        fetch("/demo/demo-board.json"),
      ]);
      if (!projectRes.ok || !boardRes.ok) {
        throw new Error("fixtures de démonstration indisponibles");
      }
      const project = (await projectRes.json()) as Project;
      const layout = (await boardRes.json()) as LayoutData;
      set((st) => ({
        currentProject: project,
        layout,
        projects: st.projects.some((p) => p.id === project.id)
          ? st.projects
          : [project, ...st.projects],
      }));
    } catch {
      set({ error: "Impossible de charger les données de démonstration." });
    }
  },

  setDRC: (result) => set({ drc: result }),
  setERC: (result) => set({ erc: result }),

  reset: () =>
    set({
      projects: [],
      currentProject: null,
      layout: null,
      loading: false,
      error: null,
      demoMode: false,
      drc: null,
      erc: null,
      lastJob: null,
      percent: 0,
    }),
}));

/**
 * Exécute un appel backend avec repli automatique en mode démo :
 * si le backend est injoignable, active le mode démo, charge les fixtures
 * `public/demo/` puis renvoie le résultat de la fonction de secours.
 */
export async function withDemoFallback<T>(
  fn: () => Promise<T>,
  fallback: () => Promise<T> | T,
): Promise<T> {
  try {
    return await fn();
  } catch (err) {
    if (isBackendDown(err)) {
      const store = useProjectStore.getState();
      store.setDemoMode(true);
      await store.loadDemoData();
      return await fallback();
    }
    throw err;
  }
}
