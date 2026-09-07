/**
 * Client WebSocket de progression (contrat contracts.md §3).
 * URL : ${NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080"}/ws/v1/progress?project_id=…
 * Reconnexion exponentielle (1 s → 2 s → 4 s …, 5 essais maximum) + ping applicatif.
 */
import type { ProgressEvent } from "./types";

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
