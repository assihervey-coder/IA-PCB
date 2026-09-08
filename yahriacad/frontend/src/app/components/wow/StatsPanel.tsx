"use client";

/**
 * StatsPanel — tableau de bord statistique live (good-to-have).
 * S'ouvre en panneau latéral : compteurs, longueur de cuivre, occupation,
 * top nets, répartition par couche et par classe. Rafraîchi à chaque
 * ouverture (calcul côté backend, quasi instantané).
 */
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { StatsReport } from "@/lib/api/types";
import { Button } from "@/app/components/buttons/Button";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

const barWidth = (v: number, max: number) => (max > 0 ? Math.max(2, (v / max) * 100) : 0);

export function StatsPanel({ onClose }: { onClose: () => void }) {
  const [report, setReport] = useState<StatsReport | null>(null);
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const pushToast = useUIStore((s) => s.pushToast);

  const load = useCallback(async () => {
    if (!projectId) return;
    try {
      setReport(await api.stats(projectId));
    } catch {
      pushToast("Statistiques indisponibles (besoin d'une carte).", "error");
    }
  }, [projectId, pushToast]);

  useEffect(() => {
    void load();
  }, [load]);

  const maxNetLen = report ? Math.max(...report.top_nets.map((n) => n.length_mm), 1) : 1;
  const maxLayerLen = report ? Math.max(...report.layer_usage.map((l) => l.length_mm), 1) : 1;

  return (
    <aside className="stats-panel" aria-label="Statistiques du design">
      <div className="stats-head">
        <h3>📊 Statistiques</h3>
        <div>
          <Button size="sm" variant="ghost" onClick={() => void load()}>
            ⟳
          </Button>
          <Button size="sm" variant="ghost" onClick={onClose} aria-label="Fermer les statistiques">
            ✕
          </Button>
        </div>
      </div>

      {!report && <p className="stats-loading">Chargement…</p>}

      {report && (
        <>
          <div className="stats-grid">
            <div className="stats-cell">
              <span className="stats-num">{report.components}</span>
              <span className="stats-label">composants</span>
            </div>
            <div className="stats-cell">
              <span className="stats-num">
                {report.tracked_nets}/{report.nets}
              </span>
              <span className="stats-label">nets routés</span>
            </div>
            <div className="stats-cell">
              <span className="stats-num">{report.total_length_mm.toFixed(0)}</span>
              <span className="stats-label">mm de cuivre</span>
            </div>
            <div className="stats-cell">
              <span className="stats-num">{report.vias}</span>
              <span className="stats-label">vias</span>
            </div>
            <div className="stats-cell">
              <span className="stats-num">{report.utilization_pct.toFixed(0)} %</span>
              <span className="stats-label">occupation</span>
            </div>
            <div className="stats-cell">
              <span className="stats-num">{report.board.area_cm2.toFixed(1)}</span>
              <span className="stats-label">cm² ({report.board.layer_count} couches)</span>
            </div>
          </div>

          {report.layer_usage.length > 0 && (
            <section className="stats-section">
              <h4>Cuivre par couche</h4>
              {report.layer_usage.map((l) => (
                <div key={l.layer} className="stats-bar-row">
                  <span className="stats-bar-name">{l.name}</span>
                  <span className="stats-bar-track">
                    <span className="stats-bar-fill" style={{ width: `${barWidth(l.length_mm, maxLayerLen)}%` }} />
                  </span>
                  <span className="stats-bar-value">{l.length_mm.toFixed(0)} mm</span>
                </div>
              ))}
            </section>
          )}

          {report.top_nets.length > 0 && (
            <section className="stats-section">
              <h4>Top nets les plus longs</h4>
              {report.top_nets.map((n) => (
                <div key={n.name} className="stats-bar-row">
                  <span className="stats-bar-name">{n.name}</span>
                  <span className="stats-bar-track">
                    <span className="stats-bar-fill" style={{ width: `${barWidth(n.length_mm, maxNetLen)}%` }} />
                  </span>
                  <span className="stats-bar-value">
                    {n.length_mm.toFixed(0)} mm{n.vias > 0 ? ` · ${n.vias}v` : ""}
                  </span>
                </div>
              ))}
            </section>
          )}

          {report.net_classes.length > 0 && (
            <section className="stats-section">
              <h4>Par classe de nets</h4>
              <ul className="stats-class-list">
                {report.net_classes.map((c) => (
                  <li key={c.class}>
                    <span className="stats-class-name">{c.class}</span>
                    <span>
                      {c.nets} net(s) · {c.length_mm.toFixed(0)} mm
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </>
      )}
    </aside>
  );
}
