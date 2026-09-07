/**
 * pkg/utils — Fonctions utilitaires (UUID, conversions d'unités, formatage).
 */

/** Identifiant unique court (uuid v4 tronqué, suffisant pour un design). */
export function uid(prefix = ''): string {
  const raw =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
  return prefix ? `${prefix}_${raw.replace(/-/g, '').slice(0, 12)}` : raw.replace(/-/g, '').slice(0, 12)
}

export const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

export const snap = (v: number, step: number) => Math.round(v / step) * step

/** mm → mil */
export const mmToMil = (mm: number) => mm / 0.0254
/** mil → mm */
export const milToMm = (mil: number) => mil * 0.0254

/** Formatage distances pour l'UI. */
export function fmtMm(mm: number, digits = 2): string {
  if (!Number.isFinite(mm)) return '—'
  if (mm >= 100) return `${mm.toFixed(1)} mm`
  return `${mm.toFixed(digits)} mm`
}

/** Formatage durée. */
export function fmtDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`
  return `${Math.floor(ms / 60000)} min ${Math.round((ms % 60000) / 1000)} s`
}

/** Référence suivante : R1 → R2 (pour l'auto-annotation). */
export function nextRef(prefix: string, existing: string[]): string {
  const max = existing
    .filter((r) => r.startsWith(prefix))
    .map((r) => parseInt(r.slice(prefix.length), 10) || 0)
    .reduce((a, b) => Math.max(a, b), 0)
  return `${prefix}${max + 1}`
}
