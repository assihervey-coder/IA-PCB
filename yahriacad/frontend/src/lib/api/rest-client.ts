/**
 * Client REST de l'API YahriaCad (contrat docs/architecture/contracts.md §2).
 * En dev, les appels passent par le rewrite Next `/api/v1/*` vers le backend Go.
 */
import axios from "axios";
import type {
  AIStrategy,
  ArenaReport,
  ArenaStanding,
  AutoFixResult,
  CollabApplyResult,
  CollabOpRequest,
  CollabState,
  CollabUndoResult,
  DFMEstimate,
  DiffImpedanceRequest,
  DiffImpedanceResult,
  DRCResult,
  DoctorReport,
  ERCResult,
  ImportResult,
  JobStarted,
  JobStatus,
  LayoutData,
  MagicResult,
  Project,
  ProjectCreate,
  ProjectPatch,
  RouteJobStart,
  SIResult,
  SnapshotDiff,
  SnapshotList,
  SnapshotMeta,
  StatsReport,
  ThermalResult,
} from "./types";

const baseURL = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1";
const TIMEOUT_MS = 10000;

const http = axios.create({
  baseURL,
  timeout: TIMEOUT_MS,
});

export interface ProjectListResponse {
  projects: Project[];
  total: number;
}

export interface ExportDownload {
  blob: Blob;
  /** Nom de fichier extrait de Content-Disposition, sinon null. */
  filename: string | null;
}

function filenameFromDisposition(value: unknown): string | null {
  if (typeof value !== "string" || value.length === 0) return null;
  const utf8 = /filename\*=UTF-8''([^;]+)/i.exec(value);
  if (utf8?.[1]) {
    try {
      return decodeURIComponent(utf8[1]);
    } catch {
      // nom mal encodé : on retombe sur le motif simple
    }
  }
  const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(value);
  return plain?.[1] ?? null;
}

async function requestBlob(path: string): Promise<ExportDownload> {
  const res = await http.get<Blob>(path, { responseType: "blob" });
  return { blob: res.data, filename: filenameFromDisposition(res.headers?.["content-disposition"]) };
}

function id(projectId: string): string {
  return encodeURIComponent(projectId);
}

