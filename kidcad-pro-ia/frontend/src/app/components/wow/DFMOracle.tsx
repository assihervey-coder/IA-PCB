"use client";

/**
 * DFMOracle — oracle de coût & rendement de fabrication (Pack WOW).
 * Estime le prix unitaire à 3 volumes (prototype / pilote / série), le
 * rendement premier passage et liste les surtaxes expliquées + risques
 * process. Les experts y voient une porte de devis instantanée.
 */
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { DFMEstimate } from "@/lib/api/types";
import { Modal } from "@/app/components/modals/Modal";
import { Button } from "@/app/components/buttons/Button";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

const RISK_CLASS: Record<string, string> = {
  faible: "ok",
  moyen: "warn",
  "élevé": "bad",
};

const eur = (v: number) =>
  new Intl.NumberFormat("fr-FR", { style: "currency", currency: "EUR", maximumFractionDigits: 2 }).format(v);

/** Jauge demi-circulaire du rendement (SVG pur). */
function YieldGauge({ value }: { value: number }) {
  const r = 54;
  const cx = 70;
  const cy = 62;
  // Demi-cercle de 180° : ratio 0..1 -> angle -180°..0°.
  const ratio = Math.max(0, Math.min(1, value / 100));
  const angle = Math.PI * (1 - ratio);
  const nx = cx + Math.cos(angle) * r;
  const ny = cy - Math.sin(angle) * r;
  const largeArc = 0;
  const cls = value >= 95 ? "ok" : value >= 85 ? "warn" : "bad";
  return (
    <svg viewBox="0 0 140 84" className="dfm-gauge" role="img" aria-label={`Rendement ${value} %`}>
      <path
        d={`M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`}
        className="dfm-gauge-track"
      />
      <path
        d={`M ${cx - r} ${cy} A ${r} ${r} 0 ${largeArc} 1 ${nx.toFixed(1)} ${ny.toFixed(1)}`}
        className={`dfm-gauge-value ${cls}`}
      />
      <text x={cx} y={cy - 12} textAnchor="middle" className={`dfm-gauge-num ${cls}`}>
        {value.toFixed(1)} %
      </text>
      <text x={cx} y={cy + 4} textAnchor="middle" className="dfm-gauge-label">
        rendement 1er passage
      </text>
    </svg>
  );
}

export function DFMOracle({ onClose }: { onClose: () => void }) {
  const [estimate, setEstimate] = useState<DFMEstimate | null>(null);
  const [busy, setBusy] = useState(true);
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const pushToast = useUIStore((s) => s.pushToast);

  const estimateNow = useCallback(async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      setEstimate(await api.dfmEstimate(projectId));
    } catch {
      pushToast("Oracle DFM indisponible (backend hors ligne ou carte absente ?)", "error");
    } finally {
      setBusy(false);
    }
  }, [projectId, pushToast]);

  useEffect(() => {
    void estimateNow();
  }, [estimateNow]);

  return (
    <Modal title="💰 Oracle DFM — coût & rendement" onClose={onClose} maxWidth={640}>
      {busy && <p className="dfm-loading">Analyse de manufacturabilité en cours…</p>}

      {!busy && estimate && (
        <div className="dfm">
          <div className="dfm-price-cards">
            {estimate.unit_prices.map((u) => (
              <div key={u.qty} className={`dfm-price-card ${u.qty === 100 ? "featured" : ""}`}>
                <span className="dfm-price-label">{u.label}</span>
                <span className="dfm-price-unit">{eur(u.unit_eur)}</span>
                <span className="dfm-price-sub">/ carte · {u.qty} pcs</span>
                <span className="dfm-price-total">Total : {eur(u.total_eur)}</span>
              </div>
            ))}
          </div>

          <div className="dfm-middle">
            <YieldGauge value={estimate.first_pass_yield_pct} />
            <div className="dfm-facts">
              <div className="dfm-fact">
                <span className={`dfm-risk ${RISK_CLASS[estimate.defect_risk] ?? "warn"}`}>
                  Risque défauts : {estimate.defect_risk}
                </span>
              </div>
              <div className="dfm-fact">{estimate.layers} couches · {estimate.area_dm2.toFixed(2)} dm²</div>
              <div className="dfm-fact">
                {estimate.via_count} vias ({estimate.via_density_per_cm2.toFixed(1)}/cm²) ·{" "}
                {estimate.components} composants · {estimate.bom_lines} lignes BOM
              </div>
              <div className="dfm-fact">
                Géométrie min : piste {estimate.min_track_mm.toFixed(2)} mm · perçage{" "}
                {estimate.min_drill_mm.toFixed(2)} mm
              </div>
            </div>
          </div>

          {estimate.surcharges.length > 0 && (
            <div className="dfm-section">
              <h3>Décomposition du prix</h3>
              <ul className="dfm-surcharge-list">
                {estimate.surcharges.map((s, i) => (
                  <li key={i}>
                    {s.label} <span className="dfm-surcharge-pct">×{s.pct.toFixed(2)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {estimate.risk_flags.length > 0 && (
            <div className="dfm-section">
              <h3>Risques process</h3>
              <ul className="dfm-risk-list">
                {estimate.risk_flags.map((r) => (
                  <li key={r.code}>
                    <span className="dfm-risk-chip">+{r.impact_pct.toFixed(0)} %</span> {r.message}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="dfm-section">
            <h3>Conseils d'optimisation</h3>
            <ul className="dfm-advice-list">
              {estimate.advice.map((a, i) => (
                <li key={i}>{a}</li>
              ))}
            </ul>
          </div>

          <div className="dfm-footer-note">
            Modèle de coût transparent (détaillé dans le guide) — estimation indicative, hors
            composants et assemblage.
            <Button size="sm" variant="ghost" onClick={() => void estimateNow()} disabled={busy}>
              Recalculer
            </Button>
          </div>
        </div>
      )}

      {!busy && !estimate && (
        <p className="dfm-loading">Ouvrez un projet avec une carte pour estimer son coût.</p>
      )}
    </Modal>
  );
}
