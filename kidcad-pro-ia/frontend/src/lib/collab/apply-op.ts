/**
 * Application pure d'une opération CRDT sur le layout local.
 * Utilisée par le client CRDT pour fusionner les opérations distantes
 * (WebSocket + rattrapage REST) dans le store Zustand. Les fonctions sont
 * pures : elles renvoient un NOUVEL objet LayoutData (immutabilité) ou
 * l'objet d'entrée lorsque l'opération est un no-op local.
 */
import type { CollabOp, LayoutData } from "@/lib/api/types";

/** Tolérance mm pour reconnaître un même via (via.remove). */
const VIA_TOLERANCE_MM = 0.05;

function sameTrackPoints(
  a: Array<{ x: number; y: number }>,
  b: Array<{ x: number; y: number }>,
): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i].x !== b[i].x || a[i].y !== b[i].y) return false;
  }
  return true;
}

export function applyCollabOp(layout: LayoutData | null, op: CollabOp): LayoutData | null {
  if (!layout) return layout;
  const p = op.payload ?? {};

  switch (op.kind) {
    case "component.move": {
      const ref = op.target ?? "";
      if (!ref) return layout;
      return {
        ...layout,
        components: layout.components.map((c) =>
          c.ref === ref ? { ...c, x: p.x ?? c.x, y: p.y ?? c.y } : c,
        ),
      };
    }

    case "component.rotate": {
      const ref = op.target ?? "";
      if (!ref) return layout;
      return {
        ...layout,
        components: layout.components.map((c) =>
          c.ref === ref ? { ...c, rotation: p.rotation ?? c.rotation } : c,
        ),
      };
    }

    case "track.add": {
      if (!p.net || !p.points || p.points.length < 2) return layout;
      const track = {
        net: p.net,
        layer: p.layer ?? 0,
        width: p.width && p.width > 0 ? p.width : 0.25,
        points: p.points.map((pt) => ({ x: pt.x, y: pt.y })),
      };
      // Déduplication (ensemble additif) : même polyline déjà présente → no-op.
      const exists = layout.tracks.some(
        (t) => t.net === track.net && t.layer === track.layer && sameTrackPoints(t.points, track.points),
      );
      if (exists) return layout;
      return { ...layout, tracks: [...layout.tracks, track] };
    }

    case "track.remove": {
      const net = p.net ?? op.target ?? "";
      if (!net) return layout;
      return { ...layout, tracks: layout.tracks.filter((t) => t.net !== net) };
    }

    case "via.add": {
      if (!p.net) return layout;
      const via = {
        net: p.net,
        x: p.x ?? 0,
        y: p.y ?? 0,
        from_layer: p.from_layer ?? 0,
        to_layer: p.to_layer ?? 0,
        diameter: p.diameter && p.diameter > 0 ? p.diameter : 0.6,
        drill: p.drill && p.drill > 0 ? p.drill : 0.3,
      };
      const exists = layout.vias.some(
        (v) =>
          v.net === via.net &&
          Math.abs(v.x - via.x) <= VIA_TOLERANCE_MM &&
          Math.abs(v.y - via.y) <= VIA_TOLERANCE_MM,
      );
      if (exists) return layout;
      return { ...layout, vias: [...layout.vias, via] };
    }

    case "via.remove": {
      const net = p.net ?? op.target ?? "";
      if (!net) return layout;
      return {
        ...layout,
        vias: layout.vias.filter(
          (v) =>
            !(
              v.net === net &&
              Math.abs(v.x - (p.x ?? 0)) <= VIA_TOLERANCE_MM &&
              Math.abs(v.y - (p.y ?? 0)) <= VIA_TOLERANCE_MM
            ),
        ),
      };
    }

    case "constraint.width":
      // Les règles de conception ne font pas partie du DTO layout :
      // la mutation est appliquée côté serveur ; l'éditeur rafraîchira
      // les valeurs effectives via les services de classes de nets.
      return layout;

    default:
      return layout;
  }
}

/** Libellé lisible d'une opération (flux d'activité de la CollabBar). */
export function describeCollabOp(op: CollabOp): string {
  const ref = op.target ?? op.payload?.net ?? "";
  switch (op.kind) {
    case "component.move":
      return `${ref} déplacé`;
    case "component.rotate":
      return `${ref} pivoté`;
    case "track.add":
      return `piste ajoutée (${op.payload?.net ?? ""})`;
    case "track.remove":
      return `pistes retirées (${op.payload?.net ?? ref})`;
    case "via.add":
      return `via ajouté (${op.payload?.net ?? ""})`;
    case "via.remove":
      return `via retiré (${op.payload?.net ?? ref})`;
    case "constraint.width":
      return `largeur ${op.payload?.net_class ?? "*"} → ${op.payload?.mm ?? "?"} mm`;
    default:
      return op.kind;
  }
}
