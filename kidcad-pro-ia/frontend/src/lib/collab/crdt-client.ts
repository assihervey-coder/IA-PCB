/**
 * CrdtEditor — client d'édition collaborative du layout (côté frontend).
 *
 * Convergence garantie par le CRDT opérationnel du backend (registres LWW
 * estampillés Lamport + ensembles additifs, journal JSONL durable) ; ce
 * client en est le homologue navigateur :
 *
 *  - Édition locale optimiste : la mutation est appliquée immédiatement au
 *    store, l'opération rejoint une outbox PERSISTANTE (localStorage) puis
 *    part par lots vers POST .../collab/ops (idempotent par client_id).
 *  - Hors ligne : les opérations s'accumulent et partent au retour du réseau.
 *  - Temps réel : les opérations des autres éditeurs arrivent par WebSocket
 *    (type "collab") et sont fusionnées dans le store (applyCollabOp).
 *  - Rattrapage : à l'ouverture puis à chaque reconnexion, GET
 *    .../collab/state?since=seq rejoue les opérations manquées.
 *  - Déduplication par op id (nos propres ops reviennent aussi par le WS).
 *  - Undo/redo persistants : délégués au serveur (piles par acteur), donc
 *    ils survivent au rechargement de la page.
 */
import { api } from "@/lib/api/rest-client";
import { CollabSocket, type SocketStatus } from "@/lib/api/ws-client";
import type { CollabOp, CollabOpKind, CollabOpRequest, CollabPayload, CollabUndoResult } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { applyCollabOp } from "./apply-op";

export type CrdtStatus = "offline" | "connecting" | "synced" | "error";

export interface CrdtEvents {
  onStatus?: (status: CrdtStatus, pending: number) => void;
  onRemoteOp?: (op: CollabOp) => void;
}

const OUTBOX_PREFIX = "kidcad-crdt-outbox";
const BATCH_LIMIT = 50;
const SEEN_LIMIT = 2000;
const RETRY_MS = 3000;

/** Identifiant d'opération client (crypto quand disponible). */
function newClientId(actor: string): string {
  const rnd =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  return `${actor}:${rnd}`;
}

export class CrdtEditor {
  private readonly projectId: string;
  private readonly actor: string;
  private readonly events: CrdtEvents;
  private readonly socket: CollabSocket;

  private outbox: CollabOpRequest[] = [];
  private seen = new Set<string>();
  private seenOrder: string[] = [];
  private lastSeq = -1;
  private flushing = false;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;
  private status: CrdtStatus = "connecting";

  constructor(projectId: string, actor: string, events: CrdtEvents = {}) {
    this.projectId = projectId;
    this.actor = actor;
    this.events = events;
    this.outbox = this.loadOutbox();

    this.socket = new CollabSocket(projectId, {
      onOp: (msg) => {
        this.lastSeq = Math.max(this.lastSeq, msg.seq ?? -1);
        void this.applyRemote(msg.op);
      },
      onResync: () => {
        void this.catchUp();
      },
      onStatus: (s: SocketStatus) => {
        this.setStatus(s === "open" ? "synced" : s === "connecting" ? "connecting" : "error");
      },
    });

    // Démarrage : rattrapage du journal puis purge de l'outbox résiduelle.
    void this.catchUp().then(() => this.flush());
  }

  // --------------------------------------------------------------
  // API publique
  // --------------------------------------------------------------

  /** Édition locale : mutation optimiste immédiate + mise en file CRDT. */
  send(kind: CollabOpKind, payload: CollabPayload, target?: string): void {
    if (this.destroyed) return;
    const req: CollabOpRequest = {
      client_id: newClientId(this.actor),
      kind,
      target,
      payload,
    };
    this.outbox.push(req);
    this.persistOutbox();
    this.notifyPending();
    void this.flush();
  }

  /** Annule la dernière opération locale (historique serveur persistant). */
  async undo(): Promise<CollabUndoResult | null> {
    if (this.destroyed) return null;
    try {
      return await api.collabUndo(this.projectId, this.actor);
    } catch {
      this.setStatus("error");
      return null;
    }
  }

  /** Rétablit la dernière opération annulée. */
  async redo(): Promise<CollabUndoResult | null> {
    if (this.destroyed) return null;
    try {
      return await api.collabRedo(this.projectId, this.actor);
    } catch {
      this.setStatus("error");
      return null;
    }
  }

