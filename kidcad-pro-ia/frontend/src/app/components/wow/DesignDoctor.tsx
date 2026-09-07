"use client";

/**
 * DesignDoctor — audit global noté du design (Pack WOW).
 * Fusionne DRC, ERC, intégrité signal, thermique et routage/DFM en une note
 * 0-100, un grade, un radar 5 axes (SVG pur) et des ordonnances
 * priorisées avec gain de points estimé.
 */
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { DoctorReport } from "@/lib/api/types";
import { Modal } from "@/app/components/modals/Modal";
import { Button } from "@/app/components/buttons/Button";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

const GRADE_CLASS: Record<string, string> = {
  "A+": "excellent",
  A: "excellent",
  B: "good",
  C: "fair",
  D: "poor",
};

/** Radar SVG 5 axes du rapport doctor. */
function Radar({ report }: { report: DoctorReport }) {
  const size = 220;
  const cx = size / 2;
  const cy = size / 2;
  const r = 78;
  const n = report.axes.length;
  if (n === 0) return null;

  const angle = (i: number) => (Math.PI * 2 * i) / n - Math.PI / 2;
  const point = (i: number, ratio: number) => ({
    x: cx + Math.cos(angle(i)) * r * ratio,
    y: cy + Math.sin(angle(i)) * r * ratio,
  });

  const rings = [0.25, 0.5, 0.75, 1].map((ratio, idx) => {
    const pts = report.axes
      .map((_, i) => {
        const p = point(i, ratio);
        return `${p.x.toFixed(1)},${p.y.toFixed(1)}`;
      })
      .join(" ");
    return <polygon key={idx} points={pts} className="doctor-radar-ring" />;
  });

  const dataPts = report.axes
    .map((a, i) => {
      const p = point(i, Math.max(0.04, Math.min(1, a.score / a.max)));
      return `${p.x.toFixed(1)},${p.y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <svg viewBox={`0 0 ${size} ${size}`} className="doctor-radar" role="img" aria-label="Radar des 5 axes">
      {rings}
      {report.axes.map((a, i) => {
        const p = point(i, 1);
        const label = point(i, 1.22);
        return (
          <g key={a.axe}>
            <line x1={cx} y1={cy} x2={p.x} y2={p.y} className="doctor-radar-spoke" />
            <text
              x={label.x}
              y={label.y}
              className="doctor-radar-label"
              textAnchor={label.x > cx + 6 ? "start" : label.x < cx - 6 ? "end" : "middle"}
              dominantBaseline="middle"
            >
              {a.axe}
            </text>
          </g>
        );
      })}
      <polygon points={dataPts} className="doctor-radar-data" />
      {report.axes.map((a, i) => {
        const p = point(i, Math.max(0.04, Math.min(1, a.score / a.max)));
        return <circle key={a.axe} cx={p.x} cy={p.y} r={2.6} className="doctor-radar-dot" />;
      })}
    </svg>
  );
}

export function DesignDoctor({ onClose }: { onClose: () => void }) {
  const [report, setReport] = useState<DoctorReport | null>(null);
  const [busy, setBusy] = useState(true);
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const pushToast = useUIStore((s) => s.pushToast);

  const examine = useCallback(async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      setReport(await api.doctor(projectId));
    } catch {
      pushToast("Le Design Doctor n'a pas pu examiner la carte (backend hors ligne ?)", "error");
    } finally {
      setBusy(false);
    }
  }, [projectId, pushToast]);

  useEffect(() => {
    void examine();
  }, [examine]);

  return (
    <Modal title="🩺 Design Doctor" onClose={onClose} maxWidth={620}>
      {busy && <p className="doctor-loading">Examen de la carte en cours…</p>}

      {!busy && report && (
        <div className="doctor">
          <div className="doctor-summary">
            <div className={`doctor-grade ${GRADE_CLASS[report.grade] ?? "fair"}`}>
              <span className="doctor-grade-letter">{report.grade}</span>
              <span className="doctor-grade-score">{report.score.toFixed(1)}/100</span>
            </div>
            <Radar report={report} />
            <div className="doctor-verdict">
              <p>{report.verdict}</p>
              <p className="doctor-meta">
                {report.metrics.components} composants · {report.metrics.nets} nets ·{" "}
                {report.metrics.tracks} pistes · {report.metrics.total_length_mm.toFixed(0)} mm de
                cuivre · {report.metrics.utilization_pct.toFixed(0)} % d&apos;occupation
              </p>
              <Button size="sm" variant="ghost" onClick={() => void examine()} disabled={busy}>
                Réexaminer
              </Button>
            </div>
          </div>

          {report.prescriptions.length > 0 && (
            <div className="doctor-prescriptions">
              <h3>Ordonnances (par priorité)</h3>
              <ol className="doctor-prescription-list">
                {report.prescriptions.map((p, i) => (
                  <li key={i} className={`doctor-prescription prio-${p.priority}`}>
                    <div className="doctor-prescription-head">
                      <span className="doctor-prescription-title">{p.title}</span>
                      <span className="doctor-prescription-gain">+{p.gain_pts.toFixed(1)} pts</span>
                    </div>
                    <p className="doctor-prescription-detail">{p.detail}</p>
                    <span className="doctor-prescription-axe">{p.axe}</span>
                  </li>
                ))}
              </ol>
              {report.metrics.drc_violations > 0 && (
                <p className="doctor-hint">
                  💡 Le DRC Auto-Healer peut réparer {report.metrics.drc_violations} violation(s)
                  automatiquement — bouton 🩹 dans la barre d&apos;outils.
                </p>
              )}
            </div>
          )}

          {report.prescriptions.length === 0 && (
            <p className="doctor-clean">
              Aucune ordonnance : tous les axes sont au vert. Belle conception ! 🎉
            </p>
          )}
        </div>
      )}

      {!busy && !report && (
        <p className="doctor-loading">
          Ouvrez un projet avec une carte (placez ou importez d&apos;abord un design).
        </p>
      )}
    </Modal>
  );
}
