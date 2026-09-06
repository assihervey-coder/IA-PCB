/**
 * library/footprints — Bibliothèque d'empreintes paramétriques (mm).
 * Chaque empreinte : corps + pads en coordonnées LOCALES (non tournées).
 */
import { FootprintInst, Layer, PadShape, PlacedPad, Side } from '../shared/types'
import { rotate } from '../pkg/geometry'
import { uid } from '../pkg/utils'

export interface LibPad {
  number: string
  dx: number
  dy: number
  w: number
  h: number
  shape: PadShape
  layer: Layer
}

export interface LibFootprint {
  name: string
  category: string
  bodyW: number
  bodyH: number
  pads: LibPad[]
}

const SMD: Layer = 'F.Cu'
const THT: Layer = 'B.Cu' // perçages : pastilles visibles côté opposé aussi (simplification)

function dip(n: number): LibFootprint {
  const rows = n / 2
  const pitch = 2.54
  const rowGap = 7.62
  const pads: LibPad[] = []
  for (let i = 0; i < rows; i++) {
    pads.push({ number: `${i + 1}`, dx: -rowGap / 2, dy: -((rows - 1) * pitch) / 2 + i * pitch, w: 1.6, h: 1.6, shape: 'circle', layer: THT })
  }
  for (let i = 0; i < rows; i++) {
    pads.push({ number: `${n - i}`, dx: rowGap / 2, dy: -((rows - 1) * pitch) / 2 + i * pitch, w: 1.6, h: 1.6, shape: 'circle', layer: THT })
  }
  return { name: `DIP-${n}`, category: 'CI traversant', bodyW: rowGap + 3.4, bodyH: (rows - 1) * pitch + 3.4, pads }
}

function soic(n: number): LibFootprint {
  const perRow = n / 2
  const pitch = 1.27
  const pads: LibPad[] = []
  for (let i = 0; i < perRow; i++) {
    pads.push({ number: `${i + 1}`, dx: -2.7, dy: -((perRow - 1) * pitch) / 2 + i * pitch, w: 1.5, h: 0.6, shape: 'rect', layer: SMD })
  }
  for (let i = 0; i < perRow; i++) {
    pads.push({ number: `${n - i}`, dx: 2.7, dy: -((perRow - 1) * pitch) / 2 + i * pitch, w: 1.5, h: 0.6, shape: 'rect', layer: SMD })
  }
  return { name: `SOIC-${n}`, category: 'CI CMS', bodyW: 5.4, bodyH: (perRow - 1) * pitch + 2.2, pads }
}

function chip0805(prefix: string): LibFootprint {
  return {
    name: prefix,
    category: 'Passifs CMS',
    bodyW: 2.0,
    bodyH: 1.25,
    pads: [
      { number: '1', dx: -1.65, dy: 0, w: 1.2, h: 1.4, shape: 'rect', layer: SMD },
      { number: '2', dx: 1.65, dy: 0, w: 1.2, h: 1.4, shape: 'rect', layer: SMD },
    ],
  }
}

function led5(): LibFootprint {
  return {
    name: 'LED-5',
    category: 'Traversant',
    bodyW: 5,
    bodyH: 5,
    pads: [
      { number: 'A', dx: -1.27, dy: 0, w: 1.7, h: 1.7, shape: 'circle', layer: THT },
      { number: 'K', dx: 1.27, dy: 0, w: 1.7, h: 1.7, shape: 'circle', layer: THT },
    ],
  }
}

function cpElec8(): LibFootprint {
  return {
    name: 'CP-ELEC-8',
    category: 'Condensateurs',
    bodyW: 8,
    bodyH: 8,
    pads: [
      { number: '+', dx: -1.75, dy: 0, w: 1.6, h: 1.6, shape: 'circle', layer: THT },
      { number: '-', dx: 1.75, dy: 0, w: 1.6, h: 1.6, shape: 'circle', layer: THT },
    ],
  }
}

