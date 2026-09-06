/**
 * domain/layout — Empreintes placées, pistes, vias : helpers de manipulation.
 */
import { FootprintInst, Layer, Point, Track, Via } from '../../shared/types'
import { rotate, toWorld } from '../../pkg/geometry'
import { uid } from '../../pkg/utils'

/** Déplace une empreinte (translation des pads absolus). */
export function moveFootprint(fp: FootprintInst, x: number, y: number) {
  const dx = x - fp.x
  const dy = y - fp.y
  fp.x = x
  fp.y = y
  for (const pad of fp.pads) {
    pad.x += dx
    pad.y += dy
  }
}

/** Applique une rotation supplémentaire (degrés, sens horaire) autour du centre. */
export function rotateFootprint(fp: FootprintInst, deltaDeg: number) {
  fp.rotation = ((fp.rotation + deltaDeg) % 360 + 360) % 360
  for (const pad of fp.pads) {
    const rel = { x: pad.x - fp.x, y: pad.y - fp.y }
    const r = rotate(rel, deltaDeg)
    pad.x = fp.x + r.x
    pad.y = fp.y + r.y
  }
}

/** Passe l'empreinte sur l'autre face (les pads changent de couche). */
export function flipFootprint(fp: FootprintInst) {
  fp.side = fp.side === 'top' ? 'bottom' : 'top'
  for (const pad of fp.pads) {
    pad.layer = pad.layer === 'F.Cu' ? 'B.Cu' : 'F.Cu'
  }
}

/** Recalcule la position absolue d'une borne de composant de schéma. */
export function padPosition(fp: FootprintInst, padNumber: string): Point | null {
  const pad = fp.pads.find((p) => p.number === padNumber)
  return pad ? { x: pad.x, y: pad.y } : null
}

/** Toutes les positions de pads d'une empreinte par numéro. */
export function padMap(fp: FootprintInst): Map<string, Point> {
  const m = new Map<string, Point>()
  for (const pad of fp.pads) m.set(pad.number, { x: pad.x, y: pad.y })
  return m
}

/** Crée une piste à partir d'un chemin de points. */
export function makeTrack(netId: string, layer: Layer, width: number, pts: Point[]): Track {
  return { id: uid('t'), netId, layer, width, pts }
}

/** Crée un via. */
export function makeVia(netId: string, x: number, y: number, drill: number, diameter: number): Via {
  return { id: uid('v'), netId, x, y, drill, diameter }
}

/** Longueur totale de cuivre (mm). */
export function totalWireLength(tracks: Track[]): number {
  let l = 0
  for (const t of tracks) {
    for (let i = 1; i < t.pts.length; i++) {
      l += Math.hypot(t.pts[i].x - t.pts[i - 1].x, t.pts[i].y - t.pts[i - 1].y)
    }
  }
  return l
}

/** Net déjà routé ? (présence de pistes reliant au moins 2 pads) */
export function netRouted(tracks: Track[], netId: string): boolean {
  return tracks.some((t) => t.netId === netId)
}
