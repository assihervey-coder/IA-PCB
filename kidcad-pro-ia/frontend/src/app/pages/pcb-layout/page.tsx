"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { api, isBackendDown } from "@/lib/api/rest-client";
import type { AutoFixResult, DRCResult, DRCViolation, LayoutComponent, LayoutData } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { Button } from "@/app/components/buttons/Button";
import { Modal } from "@/app/components/modals/Modal";
import { DesignDoctor } from "@/app/components/wow/DesignDoctor";
import { DFMOracle } from "@/app/components/wow/DFMOracle";
import { TimeMachine } from "@/app/components/wow/TimeMachine";
import { StatsPanel } from "@/app/components/wow/StatsPanel";
import { PresenceLayer } from "@/app/components/presence/PresenceLayer";
import {
  ComponentPropertiesForm,
  type ComponentPropertiesPatch,
} from "@/app/components/forms/ComponentPropertiesForm";
import {
  estimateFootprintSize,
  footprintPadOffsets,
  screenToWorld,
  type PanZoom,
} from "@/lib/utils/geometry-utils";
import { colorForNet, formatMm, percentFormat } from "@/lib/utils/converters";

const MIN_SCALE = 1;
const MAX_SCALE = 80;
const FIT_MARGIN = 34;

// Règles locales utilisées quand le backend est injoignable (mode démo).
const RULE_MIN_TRACK_WIDTH = 0.2;
const RULE_MIN_VIA_DIAMETER = 0.5;
const RULE_MIN_DRILL = 0.25;
const RULE_EDGE_CLEARANCE = 0.5;

function computeLocalDRC(layout: LayoutData): DRCResult {
  const violations: DRCViolation[] = [];
  const { width_mm: w, height_mm: h } = layout.board;
  for (const track of layout.tracks) {
    if (track.width < RULE_MIN_TRACK_WIDTH) {
      const p = track.points[0] ?? { x: 0, y: 0 };
      violations.push({
        code: "DRC_TRACK_WIDTH",
        severity: "error",
        message: `Largeur de piste ${track.width.toFixed(2)} mm inférieure au minimum (${RULE_MIN_TRACK_WIDTH.toFixed(2)} mm).`,
        x: p.x,
        y: p.y,
        layer: track.layer,
        net: track.net,
      });
    }
    for (const p of track.points) {
      if (
        p.x < RULE_EDGE_CLEARANCE ||
        p.y < RULE_EDGE_CLEARANCE ||
        p.x > w - RULE_EDGE_CLEARANCE ||
        p.y > h - RULE_EDGE_CLEARANCE
      ) {
        violations.push({
          code: "DRC_EDGE_CLEARANCE",
          severity: "error",
          message: "Piste trop proche du bord de la carte.",
          x: p.x,
          y: p.y,
          layer: track.layer,
          net: track.net,
        });
        break;
      }
    }
  }
  for (const via of layout.vias) {
    if (via.diameter < RULE_MIN_VIA_DIAMETER) {
      violations.push({
        code: "DRC_VIA_DIAMETER",
        severity: "error",
        message: `Diamètre de via ${via.diameter.toFixed(2)} mm inférieur au minimum.`,
        x: via.x,
        y: via.y,
        layer: via.from_layer,
        net: via.net,
      });
    }
    if (via.drill < RULE_MIN_DRILL) {
      violations.push({
        code: "DRC_DRILL",
        severity: "error",
        message: `Forage de via ${via.drill.toFixed(2)} mm inférieur au minimum.`,
        x: via.x,
        y: via.y,
        layer: via.from_layer,
        net: via.net,
      });
    }
  }
  return { passed: violations.length === 0, checked_rules: 4, violations, duration_ms: 8 };
}

