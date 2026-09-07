"use client";

/**
 * MagicBar — palette de commandes IA (⌘K / Ctrl+K).
 * Le copilot interprète le langage naturel (FR/EN) via POST /magic,
 * affiche un aperçu des actions, puis les exécute sur validation.
 * Raccourcis secondaires : 🌡️ thermique, 📡 eye oracle, ⚔️ arène.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api/rest-client";
import type { MagicResult } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";

// --- Web Speech API (dictée du copilot, good-to-have) ------------------

type SpeechRecognitionLike = {
  lang: string;
  interimResults: boolean;
  continuous: boolean;
  start(): void;
  stop(): void;
  onresult: ((e: { results: ArrayLike<ArrayLike<{ transcript: string }>> }) => void) | null;
  onerror: (() => void) | null;
  onend: (() => void) | null;
};

function createRecognition(): SpeechRecognitionLike | null {
  if (typeof window === "undefined") return null;
  const w = window as unknown as {
    SpeechRecognition?: new () => SpeechRecognitionLike;
    webkitSpeechRecognition?: new () => SpeechRecognitionLike;
  };
  const Ctor = w.SpeechRecognition ?? w.webkitSpeechRecognition;
  if (!Ctor) return null;
  const rec = new Ctor();
  rec.lang = "fr-FR";
  rec.interimResults = false;
  rec.continuous = false;
  return rec;
}

const KIND_ICONS: Record<string, string> = {
  place_component: "🧩",
  move_component: "✥",
  delete_component: "🗑",
  set_track_width: "〰",
  add_net_class: "🏷",
  route: "🕸",
  optimize: "⚡",
  run_drc: "🛡",
  run_erc: "🔌",
  export: "📦",
};

const SUGGESTIONS = [
  "place R1 près de U3",
  "largeur de piste 0.3 mm",
  "route tout en rapide",
  "deux condensateurs près de U1",
  "drc puis exporte gerber",
];

export function MagicBar() {
  const [open, setOpen] = useState(false);
  const [utterance, setUtterance] = useState("");
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<MagicResult | null>(null);
  const [listening, setListening] = useState(false);
  const [voiceSupported, setVoiceSupported] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const recRef = useRef<SpeechRecognitionLike | null>(null);

  const currentProject = useProjectStore((s) => s.currentProject);
  const refreshLayout = useProjectStore((s) => s.refreshLayout);
  const pushToast = useUIStore((s) => s.pushToast);

  // Disponibilité de la dictée vocale (Chrome/Edge/Safari).
  useEffect(() => {
    setVoiceSupported(createRecognition() !== null);
  }, []);

  // ⌘K / Ctrl+K : ouverture instantanée, Escape : fermeture.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      }
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (open) {
      setPreview(null);
      setTimeout(() => inputRef.current?.focus(), 30);
    }
  }, [open]);

  const requireProject = useCallback((): string | null => {
    const pid = currentProject?.id;
    if (!pid) {
      pushToast("Ouvrez d'abord un projet pour utiliser le copilot", "error");
      return null;
    }
    return pid;
  }, [currentProject, pushToast]);

  const runPreview = useCallback(async () => {
    const pid = requireProject();
    if (!pid || utterance.trim() === "" || busy) return;
    setBusy(true);
    try {
      const res = await api.magic(pid, utterance, false);
      setPreview(res);
    } catch {
      pushToast("Copilot injoignable (backend hors ligne ?)", "error");
    } finally {
      setBusy(false);
    }
  }, [busy, pushToast, requireProject, utterance]);

  const runApply = useCallback(async () => {
    const pid = requireProject();
    if (!pid || utterance.trim() === "" || busy) return;
    setBusy(true);
    try {
      const res = await api.magic(pid, utterance, true);
      setPreview(res);
      if (res.board_changed) await refreshLayout();
      res.applied.forEach((m) => pushToast(m, "success"));
      if (res.skipped.length > 0) pushToast(res.skipped[0], "info");
    } catch {
      pushToast("Le copilot n'a pas pu appliquer la commande", "error");
    } finally {
      setBusy(false);
    }
  }, [busy, pushToast, refreshLayout, requireProject, utterance]);

  const quickThermal = useCallback(async () => {
    const pid = requireProject();
    if (!pid) return;
    setBusy(true);
    try {
      const res = await api.thermal(pid);
      pushToast(
        `🌡️ Point chaud : ${res.max_temp_c.toFixed(0)} °C près de ${res.hotspots[0]?.likely_ref ?? "n/a"}`,
        "info",
      );
    } catch {
      pushToast("Simulation thermique indisponible", "error");
    } finally {
      setBusy(false);
    }
  }, [pushToast, requireProject]);

  const quickSI = useCallback(async () => {
    const pid = requireProject();
    if (!pid) return;
    setBusy(true);
    try {
      const res = await api.signalIntegrity(pid);
      pushToast(`📡 Oracle d'œil : score ${res.si_score_pct}% — ${res.summary}`, "info");
    } catch {
      pushToast("Oracle d'œil indisponible (routez d'abord des pistes)", "error");
    } finally {
      setBusy(false);
    }
  }, [pushToast, requireProject]);

  const quickArena = useCallback(async () => {
    const pid = requireProject();
    if (!pid) return;
    setBusy(true);
    try {
      const res = await api.arenaFight(pid);
      pushToast(
        `⚔️ ${res.winner === "draw" ? "Match nul !" : `${res.winner} gagne`} (ELO ${res.elo[0].rating.toFixed(0)}/${res.elo[1].rating.toFixed(0)})`,
        "success",
      );
    } catch {
      pushToast("Arène indisponible (besoin d'un netlist + placement)", "error");
    } finally {
      setBusy(false);
    }
  }, [pushToast, requireProject]);

  /** Dictée vocale : « place R1 près de U3 » à la voix, puis exécution. */
  const toggleVoice = useCallback(() => {
    if (listening) {
      recRef.current?.stop();
      return;
    }
    const rec = createRecognition();
    if (!rec) {
      pushToast("Dictée vocale non supportée par ce navigateur.", "error");
      return;
    }
    recRef.current = rec;
    rec.onresult = (e) => {
      const transcript = e.results?.[0]?.[0]?.transcript ?? "";
      if (transcript) {
        setUtterance(transcript);
        setPreview(null);
        pushToast(`🎙️ « ${transcript} »`, "info");
      }
    };
    rec.onerror = () => {
      setListening(false);
      pushToast("Dictée vocale interrompue.", "error");
    };
    rec.onend = () => setListening(false);
    setListening(true);
    rec.start();
  }, [listening, pushToast]);

  return (
    <>
      <button
        type="button"
        className="magic-fab"
        onClick={() => setOpen(true)}
        aria-label="Ouvrir le copilot magique"
        title="Copilot magique (⌘K)"
      >
        <span className="magic-fab-icon">✨</span>
      </button>

      {open && (
        <div className="magic-overlay" onClick={() => setOpen(false)} role="dialog" aria-modal>
          <div className="magic-panel" onClick={(e) => e.stopPropagation()}>
            <div className="magic-input-row">
              <span className="magic-spark">✨</span>
              <input
                ref={inputRef}
                className="magic-input"
                placeholder="Dites ce que vous voulez : « place R1 près de U3 »…"
                value={utterance}
                onChange={(e) => {
                  setUtterance(e.target.value);
                  setPreview(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void runApply();
                }}
                disabled={busy}
              />
              {voiceSupported && (
                <button
                  type="button"
                  className={`magic-mic ${listening ? "listening" : ""}`}
                  onClick={toggleVoice}
                  aria-label={listening ? "Arrêter la dictée" : "Dicter une commande"}
                  title={listening ? "Arrêter la dictée" : "Dicter une commande (fr-FR)"}
                >
                  🎙
                </button>
              )}
              <kbd className="magic-kbd">Esc</kbd>
            </div>

            <div className="magic-suggestions">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  className="magic-chip"
                  onClick={() => {
                    setUtterance(s);
                    setPreview(null);
                    inputRef.current?.focus();
                  }}
                >
                  {s}
                </button>
              ))}
            </div>

            <div className="magic-quick">
              <button type="button" onClick={quickThermal} disabled={busy}>
                🌡️ Thermique
              </button>
              <button type="button" onClick={quickSI} disabled={busy}>
                📡 Eye Oracle
              </button>
              <button type="button" onClick={quickArena} disabled={busy}>
                ⚔️ Arène IA
              </button>
            </div>

            {preview && (
              <div className="magic-result">
                <div className="magic-result-head">
                  <span>{preview.interpretation.reply}</span>
                  <span
                    className={`magic-confidence ${
                      preview.interpretation.confidence >= 0.75
                        ? "high"
                        : preview.interpretation.confidence >= 0.4
                          ? "mid"
                          : "low"
                    }`}
                  >
                    {Math.round(preview.interpretation.confidence * 100)}%
                  </span>
                </div>
                {preview.interpretation.actions.map((a, i) => (
                  <div key={i} className={`magic-action ${a.executable ? "ok" : "delegate"}`}>
                    <span className="magic-action-icon">{KIND_ICONS[a.kind] ?? "•"}</span>
                    <span>{a.summary}</span>
                    {!a.executable && <em className="magic-deleg">délégué</em>}
                  </div>
                ))}
                {preview.applied.length > 0 && (
                  <div className="magic-applied">
                    {preview.applied.map((m, i) => (
                      <div key={i} className="magic-applied-line">
                        ✓ {m}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            <div className="magic-footer">
              <button type="button" className="magic-ghost" onClick={() => void runPreview()} disabled={busy}>
                Interpréter
              </button>
              <button type="button" className="magic-cta" onClick={() => void runApply()} disabled={busy}>
                {busy ? "…" : "Exécuter ⏎"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
