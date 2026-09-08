"use client";

/**
 * useCrdt — hook React de l'éditeur collaboratif CRDT. Monte un CrdtEditor
 * par projet ouvert, expose l'état de synchronisation (statut, ops en
 * attente), le flux d'activité des opérations distantes et les actions
 * d'édition (send) + undo/redo serveur. Le mode démo (backend injoignable)
 * désactive la collaboration : l'édition locale classique prend le relais.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { CollabOp, CollabOpKind, CollabPayload } from "@/lib/api/types";
import { localUserName } from "@/lib/api/ws-client";
import { CrdtEditor, type CrdtStatus } from "./crdt-client";
import { describeCollabOp } from "./apply-op";

export interface CollabActivity {
  id: string;
  actor: string;
  label: string;
  at: number;
  mine: boolean;
}

const ACTIVITY_LIMIT = 8;

export function useCrdt(projectId: string | null, demoMode: boolean) {
  const [enabled, setEnabled] = useState(true);
  const [status, setStatus] = useState<CrdtStatus>("offline");
  const [pending, setPending] = useState(0);
  const [activity, setActivity] = useState<CollabActivity[]>([]);
  const editorRef = useRef<CrdtEditor | null>(null);

  useEffect(() => {
    const active = projectId && !demoMode && enabled;
    if (!active) {
      editorRef.current?.destroy();
      editorRef.current = null;
      setStatus("offline");
      setPending(0);
      return;
    }
    const editor = new CrdtEditor(projectId as string, localUserName(), {
      onStatus: (s, p) => {
        setStatus(s);
        setPending(p);
      },
      onRemoteOp: (op) => {
        setActivity((prev) =>
          [
            {
              id: op.id,
              actor: op.actor,
              label: describeCollabOp(op),
              at: Date.now(),
              mine: false,
            },
            ...prev,
          ].slice(0, ACTIVITY_LIMIT),
        );
      },
    });
    editorRef.current = editor;
    return () => {
      editor.destroy();
      editorRef.current = null;
    };
  }, [projectId, demoMode, enabled]);

  /** Édition locale : mutation CRDT (payload déjà appliqué au store par l'appelant). */
  const send = useCallback((kind: CollabOpKind, payload: CollabPayload, target?: string) => {
    editorRef.current?.send(kind, payload, target);
    setPending(editorRef.current?.pending ?? 0);
  }, []);

  const undo = useCallback(async () => {
    const res = await editorRef.current?.undo();
    return res ?? null;
  }, []);

  const redo = useCallback(async () => {
    const res = await editorRef.current?.redo();
    return res ?? null;
  }, []);

  const active = Boolean(projectId && !demoMode && enabled);

  return { enabled, setEnabled, status, pending, activity, send, undo, redo, active };
}