/** Placement local déterministe (mode démo) : grille ordonnée, composants fixes conservés. */
function computeLocalPlacement(layout: LayoutData): LayoutData {
  const margin = 6;
  const movable = layout.components.filter((c) => !c.fixed);
  const cols = Math.max(2, Math.ceil(Math.sqrt(movable.length || 1)));
  const rows = Math.max(1, Math.ceil(movable.length / cols));
  const cellW = (layout.board.width_mm - margin * 2) / cols;
  const cellH = (layout.board.height_mm - margin * 2) / rows;
  const placed = new Map<string, { x: number; y: number }>();
  movable.forEach((c, i) => {
    placed.set(c.ref, {
      x: margin + cellW * ((i % cols) + 0.5),
      y: margin + cellH * (Math.floor(i / cols) + 0.5),
    });
  });
  return {
    ...layout,
    components: layout.components.map((c) => {
      const pos = placed.get(c.ref);
      return pos ? { ...c, x: pos.x, y: pos.y } : c;
    }),
  };
}

function roundedRectPath(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
  const radius = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.lineTo(x + w - radius, y);
  ctx.arcTo(x + w, y, x + w, y + radius, radius);
  ctx.lineTo(x + w, y + h - radius);
  ctx.arcTo(x + w, y + h, x + w - radius, y + h, radius);
  ctx.lineTo(x + radius, y + h);
  ctx.arcTo(x, y + h, x, y + h - radius, radius);
  ctx.lineTo(x, y + radius);
  ctx.arcTo(x, y, x + radius, y, radius);
  ctx.closePath();
}

