/**
 * application/schematic/import — Import de netlists (KiCad s-expression, JSON natif).
 * fileio/reader/kicad.go (équivalent TypeScript).
 */
import { Design, Net, SchematicComponent, SymbolKind } from '../../shared/types'
import { instantiateFootprint } from '../../library/footprints'
import { makePins } from '../../library/symbols'
import { uid } from '../../pkg/utils'

// ---------------------------------------------------------------------------
// Parseur s-expression générique
// ---------------------------------------------------------------------------
export type SExprNode = string | SExprNode[]

export function parseSExpr(text: string): SExprNode[] {
  let i = 0
  const n = text.length

  function skipWs() {
    while (i < n) {
      const c = text[i]
      if (c === ' ' || c === '\t' || c === '\r' || c === '\n') i++
      else break
    }
  }

  function parseAtom(): string {
    if (text[i] === '"') {
      i++
      let out = ''
      while (i < n && text[i] !== '"') {
        if (text[i] === '\\' && i + 1 < n) {
          out += text[i + 1]
          i += 2
        } else {
          out += text[i++]
        }
      }
      i++ // guillemet fermant
      return out
    }
    let start = i
    while (i < n && !' \t\r\n()'.includes(text[i])) i++
    return text.slice(start, i)
  }

  function parseList(): SExprNode[] {
    const out: SExprNode[] = []
    skipWs()
    // appelé après '(' consommé
    for (;;) {
      skipWs()
      if (i >= n) break
      const c = text[i]
      if (c === ')') {
        i++
        break
      }
      if (c === '(') {
        i++
        out.push(parseList())
      } else {
        out.push(parseAtom())
      }
    }
    return out
  }

  const top: SExprNode[] = []
  for (;;) {
    skipWs()
    if (i >= n) break
    if (text[i] === '(') {
      i++
      top.push(parseList())
    } else {
      top.push(parseAtom())
    }
  }
  return top
}

/** Enfants d'une liste dont le premier atome vaut `tag`. */
function children(node: SExprNode, tag: string): SExprNode[] {
  if (!Array.isArray(node)) return []
  return node.filter((c) => Array.isArray(c) && (c[0] as string) === tag)
}

function firstString(node: SExprNode, tag: string): string | null {
  if (!Array.isArray(node)) return null
  for (const c of node) {
    if (Array.isArray(c) && c[0] === tag && typeof c[1] === 'string') return c[1]
  }
  return null
}

// ---------------------------------------------------------------------------
// Résolution d'empreinte depuis le champ KiCad "Lib:Name"
// ---------------------------------------------------------------------------
export function resolveFootprint(field: string, ref: string, pinCount: number): string {
  const name = field.includes(':') ? field.split(':')[1] : field
  const direct = name && !name.includes('*') ? name : ''
  const m = (re: RegExp) => re.exec(name ?? '') ?? re.exec(field ?? '')

  let r = direct ? checkDirect(direct) : null
  if (r) return r

  const dip = m(/DIP[-_]?(\d+)/i)
  if (dip) return `DIP-${dip[1]}`
  const soic = m(/SOIC[-_]?(\d+)/i)
  if (soic) return `SOIC-${soic[1]}`
  if (/0805/i.test(name ?? '')) return /^C/i.test(ref) ? 'C-0805' : 'R-0805'
  if (/LED/i.test(name ?? '')) return 'LED-5'
  const hdr = m(/HDR-?1x(\d+)/i) ?? m(/header_?1x(\d+)/i)
  if (hdr) return `HDR-1x${hdr[1]}`
  if (/CP|elec|electrolyt/i.test(name ?? '')) return 'CP-ELEC-8'
  if (/crystal|quartz|hc-?49/i.test(name ?? '')) return 'HC-49'
  if (/to-?220/i.test(name ?? '')) return 'TO-220'
  if (/axial/i.test(name ?? '')) return 'AXIAL-0.4'
  return pinCount > 2 ? 'DIP-8' : 'R-0805'
}

function checkDirect(name: string): string | null {
  const known = [
    'DIP-8', 'DIP-14', 'DIP-16', 'DIP-28', 'SOIC-8', 'SOIC-16',
    'R-0805', 'C-0805', 'LED-5', 'CP-ELEC-8', 'AXIAL-0.4', 'HC-49', 'TO-220',
    'HDR-1x2', 'HDR-1x3', 'HDR-1x4',
  ]
  return known.includes(name) ? name : null
}

// ---------------------------------------------------------------------------
// Netlist KiCad → Design (nets + composants + empreintes)
// ---------------------------------------------------------------------------
export interface ImportNetlistResult {
  nets: Net[]
  components: SchematicComponent[]
  footprintsCreated: number
  warnings: string[]
}

