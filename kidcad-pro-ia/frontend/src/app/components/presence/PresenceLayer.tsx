"use client";

/**
 * PresenceLayer — présence collaborative temps réel (best-to-have).
 * Se superpose au canvas : curseurs colorés nommés des autres
 * collaborateurs + pile d'avatars. Diffuse son propre curseur (throttlé),
 * se connecte au hub WebSocket partagé avec la progression.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { localUserName, PresenceSocket } from "@/lib/api/ws-client";
import type { PresenceMessage } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";

const CURSOR_COLORS = ["#e05a4e", "#3f8f5f", "#c98a1b", "#7a5fd0", "#2b7fc1", "#c14e9a"];

/** Curseur flottant d'un collaborateur distant. */
function RemoteCursor({ user, x, y, color, tool }: {
  user: string;
  x: number;
  y: number;
  color: string;
  tool: string;
}) {
  return (
    <div className="presence-cursor" style={{ left: x, top: y, borderColor: color }}>
      <svg width="14" height="14" viewBox="0 0 14 14" fill={color} aria-hidden>
        <path d="M1 1l4.6 12 1.8-5.2L12.6 6z" />
      </svg>
      <span className="presence-cursor-name" style={{ background: color }}>
        {user}
        {tool ? ` · ${tool}` : ""}
      </span>
    </div>
  );
}

interface Peer {
  user: string;
  x: number;
  y: number;
  tool: string;
  color: string;
  lastSeen: number;
}

/** Attribue une couleur stable par nom d'utilisateur. */
function colorFor(user: string): string {
  let h = 0;
  for (let i = 0; i < user.length; i++) h = (h * 31 + user.charCodeAt(i)) >>> 0;
  return CURSOR_COLORS[h % CURSOR_COLORS.length];
}

export function PresenceLayer() {
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const demoMode = useProjectStore((s) => s.demoMode);
  const [peers, setPeers] = useState<Record<string, Peer>>({});
  const [connected, setConnected] = useState(false);
  const socketRef = useRef<PresenceSocket | null>(null);
  const lastSentRef = useRef(0);
  const containerRef = useRef<HTMLDivElement>(null);

  const onPresence = useCallback((msg: PresenceMessage) => {
    setPeers((prev) => {
      const next = { ...prev };
      if (msg.event === "leave") {
        delete next[msg.user];
        return next;
      }
      next[msg.user] = {
        user: msg.user,
        x: msg.cursor.x,
        y: msg.cursor.y,
        tool: msg.tool,
        color: colorFor(msg.user),
        lastSeen: Date.now(),
      };
      return next;
    });
  }, []);

  useEffect(() => {
    if (!projectId || demoMode) return;
    const socket = new PresenceSocket(projectId, localUserName(), onPresence, (status) => {
      setConnected(status === "open");
    });
    socketRef.current = socket;

    // Nettoyage périodique des pairs fantômes (leave perdu).
    const gc = setInterval(() => {
      setPeers((prev) => {
        const cutoff = Date.now() - 15000;
        let changed = false;
        const next: Record<string, Peer> = {};
        for (const [k, p] of Object.entries(prev)) {
          if (p.lastSeen > cutoff) next[k] = p;
          else changed = true;
        }
        return changed ? next : prev;
      });
    }, 5000);

    return () => {
      clearInterval(gc);
      socket.close();
      socketRef.current = null;
      setPeers({});
      setConnected(false);
    };
  }, [projectId, demoMode, onPresence]);

  // Diffusion du curseur local (coordonnées relatives au conteneur, throttlé).
  // Écoute au niveau window car l'overlay est en pointer-events:none.
  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      const socket = socketRef.current;
      const container = containerRef.current;
      if (!socket || !container) return;
      const rect = container.getBoundingClientRect();
      if (
        e.clientX < rect.left || e.clientX > rect.right ||
        e.clientY < rect.top || e.clientY > rect.bottom
      ) {
        return;
      }
      const now = Date.now();
      if (now - lastSentRef.current < 90) return; // ~11 Hz
      lastSentRef.current = now;
      socket.sendPresence(
        { x: e.clientX - rect.left, y: e.clientY - rect.top },
        "layout",
      );
    };
    window.addEventListener("mousemove", onMove);
    return () => window.removeEventListener("mousemove", onMove);
  }, []);

  if (!projectId || demoMode) return null;

  const peerList = Object.values(peers);
  const myName = typeof window !== "undefined" ? localUserName() : "";

  return (
    <>
      <div ref={containerRef} className="presence-layer" aria-hidden>
        {peerList.map((p) => (
          <RemoteCursor key={p.user} user={p.user} x={p.x} y={p.y} color={p.color} tool={p.tool} />
        ))}
      </div>

      <div className="presence-stack" title={connected ? "Présence active" : "Présence hors ligne"}>
        <span className={`presence-self ${connected ? "on" : ""}`}>{myName}</span>
        {peerList.map((p) => (
          <span key={p.user} className="presence-avatar" style={{ background: p.color }}>
            {p.user.slice(0, 2)}
          </span>
        ))}
      </div>
    </>
  );
}
