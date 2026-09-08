"use client";

/**
 * DiffImpedance — oracle d'impédance différentielle par classe de nets.
 * Détecte les paires (X+/X-, X_P/X_N, XP/XN), calcule Zodd/Zeven/Zdiff/Zcom
 * en microstrip couplé (approx. Bogatin), mesure le skew intra-paire et
 * propose la largeur ou l'écart à appliquer pour atteindre la cible de la
 * classe (90 Ω USB, 100 Ω Ethernet/PCIe/LVDS…). Cible surchargeable.
 */
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { DiffClassReport, DiffImpedanceResult, DiffPairReport } from "@/lib/api/types";
import { Modal } from "@/app/components/modals/Modal";
import { Button } from "@/app/components/buttons/Button";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

const CRIT_CLASS: Record<string, string> = { ok: "ok", warning: "warn", critical: "bad" };

const ohms = (v: number) => `${v.toFixed(0)} Ω`;

/** Badge de conformité de la paire (±bande de tolérance). */
function PairBadge({ pair }: { pair: DiffPairReport }) {
  const cls = !pair.routed ? "warn" : pair.in_tolerance ? "ok" : CRIT_CLASS[pair.criticality] ?? "warn";
  const label = !pair.routed ? "non routée" : pair.in_tolerance ? "conforme" : `${pair.error_pct > 0 ? "+" : ""}${pair.error_pct.toFixed(1)} %`;
  return <span className={`imp-badge ${cls}`}>{label}</span>;
}

function PairRow({ pair, tolerance }: { pair: DiffPairReport; tolerance: number }) {
  return (
    <div className="imp-pair">
      <div className="imp-pair-head">
        <span className="imp-pair-name">
          {pair.net_plus} <span className="imp-vs">/</span> {pair.net_minus}
        </span>
        <PairBadge pair={pair} />
      </div>
      <div className="imp-pair-nums">
        <span title="Impédance différentielle">Z<sub>diff</sub> <strong>{ohms(pair.zdiff_ohms)}</strong></span>
        <span title="Mode impair (par branche)">Z<sub>odd</sub> {ohms(pair.zodd_ohms)}</span>
        <span title="Mode pair">Z<sub>even</sub> {ohms(pair.zeven_ohms)}</span>
        <span title="Mode commun">Z<sub>com</sub> {ohms(pair.zcom_ohms)}</span>
        <span title="Piste simple équivalente">Z<sub>0</sub> {ohms(pair.z0_ohms)}</span>
      </div>
      <div className="imp-pair-geo">
        piste {pair.width_mm.toFixed(2)} mm · écart {pair.gap_mm.toFixed(2)} mm ·
        skew {pair.skew_mm.toFixed(2)} mm ({pair.skew_ps.toFixed(0)} ps) ·
        cible {ohms(pair.target_ohms)} (±{tolerance.toFixed(0)} %)
      </div>
      {pair.routed && !pair.in_tolerance ? (
        <div className="imp-fix">
          Pour atteindre {ohms(pair.target_ohms)} : largeur{" "}
          <strong>{pair.recommended_width_mm.toFixed(2)} mm</strong> à écart constant, ou écart{" "}
          <strong>{pair.recommended_gap_mm.toFixed(2)} mm</strong> à largeur constante.
        </div>
      ) : null}
      {pair.advices.length > 0 ? (
        <ul className="imp-advices">
          {pair.advices.map((a, i) => (
            <li key={i}>{a}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ClassBlock({ report, tolerance }: { report: DiffClassReport; tolerance: number }) {
  return (
    <div className="imp-class">
      <h3 className="imp-class-title">
        Classe « {report.class} » — cible {ohms(report.target_ohms)} · {report.pairs.length} paire(s)
      </h3>
      {report.pairs.map((p) => (
        <PairRow key={`${p.net_plus}|${p.net_minus}`} pair={p} tolerance={tolerance} />
      ))}
    </div>
  );
}

export function DiffImpedance({ onClose }: { onClose: () => void }) {
  const [result, setResult] = useState<DiffImpedanceResult | null>(null);
  const [busy, setBusy] = useState(true);
  const projectId = useProjectStore((s) => s.currentProject?.id ?? null);
  const pushToast = useUIStore((s) => s.pushToast);

  const analyze = useCallback(async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      setResult(await api.impedance(projectId, {}));
    } catch {
      pushToast("Impédance différentielle indisponible (carte ou schéma absent ?)", "error");
    } finally {
      setBusy(false);
    }
  }, [projectId, pushToast]);

  useEffect(() => {
    void analyze();
  }, [analyze]);

  return (
    <Modal title="⚡ Impédance différentielle — paires par classe" onClose={onClose} maxWidth={620}>
      {busy && <p className="dfm-loading">Analyse des paires différentielles en cours…</p>}

      {!busy && result && (
        <div className="imp">
          <div className={`pass-banner ${result.analyzed_pairs === result.in_tolerance_count && result.analyzed_pairs > 0 ? "ok" : "warn"}`}>
            {result.summary}
          </div>

          <div className="imp-stackup">
            Stackup FR4 : εr {result.stackup.er_relative} · prépreg {result.stackup.dielectric_height_mm} mm ·
            cuivre {result.stackup.copper_thickness_mm} mm · v≈{result.stackup.propagation_mm_per_ps} mm/ps ·
            tolérance ±{result.tolerance_pct} % · {result.duration_ms} ms
          </div>

          {result.classes.map((c) => (
            <ClassBlock key={c.class} report={c} tolerance={result.tolerance_pct} />
          ))}

          <p className="imp-note">
            Paires reconnues : X+/X-, X_P/X_N, XP/XN (même classe de nets). Nommez vos nets en conséquence pour
            qu&apos;une paire soit analysée ; les cibles (90 Ω USB, 100 Ω Ethernet…) sont ajustables via l&apos;API
            POST /impedance.
          </p>
        </div>
      )}

      <div className="page-actions" style={{ marginTop: 12 }}>
        <Button size="sm" variant="secondary" onClick={() => void analyze()} disabled={busy}>
          Relancer l&apos;analyse
        </Button>
        <Button size="sm" variant="ghost" onClick={onClose}>
          Fermer
        </Button>
      </div>
    </Modal>
  );
}