export default function PcbLayoutPage() {
  const layout = useProjectStore((s) => s.layout);
  const currentProject = useProjectStore((s) => s.currentProject);
  const demoMode = useProjectStore((s) => s.demoMode);
  const drc = useProjectStore((s) => s.drc);
  const setDRC = useProjectStore((s) => s.setDRC);
  const refreshLayout = useProjectStore((s) => s.refreshLayout);
  const loadDemoData = useProjectStore((s) => s.loadDemoData);
  const pushHistory = useProjectStore((s) => s.pushHistory);
  const undo = useProjectStore((s) => s.undo);
  const redo = useProjectStore((s) => s.redo);
  const historyPast = useProjectStore((s) => s.historyPast);
  const historyFuture = useProjectStore((s) => s.historyFuture);
  const pushToast = useUIStore((s) => s.pushToast);

  const [projectId, setProjectId] = useState<string | null>(null);
  const [hiddenLayers, setHiddenLayers] = useState<number[]>([]);
  const [highlightNet, setHighlightNet] = useState<string | null>(null);
  const [selected, setSelected] = useState<LayoutComponent | null>(null);
  const [zoomLabel, setZoomLabel] = useState(100);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [canvasSize, setCanvasSize] = useState({ w: 0, h: 0 });
  const [showDoctor, setShowDoctor] = useState(false);
  const [showDFM, setShowDFM] = useState(false);
  const [showTimeMachine, setShowTimeMachine] = useState(false);
  const [showStats, setShowStats] = useState(false);
  const [autoFix, setAutoFix] = useState<AutoFixResult | null>(null);

  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewRef = useRef<PanZoom>({ offsetX: 0, offsetY: 0, scale: 10 });
  const fitScaleRef = useRef(10);
  const dragRef = useRef<{ mx: number; my: number; ox: number; oy: number; moved: boolean } | null>(null);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("project");
    setProjectId(id);
    if (id) {
      void refreshLayout(id);
    } else {
      void loadDemoData();
    }
  }, [refreshLayout, loadDemoData]);

  // ------- Rendu canvas -------
  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const data = useProjectStore.getState().layout;
    const dpr = window.devicePixelRatio || 1;
    const cssW = canvas.clientWidth || canvasSize.w || 800;
    const cssH = canvas.clientHeight || canvasSize.h || 520;
    if (canvas.width !== Math.round(cssW * dpr) || canvas.height !== Math.round(cssH * dpr)) {
      canvas.width = Math.round(cssW * dpr);
      canvas.height = Math.round(cssH * dpr);
    }

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = "#0B1220";
    ctx.fillRect(0, 0, cssW, cssH);

    const view = viewRef.current;

    if (!data) return;
    const W = data.board.width_mm;
    const H = data.board.height_mm;

    // Passer en coordonnées monde (mm).
    ctx.setTransform(dpr * view.scale, 0, 0, dpr * view.scale, dpr * view.offsetX, dpr * view.offsetY);

    // Plaque.
    ctx.fillStyle = "#0F1B33";
    ctx.fillRect(0, 0, W, H);

    // Grille 1 mm (+ repères 10 mm) si la densité le permet.
    if (view.scale >= 4.5) {
      ctx.lineWidth = 1 / view.scale;
      ctx.strokeStyle = "rgba(140, 160, 192, 0.10)";
      ctx.beginPath();
      for (let x = 0; x <= W; x += 1) {
        ctx.moveTo(x, 0);
        ctx.lineTo(x, H);
      }
      for (let y = 0; y <= H; y += 1) {
        ctx.moveTo(0, y);
        ctx.lineTo(W, y);
      }
      ctx.stroke();
      ctx.strokeStyle = "rgba(140, 160, 192, 0.20)";
      ctx.beginPath();
      for (let x = 0; x <= W; x += 10) {
        ctx.moveTo(x, 0);
        ctx.lineTo(x, H);
      }
      for (let y = 0; y <= H; y += 10) {
        ctx.moveTo(0, y);
        ctx.lineTo(W, y);
      }
      ctx.stroke();
    }

    // Contour de la carte.
    ctx.strokeStyle = "#3D5A96";
    ctx.lineWidth = 2 / view.scale;
    ctx.strokeRect(0, 0, W, H);

    // Pistes.
    ctx.lineJoin = "round";
    ctx.lineCap = "round";
    for (const track of data.tracks) {
      const visible = !hiddenLayers.includes(track.layer);
      let alpha = visible ? 1 : 0.08;
      if (highlightNet && track.net !== highlightNet) alpha = visible ? 0.18 : 0.04;
      ctx.globalAlpha = alpha;
      ctx.strokeStyle = colorForNet(track.net);
      ctx.lineWidth = Math.max(track.width, 1.2 / view.scale);
      ctx.beginPath();
      track.points.forEach((p, i) => (i === 0 ? ctx.moveTo(p.x, p.y) : ctx.lineTo(p.x, p.y)));
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    // Vias.
    for (const via of data.vias) {
      const visible = !hiddenLayers.includes(via.from_layer) || !hiddenLayers.includes(via.to_layer);
      ctx.globalAlpha = visible ? 1 : 0.1;
      if (highlightNet && via.net !== highlightNet) ctx.globalAlpha = visible ? 0.2 : 0.06;
      ctx.beginPath();
      ctx.arc(via.x, via.y, via.diameter / 2, 0, Math.PI * 2);
      ctx.fillStyle = "#FFD166";
      ctx.fill();
      ctx.beginPath();
      ctx.arc(via.x, via.y, via.drill / 2, 0, Math.PI * 2);
      ctx.fillStyle = "#0B1220";
      ctx.fill();
    }
    ctx.globalAlpha = 1;

    // Composants (corps + pastilles).
    for (const comp of data.components) {
      const size = estimateFootprintSize(comp.footprint);
      const isSel = selected?.ref === comp.ref;
      ctx.save();
      ctx.translate(comp.x, comp.y);
      ctx.rotate((comp.rotation * Math.PI) / 180);
      ctx.globalAlpha = highlightNet ? 0.85 : 1;
      roundedRectPath(ctx, -size.w / 2, -size.h / 2, size.w, size.h, 0.5);
      ctx.fillStyle = "#1B2740";
      ctx.fill();
      ctx.strokeStyle = isSel ? "#22D3EE" : "#43598A";
      ctx.lineWidth = (isSel ? 2.2 : 1.2) / view.scale;
      if (isSel) ctx.setLineDash([1.2, 0.8]);
      ctx.stroke();
      ctx.setLineDash([]);
      for (const pad of footprintPadOffsets(size)) {
        ctx.fillStyle = "#F59E0B";
        ctx.fillRect(pad.x - 0.35, pad.y - 0.4, 0.7, 0.8);
      }
      ctx.restore();
      ctx.globalAlpha = 1;
    }

    // Marqueurs DRC.
    if (drc && !drc.passed) {
      for (const v of drc.violations) {
        ctx.strokeStyle = "#F87171";
        ctx.lineWidth = 0.35;
        ctx.beginPath();
        ctx.arc(v.x, v.y, 1.6, 0, Math.PI * 2);
        ctx.stroke();
        ctx.beginPath();
        ctx.moveTo(v.x - 0.7, v.y - 0.7);
        ctx.lineTo(v.x + 0.7, v.y + 0.7);
        ctx.moveTo(v.x + 0.7, v.y - 0.7);
        ctx.lineTo(v.x - 0.7, v.y + 0.7);
        ctx.stroke();
      }
    }

    // Repères de référence (espace écran, texte net).
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.font = "600 11px system-ui, sans-serif";
    ctx.textAlign = "center";
    for (const comp of data.components) {
      const sx = comp.x * view.scale + view.offsetX;
      const sy = comp.y * view.scale + view.offsetY - (estimateFootprintSize(comp.footprint).h / 2) * view.scale - 6;
      ctx.fillStyle = selected?.ref === comp.ref ? "#22D3EE" : "#8CA0C0";
      ctx.fillText(comp.ref, sx, sy);
    }
  }, [hiddenLayers, highlightNet, selected, drc, canvasSize.w, canvasSize.h]);

  const drawRef = useRef<() => void>(() => {});
  drawRef.current = draw;

  useEffect(() => {
    draw();
  }, [draw, layout]);

  // Redimensionnement du canvas.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const observer = new ResizeObserver(() => {
      setCanvasSize({ w: canvas.clientWidth, h: canvas.clientHeight });
      drawRef.current();
    });
    observer.observe(canvas);
    return () => observer.disconnect();
  }, []);

  const fitView = useCallback(() => {
    const data = useProjectStore.getState().layout;
    const canvas = canvasRef.current;
    if (!data || !canvas) return;
    const cssW = canvas.clientWidth || 800;
    const cssH = canvas.clientHeight || 520;
    const scale = Math.min(
      (cssW - FIT_MARGIN * 2) / data.board.width_mm,
      (cssH - FIT_MARGIN * 2) / data.board.height_mm,
    );
    viewRef.current = {
      scale,
      offsetX: (cssW - data.board.width_mm * scale) / 2,
      offsetY: (cssH - data.board.height_mm * scale) / 2,
    };
    fitScaleRef.current = scale;
    setZoomLabel(100);
    drawRef.current();
  }, []);

  // Ajustement initial dès que le layout est disponible.
  const fittedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!layout || !canvasRef.current) return;
    const key = `${currentProject?.id ?? "demo"}-${layout.board.width_mm}x${layout.board.height_mm}`;
    if (fittedFor.current !== key) {
      fittedFor.current = key;
      fitView();
    }
  }, [layout, currentProject, fitView]);

  // Zoom molette centré sur le curseur.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = canvas.getBoundingClientRect();
      const mx = e.clientX - rect.left;
      const my = e.clientY - rect.top;
      const view = viewRef.current;
      const before = screenToWorld(mx, my, view);
      const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
      view.scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, view.scale * factor));
      view.offsetX = mx - before.x * view.scale;
      view.offsetY = my - before.y * view.scale;
      if (fitScaleRef.current > 0) {
        setZoomLabel(Math.round((view.scale / fitScaleRef.current) * 100));
      }
      drawRef.current();
    };
    canvas.addEventListener("wheel", onWheel, { passive: false });
    return () => canvas.removeEventListener("wheel", onWheel);
  }, []);

  const zoomBy = (factor: number) => {
    const view = viewRef.current;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const cx = canvas.clientWidth / 2;
    const cy = canvas.clientHeight / 2;
    const before = screenToWorld(cx, cy, view);
    view.scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, view.scale * factor));
    view.offsetX = cx - before.x * view.scale;
    view.offsetY = cy - before.y * view.scale;
    setZoomLabel(Math.round((view.scale / fitScaleRef.current) * 100));
    drawRef.current();
  };

  // ------- Interactions souris -------
  const handlePointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (e.button !== 0) return;
    canvasRef.current?.setPointerCapture(e.pointerId);
    dragRef.current = { mx: e.clientX, my: e.clientY, ox: viewRef.current.offsetX, oy: viewRef.current.offsetY, moved: false };
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const drag = dragRef.current;
    if (!drag) return;
    const dx = e.clientX - drag.mx;
    const dy = e.clientY - drag.my;
    if (Math.abs(dx) + Math.abs(dy) > 4) drag.moved = true;
    if (drag.moved) {
      viewRef.current.offsetX = drag.ox + dx;
      viewRef.current.offsetY = drag.oy + dy;
      drawRef.current();
    }
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const drag = dragRef.current;
    dragRef.current = null;
    if (!drag || drag.moved) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const world = screenToWorld(e.clientX - rect.left, e.clientY - rect.top, viewRef.current);
    const data = useProjectStore.getState().layout;
    if (!data) return;
    let hit: LayoutComponent | null = null;
    for (let i = data.components.length - 1; i >= 0; i--) {
      const c = data.components[i];
      const size = estimateFootprintSize(c.footprint);
      const swap = Math.abs(c.rotation % 180) === 90;
      const hw = (swap ? size.h : size.w) / 2 + 0.4;
      const hh = (swap ? size.w : size.h) / 2 + 0.4;
      if (Math.abs(world.x - c.x) <= hw && Math.abs(world.y - c.y) <= hh) {
        hit = c;
        break;
      }
    }
    setSelected(hit);
  };

  // ------- Actions -------
  const handlePlace = async () => {
    if (busyAction) return;
    if (!projectId) {
      const data = useProjectStore.getState().layout;
      if (data) {
        setBusyAction("place");
        useProjectStore.setState({ layout: computeLocalPlacement(data) });
        setBusyAction(null);
        pushToast("Mode démo : placement local simulé (grille ordonnée).", "info");
      }
      return;
    }
    setBusyAction("place");
    try {
      pushHistory();
      await api.startPlace(projectId, "heuristic");
      await refreshLayout(projectId);
      pushToast("Placement IA terminé, layout mis à jour.", "success");
    } catch (err) {
      if (isBackendDown(err)) {
        const data = useProjectStore.getState().layout;
        if (data) {
          useProjectStore.setState({ layout: computeLocalPlacement(data) });
          pushToast("Mode démo : placement local simulé (grille ordonnée).", "info");
        }
      } else {
        pushToast("Échec du placement IA.", "error");
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleSave = async () => {
    const data = useProjectStore.getState().layout;
    if (!projectId || !data || busyAction) return;
    setBusyAction("save");
    try {
      await api.putLayout(projectId, data);
      pushToast("Layout enregistré sur le backend.", "success");
    } catch (err) {
      if (isBackendDown(err)) {
        pushToast("Mode démo : enregistrement local uniquement.", "info");
      } else {
        pushToast("Échec de l'enregistrement du layout.", "error");
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleDrc = async () => {
    if (busyAction) return;
    const data = useProjectStore.getState().layout;
    if (!data) return;
    if (!projectId) {
      setDRC(computeLocalDRC(data));
      return;
    }
    setBusyAction("drc");
    try {
      const result = await api.runDRC(projectId);
      setDRC(result);
      pushToast(
        result.passed
          ? `DRC conforme (${result.checked_rules} règles vérifiées).`
          : `DRC : ${result.violations.length} violation(s).`,
        result.passed ? "success" : "error",
      );
    } catch (err) {
      if (isBackendDown(err)) {
        const local = computeLocalDRC(data);
        setDRC(local);
        pushToast(
          local.passed
            ? "Mode démo : DRC local conforme."
            : `Mode démo : DRC local — ${local.violations.length} violation(s).`,
          local.passed ? "success" : "error",
        );
      } else {
        pushToast("Échec de l'exécution du DRC.", "error");
      }
    } finally {
      setBusyAction(null);
    }
  };

  /** Auto-Healer DRC : répare largeurs, vias et marges en un clic (wow). */
  const handleAutoFix = async () => {
    if (busyAction) return;
    const data = useProjectStore.getState().layout;
    if (!data) return;
    if (!projectId || demoMode) {
      pushToast("Auto-Healer disponible en mode connecté (backend requis).", "info");
      return;
    }
    setBusyAction("autofix");
    try {
      const res = await api.drcAutoFix(projectId, false);
      setAutoFix(res);
      if (res.board_changed) {
        pushHistory();
        await refreshLayout(projectId);
      }
      // Les marqueurs DRC sont recalculés pour refléter les réparations.
      try {
        setDRC(await api.runDRC(projectId));
      } catch {
        /* marqueurs optionnels */
      }
      pushToast(
        res.violations_after === 0 && res.violations_before > 0
          ? `🩹 ${res.violations_before} violation(s) réparée(s) — DRC conforme !`
          : `🩹 Auto-fix : ${res.fixed} réparé(s), ${res.violations_after} restant(s).`,
        res.violations_after === 0 ? "success" : "info",
      );
    } catch (err) {
      if (isBackendDown(err)) {
        pushToast("Auto-Healer : backend injoignable.", "error");
      } else {
        pushToast("Auto-Healer : réparation impossible (carte absente ?).", "error");
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleUndo = () => {
    if (historyPast.length === 0) {
      pushToast("Rien à annuler.", "info");
      return;
    }
    void undo();
  };

  const handleRedo = () => {
    if (historyFuture.length === 0) {
      pushToast("Rien à rétablir.", "info");
      return;
    }
    void redo();
  };

  // Raccourcis clavier : Ctrl+Z / Ctrl+Shift+Z (ou Ctrl+Y).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      const key = e.key.toLowerCase();
      if (key === "z" && !e.shiftKey) {
        e.preventDefault();
        handleUndo();
      } else if ((key === "z" && e.shiftKey) || key === "y") {
        e.preventDefault();
        handleRedo();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const applyProperties = async (patch: ComponentPropertiesPatch) => {
    const data = useProjectStore.getState().layout;
    if (!data || !selected) return;
    pushHistory();
    const next: LayoutData = {
      ...data,
      components: data.components.map((c) =>
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
    setSelected(null);
    if (projectId && !demoMode) {
      try {
        await api.putLayout(projectId, next);
        pushToast("Composant mis à jour et enregistré.", "success");
      } catch {
        pushToast("Composant mis à jour localement (backend injoignable).", "info");
      }
    }
  };

  const toggleLayer = (index: number) => {
    setHiddenLayers((prev) => (prev.includes(index) ? prev.filter((l) => l !== index) : [...prev, index]));
  };

  const query = projectId ? `?project=${encodeURIComponent(projectId)}` : "";
  const layerNames = layout?.board.layer_names ?? [];
  const gridCols = showStats ? "270px 1fr 300px" : "270px 1fr";

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Éditeur PCB</h1>
          <p className="page-subtitle">
            {currentProject?.name ?? "Projet sans nom"}
            {projectId ? ` — projet ${projectId}` : " — données de démonstration"}
          </p>
        </div>
      </div>

      {demoMode ? (
        <div className="demo-banner" data-testid="demo-banner" role="status">
          Mode démo : backend injoignable, carte d&apos;exemple affichée.
        </div>
      ) : null}

      <div className="editor-layout" style={{ gridTemplateColumns: gridCols }}>
        <aside className="side-panel">
          <div className="side-box panel">
            <h3 className="panel-title">Couches</h3>
            <div className="layer-list">
              {layerNames.map((name, index) => (
                <label key={name} className="layer-item">
                  <input
                    type="checkbox"
                    checked={!hiddenLayers.includes(index)}
                    onChange={() => toggleLayer(index)}
                  />
                  <span>{name}</span>
                </label>
              ))}
            </div>
          </div>

          <div className="side-box panel">
            <h3 className="panel-title">Nets ({layout?.nets.length ?? 0})</h3>
            <ul className="net-list">
              {(layout?.nets ?? []).map((net) => (
                <li
                  key={net.name}
                  className={`net-item${highlightNet === net.name ? " active" : ""}`}
                  onClick={() => setHighlightNet((prev) => (prev === net.name ? null : net.name))}
                  title="Cliquer pour isoler ce net"
                >
                  <span className="net-dot" style={{ background: colorForNet(net.name) }} />
                  <span className="net-name">{net.name}</span>
                  <span className="net-count">{net.pad_count} pads</span>
                </li>
              ))}
            </ul>
          </div>

          <div className="side-box panel">
            <h3 className="panel-title">Légende</h3>
            <div className="legend">
              <div className="legend-item">
                <span className="legend-swatch" style={{ background: "#F59E0B" }} />
                Pastilles / cuivre F.Cu
              </div>
              <div className="legend-item">
                <span className="legend-swatch" style={{ background: "#FFD166" }} />
                Vias (couche B en or)
              </div>
              <div className="legend-item">
                <span className="legend-swatch" style={{ background: "#22D3EE" }} />
                Composant sélectionné
              </div>
              <div className="legend-item">
                <span className="legend-swatch" style={{ background: "#F87171" }} />
                Violation DRC
              </div>
            </div>
          </div>
        </aside>

        <section className="canvas-wrap panel">
          <div className="toolbar">
            <Button size="sm" data-testid="ai-place-btn" onClick={() => void handlePlace()} disabled={busyAction !== null}>
              {busyAction === "place" ? "Placement…" : "Placement IA"}
            </Button>
            <Link className="btn btn-secondary btn-sm" href={`/pages/pcb-layout/router${query}`}>
              Routage IA
            </Link>
            <Button size="sm" data-testid="drc-run-btn" onClick={() => void handleDrc()} disabled={busyAction !== null}>
              {busyAction === "drc" ? "Analyse…" : "DRC"}
            </Button>
            <Button
              size="sm"
              data-testid="autofix-btn"
              onClick={() => void handleAutoFix()}
              disabled={busyAction !== null}
              title="Répare automatiquement largeurs, vias et marges de bord"
            >
              {busyAction === "autofix" ? "Réparation…" : "🩹 Auto-fix"}
            </Button>
            <Link className="btn btn-secondary btn-sm" href={`/pages/schematic-editor${query}`}>
              ERC
            </Link>
            <span className="toolbar-sep" />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setShowDoctor(true)}
              title="Audit global noté du design (DRC, ERC, SI, thermique, DFM)"
            >
              🩺 Diagnostic
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setShowDFM(true)}
              title="Coût de fabrication et rendement premier passage"
            >
              💰 DFM
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setShowTimeMachine(true)}
              title="Instantanés, diff et restauration"
            >
              🕰 Historique
            </Button>
            <Button
              size="sm"
              variant={showStats ? "secondary" : "ghost"}
              onClick={() => setShowStats((v) => !v)}
              title="Tableau de bord statistique"
            >
              📊 Stats
            </Button>
            <span className="toolbar-sep" />
            <Button size="sm" variant="ghost" onClick={handleUndo} disabled={historyPast.length === 0} title="Annuler (Ctrl+Z)">
              ↶ Annuler
            </Button>
            <Button size="sm" variant="ghost" onClick={handleRedo} disabled={historyFuture.length === 0} title="Rétablir (Ctrl+Maj+Z)">
              ↷ Rétablir
            </Button>
            <span className="toolbar-sep" />
            <Link className="btn btn-secondary btn-sm" href={`/pages/export${query}`}>
              Exporter
            </Link>
            <Link className="btn btn-secondary btn-sm" href={`/pages/pcb-layout/viewer${query}`}>
              Vue 3D
            </Link>
            <span className="toolbar-sep" />
            <Button size="sm" variant="ghost" onClick={() => void handleSave()} disabled={busyAction !== null}>
              {busyAction === "save" ? "Enregistrement…" : "Enregistrer"}
            </Button>
            <span className="toolbar-sep" />
            <Button size="sm" variant="secondary" onClick={() => zoomBy(1.2)} aria-label="Zoom avant">
              Zoom +
            </Button>
            <Button size="sm" variant="secondary" onClick={() => zoomBy(1 / 1.2)} aria-label="Zoom arrière">
              Zoom −
            </Button>
            <Button size="sm" variant="ghost" onClick={fitView}>
              Ajuster
            </Button>
            <span className="zoom-label">{percentFormat(zoomLabel)}</span>
          </div>

          <canvas
            ref={canvasRef}
            data-testid="pcb-canvas"
            className="pcb-canvas"
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerLeave={handlePointerUp}
          />

          {/* Présence collaborative temps réel (curseurs distants). */}
          <PresenceLayer />

          {!layout ? (
            <div className="hint-empty">
              Aucune donnée de layout. Créez un projet, importez un fichier ou lancez le placement IA.
            </div>
          ) : null}

          <div className="canvas-status">
            <span>
              Carte : {layout ? `${formatMm(layout.board.width_mm, 0)} × ${formatMm(layout.board.height_mm, 0)}` : "—"}
            </span>
            <span>{layout?.tracks.length ?? 0} pistes — {layout?.vias.length ?? 0} vias</span>
            {highlightNet ? <span>Net isolé : {highlightNet}</span> : null}
            {selected ? <span>Sélection : {selected.ref}</span> : <span className="muted">Cliquez sur un composant</span>}
          </div>
        </section>

        {showStats && <StatsPanel onClose={() => setShowStats(false)} />}
      </div>

      {autoFix ? (
        <section className="erc-panel panel autofix-panel">
          <div className="erc-header">
            <h3>🩹 Auto-Healer DRC {autoFix.dry_run ? "(simulation)" : ""}</h3>
            <div className="page-actions">
              <span className="erc-summary">
                {autoFix.violations_before} → {autoFix.violations_after} violation(s) en{" "}
                {autoFix.duration_ms} ms
              </span>
              <Button size="sm" variant="ghost" onClick={() => setAutoFix(null)}>
                Masquer
              </Button>
            </div>
          </div>
          <div className={`pass-banner ${autoFix.violations_after === 0 ? "ok" : autoFix.fixed > 0 ? "ok" : "fail"}`}>
            {autoFix.violations_before === 0
              ? "Aucune violation à réparer : la carte était déjà conforme."
              : autoFix.violations_after === 0
                ? `Réparation complète : ${autoFix.fixed} violation(s) corrigée(s).`
                : `${autoFix.fixed} violation(s) réparée(s), ${autoFix.violations_after} restant(s) (hors réparabilité automatique).`}
          </div>
          {autoFix.fixes.length > 0 && (
            <ul className="violation-list" style={{ marginTop: 10 }}>
              {autoFix.fixes.map((f, i) => (
                <li key={i} className="violation-item">
                  <span className={`sev ${f.applied ? "sev-ok" : "sev-warning"}`}>
                    {Math.round(f.confidence * 100)} %
                  </span>
                  <div>
                    <strong>{f.action}</strong>
                    <span> — {f.message}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}

      {drc ? (
        <section className="erc-panel panel">
          <div className="erc-header">
            <h3>Résultats DRC</h3>
            <div className="page-actions">
              <span className="erc-summary">
                {drc.checked_rules} règles vérifiées en {drc.duration_ms} ms
              </span>
              <Button size="sm" variant="ghost" onClick={() => setDRC(null)}>
                Masquer
              </Button>
            </div>
          </div>
          <div className={`pass-banner ${drc.passed ? "ok" : "fail"}`}>
            {drc.passed
              ? "DRC conforme : aucune violation détectée."
              : `DRC : ${drc.violations.length} violation(s) — marquées en rouge sur la carte.`}
          </div>
          {drc.violations.length > 0 ? (
            <ul className="violation-list" style={{ marginTop: 10 }}>
              {drc.violations.map((v, i) => (
                <li key={i} className="violation-item">
                  <span className={`sev sev-${v.severity}`}>
                    {v.severity === "error" ? "Erreur" : "Avertissement"}
                  </span>
                  <div>
                    <strong>{v.code}</strong>
                    <span> — {v.message}</span>
                    <div className="violation-loc">
                      Net {v.net || "—"} · Couche {layerNames[v.layer] ?? v.layer} · x {formatMm(v.x)} · y {formatMm(v.y)}
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          ) : null}
        </section>
      ) : null}

      {selected ? (
        <Modal title={`Propriétés — ${selected.ref}`} onClose={() => setSelected(null)}>
          <ComponentPropertiesForm
            component={selected}
            onSubmit={(patch) => void applyProperties(patch)}
            onCancel={() => setSelected(null)}
          />
        </Modal>
      ) : null}

      {showDoctor ? <DesignDoctor onClose={() => setShowDoctor(false)} /> : null}
      {showDFM ? <DFMOracle onClose={() => setShowDFM(false)} /> : null}
      {showTimeMachine ? <TimeMachine onClose={() => setShowTimeMachine(false)} /> : null}
    </div>
  );
}
