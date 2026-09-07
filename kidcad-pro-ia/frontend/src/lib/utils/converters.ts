/**
 * Utilitaires de conversion et de formatage (unités, dates FR, couleurs de nets).
 */

export function mmToMil(mm: number): number {
  return mm / 0.0254;
}

export function milToMm(mil: number): number {
  return mil * 0.0254;
}

const numberFr = (decimals: number): Intl.NumberFormat =>
  new Intl.NumberFormat("fr-FR", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });

export function formatMm(value: number, decimals = 2): string {
  return `${numberFr(decimals).format(value)} mm`;
}

export function percentFormat(value: number): string {
  return `${new Intl.NumberFormat("fr-FR", { maximumFractionDigits: 0 }).format(value)} %`;
}

export function formatDateFR(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("fr-FR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

/** Couleur déterministe pour un net (hachage → teinte HSL lisible sur fond sombre). */
export function colorForNet(name: string): string {
  let hash = 5381;
  for (let i = 0; i < name.length; i++) {
    hash = ((hash << 5) + hash + name.charCodeAt(i)) >>> 0;
  }
  const hue = hash % 360;
  const lightness = 55 + (hash % 3) * 6;
  return `hsl(${hue} 72% ${lightness}%)`;
}

/** Déclenche le téléchargement d'un blob côté navigateur. */
export function downloadBlob(blob: Blob, filename: string): void {
  if (typeof window === "undefined") return;
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}
