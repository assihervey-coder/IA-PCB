/**
 * application/export/gerber — Génération Gerber RS-274X + Excellon (drill) + ZIP.
 * fileio/writer/gerber_writer.go (équivalent TypeScript).
 * Couches : F.Cu (GTL), B.Cu (GBL), F.Mask (GTS), B.Mask (GBS),
 * F.Silk (GTO), B.Silk (GBO), Edge.Cuts (GKO), drill (.drl).
 */
import { Design, Layer, PlacedPad } from '../../shared/types'
import { ConstraintSet } from '../../domain/constraints/constraint_set'
import JSZip from 'jszip'

const FMT = 6 // décimales (FSLAX36Y36)

const fmt = (mm: number): string => {
  const v = Math.round(mm * 10 ** FMT)
  return (v < 0 ? '-' : '') + Math.abs(v).toString()
}

interface Aperture {
  code: number
  def: string // ex: "C,0.800000" / "R,1.200000X1.400000" / "O,…"
}

class GerberWriter {
  private lines: string[] = []
  private apertures: Aperture[] = []
  private apByDef = new Map<string, number>()
  private currentD = 0

  constructor(private readonly comment: string) {}

  header() {
    this.lines.push(
      'G04 KidCAD-Pro-IA — ' + this.comment + '*',
      'G04 Généré le ' + new Date().toISOString() + '*',
      '%FSLAX36Y36*%',
      '%MOMM*%',
      '%LPD*%',
      'G01*',
      'G04 Endianness: little, X2 drop@2026*',
    )
  }

  aperture(def: string): number {
    let code = this.apByDef.get(def)
    if (code) return code
    code = 10 + this.apertures.length
    this.apertures.push({ code, def })
    this.apByDef.set(def, code)
    return code
  }

  writeApertures() {
    for (const ap of this.apertures) {
      this.lines.push(`%ADD${ap.code}${ap.def}*%`)
    }
  }

  select(d: number) {
    if (this.currentD !== d) {
      this.lines.push(`D${d}*`)
      this.currentD = d
    }
  }

  move(x: number, y: number) {
    this.lines.push(`X${fmt(x)}Y${fmt(y)}D02*`)
  }

  draw(x: number, y: number) {
    this.lines.push(`X${fmt(x)}Y${fmt(y)}D01*`)
  }

  flash(x: number, y: number) {
    this.lines.push(`X${fmt(x)}Y${fmt(y)}D03*`)
  }

  polyline(pts: { x: number; y: number }[]) {
    if (pts.length < 2) return
    this.move(pts[0].x, pts[0].y)
    for (let i = 1; i < pts.length; i++) this.draw(pts[i].x, pts[i].y)
  }

  content(): string {
    this.writeApertures()
    this.lines.push('M02*')
    return this.lines.join('\n') + '\n'
  }
}

function circleAp(d: number) {
  return `C,${d.toFixed(6)}`
}
function rectAp(w: number, h: number) {
  return `R,${w.toFixed(6)}X${h.toFixed(6)}`
}
function ovalAp(w: number, h: number) {
  return `O,${w.toFixed(6)}X${h.toFixed(6)}`
}

/** Pads d'une couche (traversants présents sur les deux). */
function padsForLayer(design: Design, layer: Layer, through: boolean): { pad: PlacedPad; ref: string }[] {
  const out: { pad: PlacedPad; ref: string }[] = []
  for (const fp of design.layout.footprints) {
    for (const pad of fp.pads) {
      const isThrough = pad.layer === 'B.Cu' // convention lib : THT
      if (isThrough ? through : pad.layer === layer) out.push({ pad, ref: fp.ref })
    }
  }
  return out
}

function copperLayer(design: Design, layer: Layer, through: boolean, name: string): string {
  const rules = new ConstraintSet(design.rules).value
  const w = new GerberWriter(`${design.name} — ${name}`)
  w.header()

  // Aperture de base pour pistes
  const trackD = w.aperture(circleAp(rules.trackWidth))
  w.select(trackD)
  for (const t of design.layout.tracks) {
    if (t.layer !== layer) continue
    const d = w.aperture(circleAp(t.width))
    w.select(d)
    w.polyline(t.pts)
  }
  // Pads (flash)
  for (const { pad } of padsForLayer(design, layer, through)) {
    let def: string
    if (pad.shape === 'circle') def = circleAp(Math.max(pad.w, pad.h))
    else if (pad.shape === 'oval') def = ovalAp(pad.w, pad.h)
    else def = rectAp(pad.w, pad.h)
    const d = w.aperture(def)
    w.select(d)
    w.flash(pad.x, pad.y)
  }
  // Vias
  for (const v of design.layout.vias) {
    const d = w.aperture(circleAp(v.diameter))
    w.select(d)
    w.flash(v.x, v.y)
  }
  return w.content()
}

function maskLayer(design: Design, layer: Layer, through: boolean, name: string): string {
  const expand = 0.05
  const w = new GerberWriter(`${design.name} — ${name}`)
  w.header()
  for (const { pad } of padsForLayer(design, layer, through)) {
    const def =
      pad.shape === 'circle'
        ? circleAp(Math.max(pad.w, pad.h) + 2 * expand)
        : pad.shape === 'oval'
          ? ovalAp(pad.w + 2 * expand, pad.h + 2 * expand)
          : rectAp(pad.w + 2 * expand, pad.h + 2 * expand)
    const d = w.aperture(def)
    w.select(d)
    w.flash(pad.x, pad.y)
  }
  for (const v of design.layout.vias) {
    const d = w.aperture(circleAp(v.diameter + 2 * expand))
    w.select(d)
    w.flash(v.x, v.y)
  }
  return w.content()
}

