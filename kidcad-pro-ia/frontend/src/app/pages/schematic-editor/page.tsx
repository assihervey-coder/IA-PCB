"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api, isBackendDown } from "@/lib/api/rest-client";
import type { ERCResult, ERCViolation, LayoutComponent, LayoutData } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { Button } from "@/app/components/buttons/Button";
import { Modal } from "@/app/components/modals/Modal";
import {
  ComponentPropertiesForm,
  type ComponentPropertiesPatch,
} from "@/app/components/forms/ComponentPropertiesForm";
import {
  estimateFootprintSize,
  footprintPadOffsets,
} from "@/lib/utils/geometry-utils";
import { colorForNet, percentFormat } from "@/lib/utils/converters";

// Feuille A-paysage abstraite (unités : mm, 1 unité SVG = 1 mm).
const SHEET_W = 240;
const SHEET_H = 160;
const GRID_STEP = 2.54;
const MIN_ZOOM = 1;
const MAX_ZOOM = 8;

interface ViewState {
  zoom: number;
  panX: number;
  panY: number;
}

/** ERC local (mode démo) : vérifications simples sur les données de layout. */
function computeLocalERC(layout: LayoutData): ERCResult {
  const violations: ERCViolation[] = [];
  const seen = new Set<string>();
  for (const comp of layout.components) {
    if (seen.has(comp.ref)) {
      violations.push({
        code: "ERC_DUPLICATE_REF",
        severity: "error",
        message: `Référence dupliquée « ${comp.ref} ».`,
        component_ref: comp.ref,
      });
    }
    seen.add(comp.ref);
  }
  for (const net of layout.nets) {
    if (net.pad_count < 2) {
      violations.push({
        code: "ERC_SINGLE_PIN_NET",
        severity: "warning",
        message: `Le net « ${net.name} » ne possède qu'une seule connexion.`,
        component_ref: "",
      });
    }
  }
  return { passed: violations.length === 0, violations };
}

