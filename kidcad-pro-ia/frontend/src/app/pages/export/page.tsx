"use client";

import { useEffect, useState } from "react";
import { api, isBackendDown, type ExportDownload } from "@/lib/api/rest-client";
import type { ImportResult } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { Button } from "@/app/components/buttons/Button";
import { downloadBlob } from "@/lib/utils/converters";

type ExportKind = "gerber" | "bom" | "step";

const DEFAULT_FILENAMES: Record<ExportKind, (projectId: string) => string> = {
  gerber: (id) => `kidcad-${id}-gerber.zip`,
  bom: (id) => `kidcad-${id}-bom.csv`,
  step: (id) => `kidcad-${id}.step`,
};

const ACCEPTED_IMPORT = ".kicad_pcb,.brd,.sch,.net,.kidcad.json";

export default function ExportPage() {
  const currentProject = useProjectStore((s) => s.currentProject);
  const demoMode = useProjectStore((s) => s.demoMode);
  const openProject = useProjectStore((s) => s.openProject);
  const loadDemoData = useProjectStore((s) => s.loadDemoData);
  const refreshLayout = useProjectStore((s) => s.refreshLayout);
  const loadProjects = useProjectStore((s) => s.loadProjects);
  const pushToast = useUIStore((s) => s.pushToast);

  const [projectId, setProjectId] = useState<string | null>(null);
  const [busyExport, setBusyExport] = useState<ExportKind | null>(null);
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("project");
    setProjectId(id);
    if (id) {
      void openProject(id);
    } else {
      void loadDemoData();
    }
  }, [openProject, loadDemoData]);

  const effectiveId = projectId ?? currentProject?.id ?? null;
  const query = effectiveId ? `?project=${encodeURIComponent(effectiveId)}` : "";

  const runExport = async (kind: ExportKind) => {
    if (!effectiveId || busyExport) return;
    setBusyExport(kind);
    try {
      let download: ExportDownload;
      if (kind === "gerber") {
        download = await api.exportGerber(effectiveId);
      } else if (kind === "bom") {
        download = await api.exportBOM(effectiveId);
      } else {
        download = await api.exportSTEP(effectiveId);
      }
      const filename = download.filename ?? DEFAULT_FILENAMES[kind](effectiveId);
      downloadBlob(download.blob, filename);
      pushToast(`Téléchargement lancé : ${filename}`, "success");
    } catch (err) {
      if (isBackendDown(err)) {
        pushToast("Mode démo : export indisponible sans backend.", "info");
      } else {
        pushToast("Échec de l'export. Vérifiez que le projet contient un layout.", "error");
      }
    } finally {
      setBusyExport(null);
    }
  };

  const handleImport = async (file: File) => {
    if (!effectiveId) {
      pushToast("Ouvrez un projet depuis le gestionnaire avant d'importer.", "error");
      return;
    }
    setImporting(true);
    setImportResult(null);
    try {
      const result = await api.importFile(effectiveId, file);
      setImportResult(result);
      pushToast(`Import ${result.format} réussi.`, "success");
    } catch (err) {
      if (isBackendDown(err)) {
        pushToast("Mode démo : import indisponible sans backend.", "info");
      } else {
        pushToast("Échec de l'import : format non reconnu ou fichier invalide.", "error");
      }
    } finally {
      setImporting(false);
    }
  };

  const handleRefresh = async () => {
    if (!effectiveId) return;
    await refreshLayout(effectiveId);
    await loadProjects();
    pushToast("Données du projet actualisées.", "info");
  };

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Export &amp; Import</h1>
          <p className="page-subtitle">
            {currentProject?.name ?? "Projet sans nom"}
            {effectiveId ? ` — projet ${effectiveId}` : " — aucun projet sélectionné"}
          </p>
        </div>
      </div>

      {demoMode ? (
        <div className="demo-banner" data-testid="demo-banner" role="status">
          Mode démo : backend injoignable — les exports et imports nécessitent le backend Go.
        </div>
      ) : null}

      {!effectiveId ? (
        <div className="empty-state">
          <strong>Aucun projet sélectionné</strong>
          Ouvrez un projet depuis le gestionnaire pour activer les exports et l&apos;import.
        </div>
      ) : (
        <>
          <div className="export-cards">
            <div className="export-card panel">
              <div className="export-title">Archive Gerber</div>
              <p className="export-desc">
                Fichiers de fabrication RS-274X (cuivres, masques, serigraphies, contour) + forage Excellon,
                regroupés dans une archive .zip prête pour le fabricant.
              </p>
              <div className="export-actions">
                <Button
                  data-testid="export-gerber-btn"
                  onClick={() => void runExport("gerber")}
                  disabled={busyExport !== null}
                >
                  {busyExport === "gerber" ? "Préparation…" : "Télécharger"}
                </Button>
              </div>
              <span className="export-note">application/zip — Gerber RS-274X + Excellon</span>
            </div>

            <div className="export-card panel">
              <div className="export-title">BOM CSV</div>
              <p className="export-desc">
                Liste des matériaux au format CSV (Références ; Quantité ; Valeur ; Empreinte), utilisable
                dans un tableur ou pour la commande de composants.
              </p>
              <div className="export-actions">
                <Button
                  data-testid="export-bom-btn"
                  onClick={() => void runExport("bom")}
                  disabled={busyExport !== null}
                >
                  {busyExport === "bom" ? "Préparation…" : "Télécharger"}
                </Button>
              </div>
              <span className="export-note">text/csv — en-têtes : Ref;Qty;Value;Footprint</span>
            </div>

            <div className="export-card panel">
              <div className="export-title">Modèle 3D STEP</div>
              <p className="export-desc">
                Carte et composants au format STEP AP214 (boîtes BREP), importable dans un logiciel de
                CAO mécanique pour le contrôle d&apos;encombrement.
              </p>
              <div className="export-actions">
                <Button
                  data-testid="export-step-btn"
                  onClick={() => void runExport("step")}
                  disabled={busyExport !== null}
                >
                  {busyExport === "step" ? "Préparation…" : "Télécharger"}
                </Button>
              </div>
              <span className="export-note">application/step — AP214</span>
            </div>
          </div>

          <div className="import-zone panel">
            <h3 className="panel-title">Importer un fichier</h3>
            <p className="muted" style={{ fontSize: 13 }}>
              Formats acceptés : KiCad PCB (.kicad_pcb), netlist KiCad (.net), Eagle (.brd, .sch),
              interchange KidCAD (.kidcad.json). Le contenu remplace le schéma et le layout du projet courant.
            </p>
            <div className="import-controls">
              <input
                type="file"
                data-testid="import-input"
                accept={ACCEPTED_IMPORT}
                disabled={importing}
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) void handleImport(file);
                  e.target.value = "";
                }}
              />
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void handleRefresh()}
                disabled={importing}
                data-testid="import-refresh-btn"
              >
                Actualiser
              </Button>
            </div>

            {importing ? (
              <div className="page-loading" style={{ minHeight: 80 }}>
                <div className="spinner" aria-hidden="true" />
                <p>Import en cours…</p>
              </div>
            ) : null}

            {importResult ? (
              <div className="import-result" data-testid="import-result">
                <div className="import-stats">
                  <span className="badge badge-info">Format : {importResult.format}</span>
                  <span className="badge badge-muted">Composants : {importResult.components}</span>
                  <span className="badge badge-muted">Nets : {importResult.nets}</span>
                  <span className="badge badge-muted">Pistes : {importResult.tracks}</span>
                  <span className="badge badge-muted">Vias : {importResult.vias}</span>
                </div>
                {importResult.warnings.length > 0 ? (
                  <ul className="warn-list">
                    {importResult.warnings.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                ) : (
                  <span className="muted" style={{ fontSize: 12.5 }}>
                    Aucun avertissement.
                  </span>
                )}
              </div>
            ) : null}
          </div>

          <p className="muted" style={{ marginTop: 14, fontSize: 12.5 }}>
            Une fois l&apos;import terminé, ouvrez{" "}
            <a href={`/pages/pcb-layout${query}`}>l&apos;éditeur PCB</a> pour vérifier le placement, puis
            lancez le placement et le routage IA.
          </p>
        </>
      )}
    </div>
  );
}