function silkLayer(design: Design, layer: Layer, name: string): string {
  const w = new GerberWriter(`${design.name} — ${name}`)
  w.header()
  const d = w.aperture(circleAp(0.15))
  for (const fp of design.layout.footprints) {
    const onThisSide = layer === 'F.Silk' ? fp.side === 'top' : fp.side === 'bottom'
    if (!onThisSide) continue
    w.select(d)
    const hw = fp.bodyW / 2
    const hh = fp.bodyH / 2
    // Boîte locale → monde (approximation : boîte alignée aux axes de l'empreinte)
    const corners = [
      { x: -hw, y: -hh }, { x: hw, y: -hh }, { x: hw, y: hh }, { x: -hw, y: hh }, { x: -hw, y: -hh },
    ].map((p) => {
      const r = (fp.rotation * Math.PI) / 180
      return { x: fp.x + p.x * Math.cos(r) + p.y * Math.sin(r), y: fp.y - p.x * Math.sin(r) + p.y * Math.cos(r) }
    })
    w.polyline(corners)
  }
  return w.content()
}

function edgeLayer(design: Design, name: string): string {
  const w = new GerberWriter(`${design.name} — ${name}`)
  w.header()
  const d = w.aperture(circleAp(0.1))
  w.select(d)
  const { width, height } = design.board
  w.polyline([
    { x: 0, y: 0 }, { x: width, y: 0 }, { x: width, y: height }, { x: 0, y: height }, { x: 0, y: 0 },
  ])
  return w.content()
}

/** Excellon — perçages (vias + pads traversants), métrique 3 décimales. */
function excellon(design: Design): string {
  const tools = new Map<number, { size: number; hits: { x: number; y: number }[] }>()
  let t = 0
  const addHit = (size: number, x: number, y: number) => {
    const key = Math.round(size * 1000)
    if (!tools.has(key)) tools.set(key, { size, hits: [] })
    tools.get(key)!.hits.push({ x, y })
  }
  for (const v of design.layout.vias) addHit(v.drill, v.x, v.y)
  for (const fp of design.layout.footprints) {
    for (const pad of fp.pads) {
      if (pad.layer === 'B.Cu') addHit(Math.min(pad.w, pad.h) * 0.55, pad.x, pad.y) // THT
    }
  }

  const lines: string[] = [
    '; KidCAD-Pro-IA — excellon drill', `; ${design.name}`,
    'M48', 'FMAT,2', 'METRIC,TZ',
  ]
  const toolIds = new Map<number, number>()
  let tid = 0
  for (const [key, tool] of [...tools.entries()].sort((a, b) => a[0] - b[0])) {
    tid++
    toolIds.set(key, tid)
    lines.push(`T${tid}C${tool.size.toFixed(3)}`)
  }
  lines.push('%', 'G90', 'G05')
  for (const [key, tool] of tools) {
    lines.push(`T${toolIds.get(key)}`)
    for (const h of tool.hits) {
      lines.push(`X${h.x.toFixed(3)}Y${h.y.toFixed(3)}`)
    }
  }
  lines.push('M30')
  return lines.join('\n') + '\n'
}

export interface GerberBundle {
  files: { name: string; content: string }[]
  zip: Buffer
}

export async function generateGerberZip(design: Design): Promise<GerberBundle> {
  const files = [
    { name: `${design.name}-F_Cu.gtl`, content: copperLayer(design, 'F.Cu', false, 'F.Cu') },
    { name: `${design.name}-B_Cu.gbl`, content: copperLayer(design, 'B.Cu', true, 'B.Cu') },
    { name: `${design.name}-F_Mask.gts`, content: maskLayer(design, 'F.Cu', false, 'F.Mask') },
    { name: `${design.name}-B_Mask.gbs`, content: maskLayer(design, 'B.Cu', true, 'B.Mask') },
    { name: `${design.name}-F_Silkscreen.gto`, content: silkLayer(design, 'F.Silk', 'F.Silk') },
    { name: `${design.name}-B_Silkscreen.gbo`, content: silkLayer(design, 'B.Silk', 'B.Silk') },
    { name: `${design.name}-Edge_Cuts.gko`, content: edgeLayer(design, 'Edge.Cuts') },
    { name: `${design.name}.drl`, content: excellon(design) },
  ]
  const zip = new JSZip()
  for (const f of files) zip.file(f.name, f.content)
  zip.file(
    'README-fabrication.txt',
    `Fichiers Gerber RS-274X générés par KidCAD-Pro-IA\nProjet : ${design.name}\nDate : ${new Date().toISOString()}\nCouches : 2 (F.Cu / B.Cu)\nÉpaisseur cuivre conseillée : 35 µm (1 oz)\nFormat : FSLAX36Y36 (6 décimales, mm)\nPerçage : Excellon métrique\n`,
  )
  const buf = await zip.generateAsync({ type: 'nodebuffer', compression: 'DEFLATE' })
  return { files, zip: buf }
}
