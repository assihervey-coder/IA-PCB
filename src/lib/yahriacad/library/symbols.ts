/**
 * library/symbols — Définitions des symboles de schéma (bornes locales).
 * Les composants IC reçoivent leurs bornes dynamiquement (boîte à N broches).
 */
import { SchematicPin, SymbolKind } from '../shared/types'

export interface SymbolDef {
  label: string
  w: number // boîte d'encombrement du symbole (mm)
  h: number
  pinsFor(nPins: number): SchematicPin[]
}

function twoPins(vertical: boolean, span: number): SchematicPin[] {
  return vertical
    ? [
        { id: '1', name: '1', x: 0, y: -span / 2 },
        { id: '2', name: '2', x: 0, y: span / 2 },
      ]
    : [
        { id: '1', name: '1', x: -span / 2, y: 0 },
        { id: '2', name: '2', x: span / 2, y: 0 },
      ]
}

export const SYMBOL_DEFS: Record<SymbolKind, SymbolDef> = {
  R: {
    label: 'Résistance', w: 4, h: 10,
    pinsFor: () => twoPins(true, 10),
  },
  C: {
    label: 'Condensateur', w: 4, h: 7,
    pinsFor: () => twoPins(true, 7),
  },
  CP: {
    label: 'Cond. polarisé', w: 4, h: 7,
    pinsFor: () => [
      { id: '+', name: '+', x: 0, y: -3.5 },
      { id: '-', name: '-', x: 0, y: 3.5 },
    ],
  },
  D: {
    label: 'Diode', w: 4, h: 8,
    pinsFor: () => [
      { id: 'A', name: 'A', x: 0, y: -4 },
      { id: 'K', name: 'K', x: 0, y: 4 },
    ],
  },
  LED: {
    label: 'LED', w: 4, h: 8,
    pinsFor: () => [
      { id: 'A', name: 'A', x: 0, y: -4 },
      { id: 'K', name: 'K', x: 0, y: 4 },
    ],
  },
  IC: {
    label: 'Circuit intégré', w: 14, h: 18,
    pinsFor: (n = 8) => {
      const per = Math.ceil(n / 2)
      const pins: SchematicPin[] = []
      for (let i = 0; i < per; i++) {
        pins.push({ id: `${i + 1}`, name: `${i + 1}`, x: -7, y: -((per - 1) * 2.2) / 2 + i * 2.2 })
      }
      for (let i = 0; i < n - per; i++) {
        pins.push({ id: `${n - i}`, name: `${n - i}`, x: 7, y: -((per - 1) * 2.2) / 2 + i * 2.2 })
      }
      return pins
    },
  },
  J: {
    label: 'Connecteur', w: 8, h: 8,
    pinsFor: (n = 2) => {
      const pins: SchematicPin[] = []
      for (let i = 0; i < n; i++) {
        pins.push({ id: `${i + 1}`, name: `${i + 1}`, x: 4, y: -((n - 1) * 3) / 2 + i * 3 })
      }
      return pins
    },
  },
  Y: {
    label: 'Quartz', w: 4, h: 7,
    pinsFor: () => twoPins(true, 7),
  },
  Q: {
    label: 'Transistor / Régulateur', w: 8, h: 8,
    pinsFor: () => [
      { id: '1', name: '1', x: -4, y: 3 },
      { id: '2', name: '2', x: 0, y: 3 },
      { id: '3', name: '3', x: 4, y: 3 },
    ],
  },
}

/** Crée les bornes d'un nouveau composant selon le type de symbole. */
export function makePins(kind: SymbolKind, pinCount?: number): SchematicPin[] {
  const def = SYMBOL_DEFS[kind]
  const n = pinCount ?? (kind === 'IC' ? 8 : kind === 'J' ? 2 : 2)
  return def.pinsFor(n)
}
