"use client";

/**
 * CollabBar — barre d'état de l'édition collaborative CRDT. Affiche le
 * statut de synchronisation (connecté / synchronisation… / hors ligne avec
 * N opérations en attente), le flux d'activité des opérations distantes et
 * permet de couper/rétablir la collaboration. Les curseurs des pairs sont
 * gérés par PresenceLayer ; ici on montre la SYNCHRONISATION du document.
 */
import { localUserName } from "@/lib/api/ws-client";
import type { useCrdt } from "@/lib/collab/use-crdt";

type Crdt = ReturnType<typeof useCrdt>;

const STATUS_LABEL: Record<string, string> = {
  offline: "Collaboration désactivée",
  connecting: "Connexion…",
  synced: "Synchronisé",
  error: "Hors ligne",
};

const STATUS_CLASS: Record<string, string> = {
  offline: "off",
  connecting: "wait",
  synced: "on",
  error: "err",
};

export function CollabBar({ crdt }: { crdt: Crdt }) {
  const me = typeof window !== "undefined" ? localUserName() : "";

  // Inactif hors projet ou en mode démo : rien à afficher.
  if (!crdt.active && !crdt.enabled) return null;

  return (
    <div className="collab-bar" data-testid="collab-bar">
      <span className={`collab-dot ${STATUS_CLASS[crdt.status] ?? "off"}`} aria-hidden />
      <span className="collab-status">
        {STATUS_LABEL[crdt.status] ?? crdt.status}
        {crdt.pending > 0 ? ` · ${crdt.pending} op(s) en attente` : ""}
      </span>

      <span className="collab-me" title="Votre identité de collaborateur">
        👤 {me}
      </span>

      {crdt.activity.length > 0 ? (
        <ul className="collab-feed" aria-live="polite">
          {crdt.activity.slice(0, 3).map((a) => (
            <li key={a.id} title={`${a.actor} — ${new Date(a.at).toLocaleTimeString()}`}>
              <strong>{a.actor}</strong> {a.label}
            </li>
          ))}
        </ul>
      ) : (
        <span className="collab-hint">Éditez un composant : chaque modification est partagée en temps réel.</span>
      )}

      <button
        type="button"
        className="collab-toggle"
        onClick={() => crdt.setEnabled(!crdt.enabled)}
        title={crdt.enabled ? "Désactiver l'édition collaborative" : "Activer l'édition collaborative"}
      >
        {crdt.enabled ? "Désactiver" : "Activer"}
      </button>
    </div>
  );
}
