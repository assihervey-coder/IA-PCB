"use client";

/**
 * ShortcutsOverlay — aide-mémoire des raccourcis clavier (good-to-have).
 * S'ouvre avec « ? » (Shift+/), se ferme avec Échap ou clic extérieur.
 */
import { useEffect, useState } from "react";

const SHORTCUTS: Array<{ keys: string; label: string }> = [
  { keys: "⌘K / Ctrl+K", label: "Copilot magique (langage naturel)" },
  { keys: "?", label: "Cette aide-mémoire" },
  { keys: "Ctrl+Z", label: "Annuler la dernière modification" },
  { keys: "Ctrl+Maj+Z", label: "Rétablir" },
  { keys: "Échap", label: "Fermer la fenêtre / palette active" },
  { keys: "Molette", label: "Zoomer sur la carte" },
  { keys: "Glisser (fond)", label: "Déplacer la vue" },
  { keys: "Clic composant", label: "Sélectionner et éditer" },
];

export function ShortcutsOverlay() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const typing =
        target != null &&
        (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);
      if (!typing && e.key === "?") {
        e.preventDefault();
        setOpen((v) => !v);
      }
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  if (!open) return null;

  return (
    <div className="shortcuts-overlay" role="dialog" aria-modal aria-label="Raccourcis clavier"
      onClick={(e) => {
        if (e.target === e.currentTarget) setOpen(false);
      }}
    >
      <div className="shortcuts-panel">
        <div className="shortcuts-head">
          <h2>⌨️ Raccourcis clavier</h2>
          <kbd>Esc</kbd>
        </div>
        <ul className="shortcuts-list">
          {SHORTCUTS.map((s) => (
            <li key={s.keys}>
              <kbd>{s.keys}</kbd>
              <span>{s.label}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
