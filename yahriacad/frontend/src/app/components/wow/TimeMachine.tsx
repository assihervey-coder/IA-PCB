"use client";

/**
 * TimeMachine — voyage dans le temps du design (Pack WOW).
 * Capture des instantanés carte + règles, compare n'importe quelle capture
 * à l'état courant (diff structurel lisible) et restaure en un clic — la
 * restauration capture d'abord l'état actuel, donc le voyage est toujours
 * réversible.
 */
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { SnapshotDiff, SnapshotMeta } from "@/lib/api/types";
import { Modal } from "@/app/components/modals/Modal";
import { Button } from "@/app/components/buttons/Button";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

const CHANGE_CLASS: Record<string, string> = {
  added: "add",
  removed: "del",
  moved: "move",
  changed: "chg",
};

const CHANGE_LABEL: Record<string, string> = {
  added: "+",
  removed: "−",
  moved: "→",
  changed: "≠",
};

const timeFmt = new Intl.DateTimeFormat("fr-FR", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

export function TimeMachine({ onClose }: { onClose: () => void }) {
  const [snapshots, setSnapshots] = useState<SnapshotMeta[]>([]);
  const [diff, setDiff] = useState<SnapshotDiff | null>(null);
  const [busy, setBusy] = useState(false);
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const refreshLayout = useProjectStore((s) => s.refreshLayout);
  const pushHistory = useProjectStore((s) => s.pushHistory);
  const pushToast = useUIStore((s) => s.pushToast);

  const reload = useCallback(async () => {
    if (!projectId) return;
    try {
      const res = await api.listSnapshots(projectId);
      setSnapshots(res.snapshots);
    } catch {
      pushToast("Time Machine indisponible (backend hors ligne ?)", "error");
    }
  }, [projectId, pushToast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const capture = useCallback(async () => {
    if (!projectId || busy) return;
    setBusy(true);
    try {
      await api.captureSnapshot(projectId, "");
      pushToast("📸 Instantané capturé.", "success");
      await reload();
    } catch {
      pushToast("Capture impossible (besoin d'une carte).", "error");
    } finally {
      setBusy(false);
    }
  }, [busy, projectId, pushToast, reload]);

  const showDiff = useCallback(
    async (snapshotId: string) => {
      if (!projectId) return;
      setDiff(null);
      setBusy(true);
      try {
        setDiff(await api.snapshotDiff(projectId, snapshotId));
      } catch {
        pushToast("Diff impossible pour cette capture.", "error");
      } finally {
        setBusy(false);
      }
    },
    [projectId, pushToast],
  );

  const restore = useCallback(
    async (snapshotId: string, label: string) => {
      if (!projectId || busy) return;
      setBusy(true);
      try {
        pushHistory(); // sécurité supplémentaire côté client (undo local)
        await api.restoreSnapshot(projectId, snapshotId);
        await refreshLayout();
        pushToast(`⏪ Restauré : « ${label} » (état actuel sauvegardé avant).`, "success");
        setDiff(null);
        await reload();
      } catch {
        pushToast("Restauration impossible.", "error");
      } finally {
        setBusy(false);
      }
    },
    [busy, projectId, pushHistory, pushToast, refreshLayout, reload],
  );

  return (
    <Modal title="🕰️ Time Machine" onClose={onClose} maxWidth={620}>
      <div className="timemachine">
        <div className="tm-actions">
          <Button size="sm" onClick={() => void capture()} disabled={busy}>
            📸 Capturer l'état actuel
          </Button>
          <span className="tm-hint">20 instantanés max par projet (anneau).</span>
        </div>

        {snapshots.length === 0 && !busy && (
          <p className="tm-empty">
            Aucun instantané. Capturez l'état actuel avant une modification risquée — vous pourrez
            comparer et revenir à tout moment.
          </p>
        )}

        <ul className="tm-list">
          {snapshots.map((s) => (
            <li key={s.id} className="tm-item">
              <div className="tm-item-main">
                <span className="tm-item-label">{s.label}</span>
                <span className="tm-item-time">{timeFmt.format(new Date(s.at))}</span>
                <span className="tm-item-stats">
                  {s.board.components} comp. · {s.board.tracks} pistes · {s.board.vias} vias ·{" "}
                  {s.board.length_mm.toFixed(0)} mm
                </span>
              </div>
              <div className="tm-item-actions">
                <Button size="sm" variant="ghost" onClick={() => void showDiff(s.id)} disabled={busy}>
                  Comparer
                </Button>
                <Button size="sm" variant="ghost" onClick={() => void restore(s.id, s.label)} disabled={busy}>
                  Restaurer
                </Button>
              </div>
            </li>
          ))}
        </ul>

        {diff && (
          <div className="tm-diff">
            <h3>
              Diff avec « {diff.snapshot_label} » — {diff.summary}
            </h3>
            {!diff.changed && <p className="tm-diff-none">Aucune différence détectée.</p>}
            <ul className="tm-diff-list">
              {diff.entries.map((e, i) => (
                <li key={i} className={`tm-diff-entry ${CHANGE_CLASS[e.change] ?? "chg"}`}>
                  <span className="tm-diff-badge">{CHANGE_LABEL[e.change] ?? "≠"}</span>
                  <span className="tm-diff-kind">{e.kind}</span>
                  <span className="tm-diff-detail">{e.detail}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </Modal>
  );
}