  get pending(): number {
    return this.outbox.length;
  }

  /** Ferme le socket et persiste l'outbox (reprise au prochain montage). */
  destroy(): void {
    this.destroyed = true;
    if (this.retryTimer !== null) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    this.socket.close();
    this.persistOutbox();
  }

  // --------------------------------------------------------------
  // Envoi par lots + réessai
  // --------------------------------------------------------------

  private async flush(): Promise<void> {
    if (this.destroyed || this.flushing || this.outbox.length === 0) return;
    this.flushing = true;
    const batch = this.outbox.slice(0, BATCH_LIMIT);
    try {
      const res = await api.collabOps(this.projectId, this.actor, batch);
      // Nos ops nous reviendront aussi par WS : mémoriser leurs ids.
      for (const op of res.ops ?? []) this.markSeen(op.id);
      if (res.rejected?.length) {
        // Ops refusées (incohérentes avec la carte) : jetées avec trace.
        console.warn("[crdt] opérations refusées", res.rejected);
      }
      this.outbox = this.outbox.slice(batch.length);
      this.persistOutbox();
      this.lastSeq = Math.max(this.lastSeq, res.seq ?? -1);
      this.setStatus("synced");
      this.notifyPending();
    } catch {
      // Backend injoignable : l'outbox persistante gardera les ops.
      this.setStatus("error");
      this.scheduleRetry();
    } finally {
      this.flushing = false;
      if (this.outbox.length > 0 && !this.destroyed) {
        this.scheduleRetry(50);
      }
    }
  }

  private scheduleRetry(delayMs = RETRY_MS): void {
    if (this.destroyed || this.retryTimer !== null) return;
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.flush();
    }, delayMs);
  }

  // --------------------------------------------------------------
  // Réception + rattrapage
  // --------------------------------------------------------------

  private async catchUp(): Promise<void> {
    if (this.destroyed) return;
    try {
      const st = await api.collabState(this.projectId, this.lastSeq);
      for (const entry of st.ops ?? []) {
        if (!entry?.op || entry.applied === false) continue;
        this.lastSeq = Math.max(this.lastSeq, entry.seq ?? -1);
        await this.applyRemote(entry.op, true);
      }
      this.lastSeq = Math.max(this.lastSeq, st.seq ?? -1);
      this.setStatus("synced");
    } catch {
      this.setStatus("error");
    }
  }

  /** Fusionne une opération distante dans le store (dédupliquée par id). */
  private async applyRemote(op: CollabOp, quiet = false): Promise<void> {
    if (!op?.id || this.destroyed) return;
    if (this.seen.has(op.id)) return;
    this.markSeen(op.id);

    const cur = useProjectStore.getState().layout;
    const next = applyCollabOp(cur, op);
    if (next && next !== cur) {
      useProjectStore.setState({ layout: next });
    }
    if (!quiet) this.events.onRemoteOp?.(op);
  }

  // --------------------------------------------------------------
  // Divers
  // --------------------------------------------------------------

  private markSeen(id: string): void {
    if (this.seen.has(id)) return;
    this.seen.add(id);
    this.seenOrder.push(id);
    while (this.seenOrder.length > SEEN_LIMIT) {
      const evicted = this.seenOrder.shift();
      if (evicted !== undefined) this.seen.delete(evicted);
    }
  }

  private setStatus(status: CrdtStatus): void {
    if (this.destroyed || this.status === status) return;
    this.status = status;
    this.events.onStatus?.(status, this.outbox.length);
  }

  private notifyPending(): void {
    this.events.onStatus?.(this.status, this.outbox.length);
  }

  private loadOutbox(): CollabOpRequest[] {
    if (typeof window === "undefined") return [];
    try {
      const raw = window.localStorage.getItem(`${OUTBOX_PREFIX}:${this.projectId}`);
      return raw ? (JSON.parse(raw) as CollabOpRequest[]) : [];
    } catch {
      return [];
    }
  }

  private persistOutbox(): void {
    if (typeof window === "undefined") return;
    const key = `${OUTBOX_PREFIX}:${this.projectId}`;
    try {
      if (this.outbox.length === 0) {
        window.localStorage.removeItem(key);
      } else {
        window.localStorage.setItem(key, JSON.stringify(this.outbox));
      }
    } catch {
      // quota / mode privé : l'outbox reste en mémoire
    }
  }
}
