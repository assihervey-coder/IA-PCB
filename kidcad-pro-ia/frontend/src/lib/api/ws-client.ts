/**
 * Client WebSocket de progression (contrat contracts.md §3).
 * URL : ${NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080"}/ws/v1/progress?project_id=…
 * Reconnexion exponentielle (1 s → 2 s → 4 s …, 5 essais maximum) + ping applicatif.
 * Le même canal transporte la présence collaborative (Pack WOW) : curseurs,
 * outils actifs et arrivées/départs des autres collaborateurs du projet.
 */
import type { PresenceMessage, ProgressEvent } from "./types";

export type SocketStatus = "connecting" | "open" | "closed" | "error";

const WS_BASE = process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080";
const MAX_RETRIES = 5;
const PING_INTERVAL_MS = 25000;

interface ServerMessage {
  type?: string;
}

export class ProgressSocket {
  private ws: WebSocket | null = null;
  private retries = 0;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;

  constructor(
    private readonly projectId: string,
    private readonly onEvent: (event: ProgressEvent) => void,
    private readonly onStatus?: (status: SocketStatus) => void,
  ) {
    this.connect();
  }

  private connect(): void {
    if (this.closedByUser) return;
    this.onStatus?.("connecting");
    let socket: WebSocket;
    try {
      socket = new WebSocket(
        `${WS_BASE}/ws/v1/progress?project_id=${encodeURIComponent(this.projectId)}`,
      );
    } catch {
      this.onStatus?.("error");
      this.scheduleReconnect();
      return;
    }
    this.ws = socket;

    socket.onopen = () => {
      this.retries = 0;
      this.onStatus?.("open");
      this.send({ type: "subscribe", project_id: this.projectId });
      this.pingTimer = setInterval(() => this.send({ type: "ping" }), PING_INTERVAL_MS);
    };

    socket.onmessage = (ev: MessageEvent) => {
      try {
        const data = JSON.parse(String(ev.data)) as ServerMessage;
        if (data.type === "progress") {
          this.onEvent(data as unknown as ProgressEvent);
        }
        // "pong" et autres messages : ignorés.
      } catch {
        // message non JSON : ignoré silencieusement
      }
    };

    socket.onerror = () => {
      this.onStatus?.("error");
    };

    socket.onclose = () => {
      this.clearPing();
      this.onStatus?.("closed");
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect(): void {
    if (this.closedByUser) return;
    if (this.retries >= MAX_RETRIES) {
      this.onStatus?.("error");
      return;
    }
    const delay = 1000 * Math.pow(2, this.retries); // 1 s → 2 s → 4 s → 8 s → 16 s
    this.retries += 1;
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private send(payload: Record<string, unknown>): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(payload));
    }
  }

  private clearPing(): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  /** Fermeture propre (arrête ping + reconnexions). */
  close(): void {
    this.closedByUser = true;
    this.clearPing();
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onerror = null;
      this.ws.onclose = null;
      if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.close();
      }
      this.ws = null;
    }
  }
}

/**
 * PresenceSocket — canal de présence collaborative (Pack WOW) partagé avec
 * la progression : chaque onglet diffuse son nom, son curseur (coordonnées
 * carte en mm) et son outil actif ; le hub relaie aux autres abonnés du
 * projet et synthétise les événements join/leave.
 */
export class PresenceSocket {
  private ws: WebSocket | null = null;
  private retries = 0;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;

  constructor(
    private readonly projectId: string,
    private readonly userName: string,
    private readonly onPresence: (msg: PresenceMessage) => void,
    private readonly onStatus?: (status: SocketStatus) => void,
  ) {
    this.connect();
  }

  private connect(): void {
    if (this.closedByUser) return;
    this.onStatus?.("connecting");
    let socket: WebSocket;
    try {
      socket = new WebSocket(
        `${WS_BASE}/ws/v1/progress?project_id=${encodeURIComponent(this.projectId)}`,
      );
    } catch {
      this.onStatus?.("error");
      this.scheduleReconnect();
      return;
    }
    this.ws = socket;

    socket.onopen = () => {
      this.retries = 0;
      this.onStatus?.("open");
      this.send({ type: "subscribe", project_id: this.projectId });
      // Annonce de présence : la première trace déclenche le "join" côté hub.
      this.send({ type: "presence", user: this.userName, cursor: { x: 0, y: 0 }, tool: "" });
      this.pingTimer = setInterval(() => this.send({ type: "ping" }), PING_INTERVAL_MS);
    };

    socket.onmessage = (ev: MessageEvent) => {
      try {
        const data = JSON.parse(String(ev.data)) as { type?: string };
        if (data.type === "presence") {
          this.onPresence(data as unknown as PresenceMessage);
        }
        // "progress", "pong" : ignorés ici (gérés par ProgressSocket).
      } catch {
        // message non JSON : ignoré silencieusement
      }
    };

    socket.onerror = () => {
      this.onStatus?.("error");
    };

    socket.onclose = () => {
      this.clearPing();
      this.onStatus?.("closed");
      this.scheduleReconnect();
    };
  }

  /** Diffuse la position du curseur et l'outil actif. */
  sendPresence(cursor: { x: number; y: number }, tool: string): void {
    this.send({ type: "presence", user: this.userName, cursor, tool });
  }

  private scheduleReconnect(): void {
    if (this.closedByUser) return;
    if (this.retries >= MAX_RETRIES) {
      this.onStatus?.("error");
      return;
    }
    const delay = 1000 * Math.pow(2, this.retries);
    this.retries += 1;
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private send(payload: Record<string, unknown>): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(payload));
    }
  }

  private clearPing(): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  close(): void {
    this.closedByUser = true;
    this.clearPing();
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onerror = null;
      this.ws.onclose = null;
      if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.close();
      }
      this.ws = null;
    }
  }
}

/** Nom d'affichage local persistant pour la présence collaborative. */
export function localUserName(): string {
  if (typeof window === "undefined") return "Invité";
  const KEY = "kidcad-user-name";
  let name = window.localStorage.getItem(KEY);
  if (!name) {
    name = `Invité-${Math.floor(1000 + Math.random() * 9000)}`;
    window.localStorage.setItem(KEY, name);
  }
  return name;
}