export function importKicadNetlist(text: string): ImportNetlistResult {
  const top = parseSExpr(text)
  const exportNode = top.find((n) => Array.isArray(n) && n[0] === 'export')
  if (!exportNode || !Array.isArray(exportNode)) {
    throw new Error('Netlist KiCad invalide : nœud (export …) introuvable')
  }
  const warnings: string[] = []
  const componentsNodes = children(exportNode, 'components')
  const netsNodes = children(exportNode, 'nets')

  const pinCounts = new Map<string, string[]>() // ref → noms de bornes
  const values = new Map<string, string>()
  const footprints = new Map<string, string>()

  for (const compsNode of componentsNodes) {
    for (const comp of children(compsNode as SExprNode, 'comp')) {
      const ref = firstString(comp, 'ref')
      if (!ref) continue
      values.set(ref, firstString(comp, 'value') ?? '')
      footprints.set(ref, firstString(comp, 'footprint') ?? '')
      const pins = children(comp, 'pins')
      const pinNames: string[] = []
      for (const pinsNode of pins) {
        for (const pin of children(pinsNode as SExprNode, 'pin')) {
          const num = firstString(pin, 'num')
          if (num) pinNames.push(num)
        }
      }
      pinCounts.set(ref, pinNames)
    }
  }

  // Composants de schéma (pour ERC / affichage)
  const components: SchematicComponent[] = []
  const refToCompId = new Map<string, string>()
  let idx = 0
  for (const [ref, pinNames] of pinCounts) {
    const kind: SymbolKind = inferSymbolKind(ref, values.get(ref) ?? '', pinNames.length)
    const comp: SchematicComponent = {
      id: `imp_${++idx}`,
      ref,
      value: values.get(ref) ?? '',
      symbol: kind,
      pins: makePinsFor(kind, pinNames),
      x: 20 + ((idx - 1) % 6) * 30,
      y: 20 + Math.floor((idx - 1) / 6) * 24,
      rotation: 0,
      footprintName: resolveFootprint(footprints.get(ref) ?? '', ref, pinNames.length),
    }
    components.push(comp)
    refToCompId.set(ref, comp.id)
  }

  // Nets
  const nets: Net[] = []
  let netIdx = 0
  for (const netsNode of netsNodes) {
    for (const netNode of children(netsNode as SExprNode, 'net')) {
      const name = firstString(netNode, 'name') ?? `N$${++netIdx}`
      const pins: { componentId: string; pad: string }[] = []
      for (const node of children(netNode as SExprNode, 'node')) {
        const ref = firstString(node, 'ref')
        const pin = firstString(node, 'pin')
        if (ref && pin) {
          const compId = refToCompId.get(ref)
          if (compId) pins.push({ componentId: compId, pad: pin })
          else warnings.push(`Net ${name} : composant ${ref} absent de (components)`)
        }
      }
      nets.push({ id: `impnet_${++netIdx}`, name, pins })
    }
  }

  return { nets, components, footprintsCreated: pinCounts.size, warnings }
}

function inferSymbolKind(ref: string, value: string, pinCount: number): SymbolKind {
  if (/^R\d/i.test(ref)) return 'R'
  if (/^C\d/i.test(ref)) return /µ|u|elec/i.test(value) ? 'CP' : 'C'
  if (/^D\d/i.test(ref)) return /led/i.test(value) ? 'LED' : 'D'
  if (/^J\d|^P\d|^CN\d/i.test(ref)) return 'J'
  if (/^Y\d|^X\d|^QZ\d/i.test(ref)) return 'Y'
  if (/^U\d|^IC\d|^Q\d/i.test(ref)) return 'IC'
  return pinCount > 2 ? 'IC' : 'R'
}

function makePinsFor(kind: SymbolKind, pinNames: string[]): ReturnType<typeof makePins> {
  if (kind === 'IC') return makePins('IC', Math.max(2, pinNames.length))
  if (kind === 'J') return makePins('J', Math.max(2, pinNames.length))
  if (pinNames.some((p) => p === 'A' || p === 'K')) return makePins('LED')
  if (pinNames.some((p) => p === '+' || p === '-')) return makePins('CP')
  return makePins(kind)
}

/** Applique un résultat d'import sur un design existant (remplace netlist + composants). */
export function applyNetlistImport(design: Design, result: ImportNetlistResult): void {
  design.nets = result.nets
  design.schematic = { components: result.components, wires: [] }
  design.layout.tracks = []
  design.layout.vias = []
  // Empreintes : conserve celles qui correspondent, recrée le reste
  const byCompId = new Map(design.layout.footprints.map((f) => [f.componentId, f]))
  const newFootprints: typeof design.layout.footprints = []
  for (const comp of result.components) {
    const existing = byCompId.get(comp.id)
    if (existing && existing.footprintName === comp.footprintName) {
      newFootprints.push(existing)
      continue
    }
    const fp = instantiateFootprint(comp.footprintName, {
      componentId: comp.id, ref: comp.ref, value: comp.value,
      x: 10 + Math.random() * (design.board.width - 20),
      y: 10 + Math.random() * (design.board.height - 20),
    })
    if (fp) newFootprints.push(fp)
  }
  design.layout.footprints = newFootprints
}