export const api = {
  /** Vérifie que le backend répond (utilisé par l'indicateur d'état). */
  async pingBackend(): Promise<boolean> {
    try {
      await http.get("/projects", { params: { limit: 1 }, timeout: 4000 });
      return true;
    } catch (err) {
      // Une réponse HTTP (même 4xx) prouve que le serveur est joignable.
      return axios.isAxiosError(err) && err.response !== undefined;
    }
  },

  async listProjects(limit = 50, offset = 0): Promise<ProjectListResponse> {
    const res = await http.get<ProjectListResponse>("/projects", { params: { limit, offset } });
    return res.data;
  },

  async createProject(payload: ProjectCreate): Promise<Project> {
    const res = await http.post<Project>("/projects", payload);
    return res.data;
  },

  async getProject(projectId: string): Promise<Project> {
    const res = await http.get<Project>(`/projects/${id(projectId)}`);
    return res.data;
  },

  async updateProject(projectId: string, patch: ProjectPatch): Promise<Project> {
    const res = await http.put<Project>(`/projects/${id(projectId)}`, patch);
    return res.data;
  },

  async deleteProject(projectId: string): Promise<void> {
    await http.delete(`/projects/${id(projectId)}`);
  },

  async importFile(projectId: string, file: File): Promise<ImportResult> {
    const form = new FormData();
    form.append("file", file, file.name);
    const res = await http.post<ImportResult>(`/projects/${id(projectId)}/import`, form, {
      headers: { "Content-Type": "multipart/form-data" },
    });
    return res.data;
  },

  async getLayout(projectId: string): Promise<LayoutData> {
    const res = await http.get<LayoutData>(`/projects/${id(projectId)}/layout`);
    return res.data;
  },

  async putLayout(projectId: string, layout: LayoutData): Promise<LayoutData> {
    const res = await http.put<LayoutData>(`/projects/${id(projectId)}/layout`, layout);
    return res.data;
  },

  /** Placement IA — synchrone, renvoie le layout mis à jour. */
  async startPlace(projectId: string, strategy: AIStrategy): Promise<LayoutData> {
    const res = await http.post<LayoutData>(`/projects/${id(projectId)}/place`, { strategy });
    return res.data;
  },

  /** Routage IA — asynchrone, renvoie un job à suivre (REST + WebSocket). */
  async startRoute(projectId: string, payload: RouteJobStart): Promise<JobStarted> {
    const res = await http.post<JobStarted>(`/projects/${id(projectId)}/route`, payload);
    return res.data;
  },

  async startOptimize(projectId: string): Promise<JobStarted> {
    const res = await http.post<JobStarted>(`/projects/${id(projectId)}/optimize`, {});
    return res.data;
  },

  async getJob(projectId: string, jobId: string): Promise<JobStatus> {
    const res = await http.get<JobStatus>(`/projects/${id(projectId)}/jobs/${encodeURIComponent(jobId)}`);
    return res.data;
  },

  async runDRC(projectId: string): Promise<DRCResult> {
    const res = await http.post<DRCResult>(`/projects/${id(projectId)}/drc`);
    return res.data;
  },

  async runERC(projectId: string): Promise<ERCResult> {
    const res = await http.post<ERCResult>(`/projects/${id(projectId)}/erc`);
    return res.data;
  },

  async exportGerber(projectId: string): Promise<ExportDownload> {
    return requestBlob(`/projects/${id(projectId)}/export/gerber`);
  },

  async exportBOM(projectId: string): Promise<ExportDownload> {
    return requestBlob(`/projects/${id(projectId)}/export/bom`);
  },

  async exportSTEP(projectId: string): Promise<ExportDownload> {
    return requestBlob(`/projects/${id(projectId)}/export/step`);
  },

  // ------------------------------------------------------------
  // Magic Pack
  // ------------------------------------------------------------

  async magic(projectId: string, utterance: string, apply = false): Promise<MagicResult> {
    const res = await http.post<MagicResult>(`/projects/${id(projectId)}/magic`, {
      utterance,
      apply,
    });
    return res.data;
  },

  async thermal(projectId: string): Promise<ThermalResult> {
    const res = await http.post<ThermalResult>(`/projects/${id(projectId)}/thermal`, {});
    return res.data;
  },

  async signalIntegrity(projectId: string): Promise<SIResult> {
    const res = await http.post<SIResult>(`/projects/${id(projectId)}/si`, {});
    return res.data;
  },

  async arenaFight(projectId: string): Promise<ArenaReport> {
    const res = await http.post<ArenaReport>(`/projects/${id(projectId)}/arena`, {});
    return res.data;
  },

  async arenaLeaderboard(): Promise<{ standings: ArenaStanding[] }> {
    const res = await http.get<{ standings: ArenaStanding[] }>("/arena/leaderboard");
    return res.data;
  },

  // ------------------------------------------------------------
  // Pack WOW (additif)
  // ------------------------------------------------------------

  /** DRC Auto-Healer — répare largeurs, vias et marges de bord. */
  async drcAutoFix(projectId: string, dryRun = false): Promise<AutoFixResult> {
    const res = await http.post<AutoFixResult>(`/projects/${id(projectId)}/drc/autofix`, {
      dry_run: dryRun,
    });
    return res.data;
  },

  /** Design Doctor — audit global noté avec ordonnances. */
  async doctor(projectId: string): Promise<DoctorReport> {
    const res = await http.get<DoctorReport>(`/projects/${id(projectId)}/doctor`);
    return res.data;
  },

  /** Oracle DFM — coût de fabrication + rendement premier passage. */
  async dfmEstimate(projectId: string): Promise<DFMEstimate> {
    const res = await http.post<DFMEstimate>(`/projects/${id(projectId)}/dfm`, {});
    return res.data;
  },

  /** Time Machine — capture un instantané de la carte. */
  async captureSnapshot(projectId: string, label = ""): Promise<SnapshotMeta> {
    const res = await http.post<SnapshotMeta>(`/projects/${id(projectId)}/snapshots`, { label });
    return res.data;
  },

  /** Time Machine — liste les instantanés (plus récents d'abord). */
  async listSnapshots(projectId: string): Promise<SnapshotList> {
    const res = await http.get<SnapshotList>(`/projects/${id(projectId)}/snapshots`);
    return res.data;
  },

  /** Time Machine — diff structurel entre une capture et l'état courant. */
  async snapshotDiff(projectId: string, snapshotId: string): Promise<SnapshotDiff> {
    const res = await http.get<SnapshotDiff>(
      `/projects/${id(projectId)}/snapshots/${encodeURIComponent(snapshotId)}/diff`,
    );
    return res.data;
  },

  /** Time Machine — restaure une capture (avec capture de sécurité). */
  async restoreSnapshot(projectId: string, snapshotId: string): Promise<SnapshotMeta> {
    const res = await http.post<SnapshotMeta>(
      `/projects/${id(projectId)}/snapshots/${encodeURIComponent(snapshotId)}/restore`,
      {},
    );
    return res.data;
  },

  /** Stats live — tableau de bord statistique du design. */
  async stats(projectId: string): Promise<StatsReport> {
    const res = await http.get<StatsReport>(`/projects/${id(projectId)}/stats`);
    return res.data;
  },

  // ------------------------------------------------------------
  // Collaboration CRDT (additif)
  // ------------------------------------------------------------

  /** Applique un lot d'opérations CRDT (idempotent par client_id). */
  async collabOps(projectId: string, actor: string, ops: CollabOpRequest[]): Promise<CollabApplyResult> {
    const res = await http.post<CollabApplyResult>(`/projects/${id(projectId)}/collab/ops`, {
      actor,
      ops,
    });
    return res.data;
  },

  /** Séquence + horloge vectorielle + rattrapage du journal depuis since. */
  async collabState(projectId: string, since = -1): Promise<CollabState> {
    const res = await http.get<CollabState>(`/projects/${id(projectId)}/collab/state`, {
      params: since >= 0 ? { since } : undefined,
    });
    return res.data;
  },

  /** Annule la dernière op de l'acteur (historique persistant côté serveur). */
  async collabUndo(projectId: string, actor: string): Promise<CollabUndoResult> {
    const res = await http.post<CollabUndoResult>(`/projects/${id(projectId)}/collab/undo`, { actor });
    return res.data;
  },

  /** Rétablit la dernière op annulée de l'acteur. */
  async collabRedo(projectId: string, actor: string): Promise<CollabUndoResult> {
    const res = await http.post<CollabUndoResult>(`/projects/${id(projectId)}/collab/redo`, { actor });
    return res.data;
  },

  // ------------------------------------------------------------
  // Impédance différentielle (additif)
  // ------------------------------------------------------------

  /** Oracle d'impédance différentielle — paires détectées par classe de nets. */
  async impedance(projectId: string, payload: DiffImpedanceRequest = {}): Promise<DiffImpedanceResult> {
    const res = await http.post<DiffImpedanceResult>(`/projects/${id(projectId)}/impedance`, payload);
    return res.data;
  },
};

/**
 * Détermine si l'erreur correspond à un backend injoignable
 * (erreur réseau, ECONNREFUSED, timeout ou erreur serveur 5xx).
 */
export function isBackendDown(err: unknown): boolean {
  if (axios.isAxiosError(err)) {
    if (!err.response) return true; // pas de réponse : réseau / DNS / timeout
    return err.response.status >= 500 || err.code === "ECONNREFUSED";
  }
  return false;
}