export default function SchematicEditorPage() {
  const layout = useProjectStore((s) => s.layout);
  const currentProject = useProjectStore((s) => s.currentProject);
  const demoMode = useProjectStore((s) => s.demoMode);
  const erc = useProjectStore((s) => s.erc);
  const setERC = useProjectStore((s) => s.setERC);
  const openProject = useProjectStore((s) => s.openProject);
  const loadDemoData = useProjectStore((s) => s.loadDemoData);
  const pushToast = useUIStore((s) => s.pushToast);

  const [projectId, setProjectId] = useState<string | null>(null);
  const [view, setView] = useState<ViewState>({ zoom: 1.6, panX: 8, panY: 8 });
  const [gridOn, setGridOn] = useState(true);
  const [selected, setSelected] = useState<LayoutComponent | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [ercBusy, setErcBusy] = useState(false);

  const svgRef = useRef<SVGSVGElement | null>(null);
  const dragRef = useRef<{ startX: number; startY: number; panX: number; panY: number } | null>(null);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("project");
    setProjectId(id);
    if (id) {
      void openProject(id);
    } else {
      void loadDemoData();
    }
  }, [openProject, loadDemoData]);

  // Zoom molette (listener natif non passif pour pouvoir bloquer le défilement).
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15;
      setView((v) => {
        const zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, v.zoom * factor));
        const cx = v.panX + SHEET_W / (2 * v.zoom);
        const cy = v.panY + SHEET_H / (2 * v.zoom);
        return { zoom, panX: cx - SHEET_W / (2 * zoom), panY: cy - SHEET_H / (2 * zoom) };
      });
    };
    svg.addEventListener("wheel", onWheel, { passive: false });
    return () => svg.removeEventListener("wheel", onWheel);
  }, []);

  const zoomBy = (factor: number) => {
    setView((v) => {
      const zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, v.zoom * factor));
      const cx = v.panX + SHEET_W / (2 * v.zoom);
      const cy = v.panY + SHEET_H / (2 * v.zoom);
      return { zoom, panX: cx - SHEET_W / (2 * zoom), panY: cy - SHEET_H / (2 * zoom) };
    });
  };

  const resetView = () => setView({ zoom: 1.6, panX: 8, panY: 8 });

  const handlePointerDown = (e: React.PointerEvent<SVGSVGElement>) => {
    if (e.button !== 0) return;
    dragRef.current = { startX: e.clientX, startY: e.clientY, panX: view.panX, panY: view.panY };
    svgRef.current?.setPointerCapture(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current;
    const svg = svgRef.current;
    if (!drag || !svg) return;
    const rect = svg.getBoundingClientRect();
    const mmPerPx = SHEET_W / view.zoom / Math.max(rect.width, 1);
    setView((v) => ({
      ...v,
      panX: drag.panX - (e.clientX - drag.startX) * mmPerPx,
      panY: drag.panY - (e.clientY - drag.startY) * mmPerPx,
    }));
  };

  const handlePointerUp = () => {
    dragRef.current = null;
  };

  const applyProperties = async (patch: ComponentPropertiesPatch) => {
    if (!layout || !selected) return;
    const next: LayoutData = {
      ...layout,
      components: layout.components.map((c) =>
        c.ref === selected.ref
          ? {
              ...c,
              footprint: patch.footprint,
              x: patch.x,
              y: patch.y,
              rotation: patch.rotation,
              fixed: patch.fixed,
            }
          : c,
      ),
    };
    useProjectStore.setState({ layout: next });
    setValues((prev) => ({ ...prev, [selected.ref]: patch.value }));
    setSelected(null);
    if (projectId && !demoMode) {
      try {
        await api.putLayout(projectId, next);
        pushToast("Modifications enregistrées sur le backend.", "success");
      } catch {
        pushToast("Modification appliquée localement (enregistrement distant impossible).", "info");
      }
    }
  };

  const runErc = async () => {
    if (!layout) return;
    setErcBusy(true);
    try {
      if (projectId && !demoMode) {
        setERC(await api.runERC(projectId));
      } else {
        setERC(computeLocalERC(layout));
      }
    } catch (err) {
      if (isBackendDown(err)) {
        setERC(computeLocalERC(layout));
      } else {
        pushToast("Échec de l'exécution de l'ERC.", "error");
      }
    } finally {
      setErcBusy(false);
    }
  };

  const vbW = SHEET_W / view.zoom;
  const vbH = SHEET_H / view.zoom;

  const netRows = useMemo(() => layout?.nets ?? [], [layout]);
  const projectName = currentProject?.name ?? "Projet sans nom";

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Éditeur de schéma</h1>
          <p className="page-subtitle">
            {projectName}
            {projectId ? ` — projet ${projectId}` : " — données de démonstration"}
          </p>
        </div>
      </div>

      {demoMode ? (
        <div className="demo-banner" data-testid="demo-banner" role="status">
          Mode démo : backend injoignable, carte d&apos;exemple affichée.
        </div>
      ) : null}

      <div className="editor-layout">
        <section className="canvas-wrap panel">
          <svg
            ref={svgRef}
            data-testid="schematic-canvas"
            className="sch-svg"
            viewBox={`${view.panX} ${view.panY} ${vbW} ${vbH}`}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerLeave={handlePointerUp}
            role="img"
            aria-label="Feuille de schéma"
          >
            <defs>
              <pattern id="sch-grid" width={GRID_STEP} height={GRID_STEP} patternUnits="userSpaceOnUse">
                <path
                  d={`M ${GRID_STEP} 0 H 0 V ${GRID_STEP}`}
                  fill="none"
                  className="sch-grid-line"
                  strokeWidth={0.12}
                />
              </pattern>
            </defs>

            <rect x={0} y={0} width={SHEET_W} height={SHEET_H} className="sch-paper" />
            {gridOn ? <rect x={0} y={0} width={SHEET_W} height={SHEET_H} fill="url(#sch-grid)" /> : null}
            <rect x={4} y={4} width={SHEET_W - 8} height={SHEET_H - 8} className="sch-frame" fill="none" />

            {/* Cartouche */}
            <g>
              <rect x={SHEET_W - 96} y={SHEET_H - 28} width={90} height={24} className="sch-tb-box" strokeWidth={0.3} />
              <text x={SHEET_W - 92} y={SHEET_H - 20.5} fontSize={3.2} fontWeight={700} fill="#33415c" fontFamily="inherit">
                KidCAD-Pro-IA — Schéma
              </text>
              <text x={SHEET_W - 92} y={SHEET_H - 15} fontSize={3} fill="#4a5b7d" fontFamily="inherit">
                {projectName}
              </text>
              <text x={SHEET_W - 92} y={SHEET_H - 9.5} fontSize={2.6} fill="#6a7c9e" fontFamily="inherit">
                Unités : mm — grille {GRID_STEP} mm
              </text>
            </g>

            {/* Composants */}
            {(layout?.components ?? []).map((comp) => {
              const size = estimateFootprintSize(comp.footprint);
              const pads = footprintPadOffsets(size);
              const value = values[comp.ref];
              return (
                <g
                  key={comp.ref}
                  className={`sch-component${selected?.ref === comp.ref ? " selected" : ""}`}
                  transform={`translate(${comp.x} ${comp.y}) rotate(${comp.rotation})`}
                  onPointerDown={(e) => {
                    e.stopPropagation();
                    setSelected(comp);
                  }}
                >
                  <rect
                    x={-size.w / 2}
                    y={-size.h / 2}
                    width={size.w}
                    height={size.h}
                    rx={0.5}
                    className="sch-body"
                  />
                  {pads.map((pad, i) => (
                    <rect key={i} x={pad.x - 0.3} y={pad.y - 0.35} width={0.6} height={0.7} className="sch-pin" />
                  ))}
                  <text y={-size.h / 2 - 1.1} textAnchor="middle" className="sch-ref">
                    {comp.ref}
                  </text>
                  {value ? (
                    <text y={size.h / 2 + 2.6} textAnchor="middle" className="sch-value">
                      {value}
                    </text>
                  ) : null}
                </g>
              );
            })}
          </svg>

          {!layout || layout.components.length === 0 ? (
            <div className="hint-empty">
              Aucun composant à afficher. Importez un fichier depuis la page Export ou lancez le placement IA
              depuis l&apos;éditeur PCB.
            </div>
          ) : null}

          <div className="canvas-status">
            <span>Zoom : {percentFormat(view.zoom * 62.5)}</span>
            <span>Grille : {gridOn ? `${GRID_STEP} mm` : "masquée"}</span>
            <span>
              {layout?.components.length ?? 0} composants — {netRows.length} nets
            </span>
            {selected ? <span>Sélection : {selected.ref}</span> : <span className="muted">Cliquez sur un composant pour l&apos;éditer</span>}
          </div>
        </section>

        <aside className="side-panel">
          <div className="side-box panel">
            <h3 className="panel-title">Affichage</h3>
            <div className="toolbar" style={{ border: "none", background: "transparent", padding: 0, gap: 6 }}>
              <Button size="sm" variant="secondary" onClick={() => zoomBy(1.25)} aria-label="Zoom avant">
                Zoom +
              </Button>
              <Button size="sm" variant="secondary" onClick={() => zoomBy(0.8)} aria-label="Zoom arrière">
                Zoom −
              </Button>
              <Button size="sm" variant="ghost" onClick={resetView}>
                Ajuster
              </Button>
            </div>
            <label className="field-checkbox" style={{ marginTop: 10, marginBottom: 0 }}>
              <input type="checkbox" checked={gridOn} onChange={(e) => setGridOn(e.target.checked)} />
              Afficher la grille
            </label>
          </div>

          <div className="side-box panel">
            <h3 className="panel-title">Nets ({netRows.length})</h3>
            <ul className="net-list">
              {netRows.map((net) => (
                <li key={net.name} className="net-item" style={{ cursor: "default" }}>
                  <span className="net-dot" style={{ background: colorForNet(net.name) }} />
                  <span className="net-name">{net.name}</span>
                  <span className="net-count">
                    {net.pad_count} connexion{net.pad_count > 1 ? "s" : ""}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>

      <section className="erc-panel panel">
        <div className="erc-header">
          <h3>Contrôle ERC</h3>
          <div className="page-actions">
            {erc ? (
              <span className="erc-summary">
                {erc.violations.length} violation{erc.violations.length > 1 ? "s" : ""} —{" "}
                {erc.passed ? "conforme" : "à corriger"}
              </span>
            ) : (
              <span className="erc-summary">Vérification des règles électriques</span>
            )}
            <Button data-testid="erc-run-btn" onClick={() => void runErc()} disabled={ercBusy || !layout}>
              {ercBusy ? "Analyse…" : "Lancer l'ERC"}
            </Button>
          </div>
        </div>

        {erc ? (
          <>
            <div className={`pass-banner ${erc.passed ? "ok" : "fail"}`}>
              {erc.passed
                ? "ERC conforme : aucune violation détectée."
                : `ERC : ${erc.violations.length} violation(s) détectée(s).`}
            </div>
            {erc.violations.length > 0 ? (
              <ul className="violation-list" style={{ marginTop: 10 }}>
                {erc.violations.map((v, i) => (
                  <li key={i} className="violation-item">
                    <span className={`sev sev-${v.severity}`}>
                      {v.severity === "error" ? "Erreur" : "Avertissement"}
                    </span>
                    <div>
                      <strong>{v.code}</strong>
                      <span> — {v.message}</span>
                      {v.component_ref ? (
                        <div className="violation-loc">Composant : {v.component_ref}</div>
                      ) : null}
                    </div>
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : (
          <p className="muted">
            L&apos;ERC analyse les nets (connexion unique, références dupliquées…) et signale les anomalies.
          </p>
        )}
      </section>

      {selected ? (
        <Modal title={`Propriétés — ${selected.ref}`} onClose={() => setSelected(null)}>
          <ComponentPropertiesForm
            component={selected}
            value={values[selected.ref] ?? ""}
            onSubmit={(patch) => void applyProperties(patch)}
            onCancel={() => setSelected(null)}
          />
        </Modal>
      ) : null}
    </div>
  );
}