function axial(): LibFootprint {
  return {
    name: 'AXIAL-0.4',
    category: 'Passifs traversant',
    bodyW: 7.5,
    bodyH: 2.6,
    pads: [
      { number: '1', dx: -5.08, dy: 0, w: 1.5, h: 1.5, shape: 'circle', layer: THT },
      { number: '2', dx: 5.08, dy: 0, w: 1.5, h: 1.5, shape: 'circle', layer: THT },
    ],
  }
}

function hc49(): LibFootprint {
  return {
    name: 'HC-49',
    category: 'Quartz',
    bodyW: 11,
    bodyH: 4.8,
    pads: [
      { number: '1', dx: -4.65, dy: 0, w: 1.6, h: 2.4, shape: 'rect', layer: THT },
      { number: '2', dx: 4.65, dy: 0, w: 1.6, h: 2.4, shape: 'rect', layer: THT },
    ],
  }
}

function to220(): LibFootprint {
  return {
    name: 'TO-220',
    category: 'Régulateurs',
    bodyW: 10.2,
    bodyH: 9,
    pads: [
      { number: '1', dx: -2.54, dy: 3.8, w: 1.8, h: 1.6, shape: 'rect', layer: THT },
      { number: '2', dx: 0, dy: 3.8, w: 1.8, h: 1.6, shape: 'rect', layer: THT },
      { number: '3', dx: 2.54, dy: 3.8, w: 1.8, h: 1.6, shape: 'rect', layer: THT },
    ],
  }
}

function header1xN(n: number): LibFootprint {
  const pitch = 2.54
  const pads: LibPad[] = []
  for (let i = 0; i < n; i++) {
    pads.push({ number: `${i + 1}`, dx: 0, dy: -((n - 1) * pitch) / 2 + i * pitch, w: 1.8, h: 1.8, shape: 'rect', layer: THT })
  }
  return { name: `HDR-1x${n}`, category: 'Connecteurs', bodyW: 2.7, bodyH: (n - 1) * pitch + 2.7, pads }
}

const LIB: LibFootprint[] = [
  dip(8), dip(14), dip(16), dip(28),
  soic(8), soic(16),
  chip0805('R-0805'), chip0805('C-0805'),
  led5(), cpElec8(), axial(), hc49(), to220(),
  header1xN(2), header1xN(3), header1xN(4),
]

export function listFootprints(): LibFootprint[] {
  return LIB
}

export function getFootprint(name: string): LibFootprint | null {
  return LIB.find((f) => f.name === name) ?? null
}

/** Instancie une empreinte à une position (pads en absolu, rotation appliquée). */
export function instantiateFootprint(
  libName: string,
  opts: {
    componentId: string
    ref: string
    value: string
    x: number
    y: number
    rotation?: number
    side?: Side
    id?: string
  },
): FootprintInst | null {
  const lib = getFootprint(libName)
  if (!lib) return null
  const rotation = opts.rotation ?? 0
  const side: Side = opts.side ?? 'top'
  const pads: PlacedPad[] = lib.pads.map((p) => {
    const r = rotate({ x: p.dx, y: p.dy }, rotation)
    return {
      number: p.number,
      x: opts.x + r.x,
      y: opts.y + r.y,
      w: p.w,
      h: p.h,
      shape: p.shape,
      layer: side === 'top' ? p.layer : p.layer === 'F.Cu' ? 'B.Cu' : 'F.Cu',
      netId: null,
    }
  })
  return {
    id: opts.id ?? uid('fp'),
    componentId: opts.componentId,
    ref: opts.ref,
    value: opts.value,
    footprintName: libName,
    x: opts.x,
    y: opts.y,
    rotation,
    side,
    bodyW: lib.bodyW,
    bodyH: lib.bodyH,
    pads,
  }
}

/** Défauts raisonnables par symbole (pour l'ajout depuis l'éditeur schéma). */
export const DEFAULT_FOOTPRINT_BY_SYMBOL: Record<string, string> = {
  R: 'R-0805',
  C: 'C-0805',
  CP: 'CP-ELEC-8',
  D: 'LED-5',
  LED: 'LED-5',
  IC: 'DIP-8',
  J: 'HDR-1x2',
  Y: 'HC-49',
  Q: 'TO-220',
}
