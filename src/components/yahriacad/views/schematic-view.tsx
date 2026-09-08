'use client'

/**
 * views/schematic-view — Éditeur de schéma électrique (SVG) :
 * placement de symboles, câblage borne-à-borne, extraction de netlist,
 * propriétés du composant sélectionné. ViewBox en unités monde (mm).
 */
import { useCallback, useMemo, useRef, useState } from 'react'
import {
  Eraser, GitBranch, MousePointer2, Plus, RotateCw, Route, Spline, Trash2, Zap,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Design, SchematicComponent, SymbolKind } from '@/lib/yahriacad/shared/types'
import { extractNetlist, pinWorldPos } from '@/lib/yahriacad/domain/schematic/schematic'
import { makePins } from '@/lib/yahriacad/library/symbols'
import { instantiateFootprint } from '@/lib/yahriacad/library/footprints'
import { uid } from '@/lib/yahriacad/pkg/utils'
import { useProjectStore } from '@/lib/yahriacad/client/stores/project-store'
import { useUiStore } from '@/lib/yahriacad/client/stores/ui-store'

const GRID = 2.54 // mm

const PALETTE: { kind: SymbolKind; label: string; pins?: number }[] = [
  { kind: 'R', label: 'Résistance' },
  { kind: 'C', label: 'Condensateur' },
  { kind: 'CP', label: 'Cond. polarisé' },
  { kind: 'LED', label: 'LED' },
  { kind: 'IC', label: 'CI 8 broches', pins: 8 },
  { kind: 'IC', label: 'CI 16 broches', pins: 16 },
  { kind: 'J', label: 'Connecteur', pins: 2 },
  { kind: 'Y', label: 'Quartz' },
]

const DEFAULT_FP: Record<string, string> = {
  R: 'R-0805', C: 'C-0805', CP: 'CP-ELEC-8', LED: 'LED-5', IC: 'DIP-8', J: 'HDR-1x2', Y: 'HC-49', Q: 'TO-220',
}

export default function SchematicView() {
  const { design, update } = useProjectStore()
  const ui = useUiStore()
  const svgRef = useRef<SVGSVGElement>(null)
  const wireStart = useRef<{ compId: string; pin: string; x: number; y: number } | null>(null)
  const pendingAdd = useRef<{ kind: SymbolKind; pins?: number } | null>(null)
  const [preview, setPreview] = useState<{ x1: number; y1: number; x2: number; y2: number } | null>(null)
  const [info, setInfo] = useState<string>('')

  // ViewBox monde (mm) couvrant les composants avec marge
  const vb = useMemo(() => {
    if (!design || design.schematic.components.length === 0) return { x: 0, y: 0, w: 160, h: 100 }
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
    for (const c of design.schematic.components) {
      minX = Math.min(minX, c.x - 15)
      minY = Math.min(minY, c.y - 15)
      maxX = Math.max(maxX, c.x + 15)
      maxY = Math.max(maxY, c.y + 15)
    }
    const w = Math.max(160, maxX - minX)
    const h = Math.max(100, maxY - minY)
    return { x: minX, y: minY, w, h }
  }, [design])

  const toWorld = useCallback(
    (e: { clientX: number; clientY: number }) => {
      const rect = svgRef.current!.getBoundingClientRect()
      const sx = rect.width / vb.w
      const sy = rect.height / vb.h
      return {
        x: (e.clientX - rect.left) / sx + vb.x,
        y: (e.clientY - rect.top) / sy + vb.y,
      }
    },
    [vb],
  )

  const snap = (v: number) => Math.round(v / GRID) * GRID
  const refOf = (d: Design, compId: string) => d.schematic.components.find((c) => c.id === compId)?.ref ?? '?'

  const hitPin = (p: { x: number; y: number }) => {
    if (!design) return null
    for (const comp of design.schematic.components) {
      for (const pin of comp.pins) {
        const w = pinWorldPos(comp, pin)
        if (Math.hypot(w.x - p.x, w.y - p.y) < 2.2) return { compId: comp.id, pin: pin.id, x: w.x, y: w.y }
      }
    }
    return null
  }

  const onClick = (e: React.MouseEvent) => {
    if (!design) return
    const p = toWorld(e)
    if (ui.schematicTool === 'wire') {
      const pin = hitPin(p)
      if (!wireStart.current) {
        if (pin) {
          wireStart.current = pin
          setPreview({ x1: pin.x, y1: pin.y, x2: p.x, y2: p.y })
          setInfo(`Départ : ${refOf(design, pin.compId)}.${pin.pin} — cliquez la borne d'arrivée`)
        }
        return
      }
      if (pin && (pin.compId !== wireStart.current.compId || pin.pin !== wireStart.current.pin)) {
        const a = wireStart.current
        update((d) => {
          d.schematic.wires.push({ id: uid('w'), pts: [{ x: a.x, y: a.y }, { x: pin.x, y: pin.y }] })
        })
        setInfo(`Fil créé : ${refOf(design, a.compId)}.${a.pin} → ${refOf(design, pin.compId)}.${pin.pin}`)
      }
      wireStart.current = null
      setPreview(null)
      return
    }
    if (ui.schematicTool === 'add' && pendingAdd.current) {
      const { kind, pins } = pendingAdd.current
      pendingAdd.current = null
      ui.setSchematicTool('select')
      update((d) => {
        const n = d.schematic.components.length
        const prefix: Record<string, string> = { R: 'R', C: 'C', CP: 'C', LED: 'D', D: 'D', IC: 'U', J: 'J', Y: 'Y', Q: 'Q' }
        const ref = `${prefix[kind] ?? 'U'}${n + 1}`
        d.schematic.components.push({
          id: uid('sc'),
          ref,
          value: kind === 'R' ? '10k' : kind === 'C' ? '100nF' : kind === 'LED' ? 'LED' : kind,
          symbol: kind,
          pins: makePins(kind, pins),
          x: snap(p.x),
          y: snap(p.y),
          rotation: 0,
          footprintName: DEFAULT_FP[kind] ?? 'R-0805',
        })
      })
      return
    }
    ui.setSelectedComponentId(null)
  }

  const onMouseMove = (e: React.MouseEvent) => {
    if (wireStart.current) {
      const p = toWorld(e)
      setPreview({ x1: wireStart.current.x, y1: wireStart.current.y, x2: p.x, y2: p.y })
    }
  }

  const startDrag = (comp: SchematicComponent) => (e: React.MouseEvent) => {
    e.stopPropagation()
    if (ui.schematicTool !== 'select') return
    ui.setSelectedComponentId(comp.id)
    const p0 = toWorld(e)
    const offX = p0.x - comp.x
    const offY = p0.y - comp.y
    const onMove = (ev: MouseEvent) => {
      const p = toWorld(ev)
      const nx = snap(p.x - offX)
      const ny = snap(p.y - offY)
      update((d) => {
        const c = d.schematic.components.find((cc) => cc.id === comp.id)
        if (!c) return
        c.x = nx
        c.y = ny
      })
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  /** Extraction de netlist : fils → nets, puis affectation aux pads du layout. */
  const extractNets = () => {
    if (!design) return
    const ex = extractNetlist(design.schematic)
    update((d) => {
      if (ex.nets.length > 0) d.nets = ex.nets
      for (const fp of d.layout.footprints) for (const pad of fp.pads) pad.netId = null
      const fpByComp = new Map(d.layout.footprints.map((f) => [f.componentId, f]))
      for (const net of d.nets) {
        for (const pin of net.pins) {
          const pad = fpByComp.get(pin.componentId)?.pads.find((p) => p.number === pin.pad)
          if (pad) pad.netId = net.id
        }
      }
    })
    setInfo(
      ex.nets.length > 0
        ? `Netlist extraite : ${ex.nets.length} nets${ex.unconnectedPins.length ? `, ${ex.unconnectedPins.length} borne(s) non connectée(s)` : ''}`
        : 'Aucun net extrait — câblez des bornes avec l\'outil Fil.',
    )
  }

  /** Crée les empreintes manquantes dans le layout à partir du schéma. */
  const pushToLayout = () => {
    if (!design) return
    update((d) => {
      const existing = new Set(d.layout.footprints.map((f) => f.componentId))
      for (const comp of d.schematic.components) {
        if (existing.has(comp.id)) continue
        const fp = instantiateFootprint(comp.footprintName, {
          componentId: comp.id, ref: comp.ref, value: comp.value,
          x: 10 + Math.random() * (d.board.width - 20),
          y: 10 + Math.random() * (d.board.height - 20),
        })
        if (fp) d.layout.footprints.push(fp)
      }
    })
    setInfo('Empreintes ajoutées au layout — passez à la vue PCB.')
  }

  const selected = useMemo(
    () => design?.schematic.components.find((c) => c.id === ui.selectedComponentId) ?? null,
    [design, ui.selectedComponentId],
  )

  if (!design) return null

  return (
    <div className="flex h-full">
      {/* Canvas SVG */}
      <div className="relative min-w-0 flex-1 overflow-hidden bg-zinc-950">
        <svg
          ref={svgRef}
          className="h-full w-full"
          viewBox={`${vb.x} ${vb.y} ${vb.w} ${vb.h}`}
          preserveAspectRatio="xMidYMid meet"
          onClick={onClick}
          onMouseMove={onMouseMove}
          style={{ cursor: ui.schematicTool === 'wire' ? 'crosshair' : 'default' }}
        >
          <defs>
            <pattern id="sgrid" width={GRID} height={GRID} patternUnits="userSpaceOnUse">
              <circle cx={0.2} cy={0.2} r={0.12} fill="#26272d" />
            </pattern>
          </defs>
          <rect x={vb.x} y={vb.y} width={vb.w} height={vb.h} fill="url(#sgrid)" />

          {/* Fils */}
          {design.schematic.wires.map((w) => (
            <polyline
              key={w.id}
              points={w.pts.map((p) => `${p.x},${p.y}`).join(' ')}
              fill="none"
              stroke="#10b981"
              strokeWidth={0.28}
              className="cursor-pointer"
              onClick={(e) => {
                e.stopPropagation()
                update((d) => { d.schematic.wires = d.schematic.wires.filter((x) => x.id !== w.id) })
                setInfo('Fil supprimé')
              }}
            />
          ))}
          {preview && (
            <line
              x1={preview.x1} y1={preview.y1}
              x2={preview.x2} y2={preview.y2}
              stroke="#f59e0b" strokeWidth={0.24} strokeDasharray="0.8 0.6"
            />
          )}

          {/* Composants */}
          {design.schematic.components.map((comp) => (
            <Symbol key={comp.id} comp={comp} selected={comp.id === ui.selectedComponentId} onMouseDown={startDrag(comp)} />
          ))}
        </svg>

        {/* Outils flottants */}
        <div className="absolute left-3 top-3 flex items-center gap-1 rounded-md border border-zinc-800 bg-zinc-900/90 p-1 shadow-lg">
          <STool active={ui.schematicTool === 'select'} onClick={() => ui.setSchematicTool('select')} title="Sélection" icon={<MousePointer2 className="h-4 w-4" />} />
          <STool active={ui.schematicTool === 'wire'} onClick={() => { ui.setSchematicTool('wire'); wireStart.current = null; setInfo('Outil Fil : cliquez une borne de départ') }} title="Fil" icon={<Spline className="h-4 w-4" />} />
        </div>
        {info && (
          <div className="absolute bottom-3 left-3 rounded bg-zinc-900/85 px-3 py-1.5 text-xs text-emerald-300">{info}</div>
        )}
      </div>

      {/* Panneau */}
      <aside className="flex w-80 shrink-0 flex-col border-l border-zinc-800 bg-zinc-900">
        <ScrollArea className="flex-1">
          <div className="space-y-4 p-4">
            <section>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">Ajouter un composant</h3>
              <div className="grid grid-cols-2 gap-1.5">
                {PALETTE.map((p) => (
                  <button
                    key={p.label}
                    onClick={() => {
                      pendingAdd.current = { kind: p.kind, pins: p.pins }
                      ui.setSchematicTool('add')
                      setInfo(`Cliquez sur la feuille pour placer « ${p.label} »`)
                    }}
                    className="rounded border border-zinc-800 px-2 py-1.5 text-left text-xs text-zinc-300 hover:border-emerald-700 hover:bg-emerald-950/30"
                  >
                    <Plus className="mr-1 inline h-3 w-3 text-emerald-500" />
                    {p.label}
                  </button>
                ))}
              </div>
            </section>

            <section className="rounded-md border border-zinc-800 bg-zinc-950 p-3">
              <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-zinc-500">
                <Zap className="h-3.5 w-3.5" /> Netlist
              </h3>
              <p className="mb-2 text-xs text-zinc-500">
                {design.nets.length} net(s) · {design.schematic.wires.length} fil(s) · {design.schematic.components.length} composant(s)
              </p>
              <div className="grid gap-1.5">
                <Button size="sm" variant="outline" className="justify-start gap-2" onClick={extractNets}>
                  <GitBranch className="h-3.5 w-3.5" /> Extraire la netlist des fils
                </Button>
                <Button size="sm" variant="outline" className="justify-start gap-2" onClick={pushToLayout}>
                  <Route className="h-3.5 w-3.5" /> Envoyer vers le PCB
                </Button>
                <Button size="sm" variant="outline" className="justify-start gap-2" onClick={() => { update((d) => { d.schematic.wires = [] }); setInfo('Fils effacés') }}>
                  <Eraser className="h-3.5 w-3.5" /> Effacer les fils
                </Button>
              </div>
            </section>

            {selected && (
              <section className="rounded-md border border-zinc-800 bg-zinc-950 p-3">
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">Composant sélectionné</h3>
                <div className="space-y-2 text-xs">
                  <div className="flex items-center gap-2">
                    <label className="w-16 text-zinc-500">Réf.</label>
                    <Input
                      className="h-7"
                      value={selected.ref}
                      onChange={(e) => update((d) => { const c = d.schematic.components.find((c) => c.id === selected.id); if (c) c.ref = e.target.value })}
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <label className="w-16 text-zinc-500">Valeur</label>
                    <Input
                      className="h-7"
                      value={selected.value}
                      onChange={(e) => update((d) => { const c = d.schematic.components.find((c) => c.id === selected.id); if (c) c.value = e.target.value })}
                    />
                  </div>
                  <div className="text-zinc-500">
                    Symbole {selected.symbol} · {selected.pins.length} bornes · empreinte {selected.footprintName}
                  </div>
                  <div className="flex gap-1.5 pt-1">
                    <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={() => update((d) => { const c = d.schematic.components.find((c) => c.id === selected.id); if (c) c.rotation = ((c.rotation + 90) % 360) as 0 | 90 | 180 | 270 })}>
                      <RotateCw className="h-3 w-3" /> 90°
                    </Button>
                    <Button size="sm" variant="outline" className="h-7 gap-1 text-xs text-red-400" onClick={() => { update((d) => { d.schematic.components = d.schematic.components.filter((c) => c.id !== selected.id) }); ui.setSelectedComponentId(null) }}>
                      <Trash2 className="h-3 w-3" /> Supprimer
                    </Button>
                  </div>
                </div>
              </section>
            )}
          </div>
        </ScrollArea>
      </aside>
    </div>
  )
}

/** Rendu SVG d'un symbole (coordonnées en mm). */
function Symbol({ comp, selected, onMouseDown }: {
  comp: SchematicComponent
  selected: boolean
  onMouseDown: (e: React.MouseEvent) => void
}) {
  const stroke = selected ? '#34d399' : '#a8b0bf'
  const sw = 0.3
  const isBox = comp.symbol === 'IC' || comp.symbol === 'J' || comp.symbol === 'Q'
  const rows = Math.max(2, Math.ceil(comp.pins.length / 2))
  const boxH = (rows - 1) * 2.2 + 4
  const boxW = comp.symbol === 'J' ? 8 : comp.symbol === 'Q' ? 8 : 14
  return (
    <g
      transform={`translate(${comp.x},${comp.y}) rotate(${comp.rotation})`}
      onMouseDown={onMouseDown}
      className="cursor-move"
    >
      {comp.symbol === 'R' && (
        <polyline
          points="0,-5 -2,-3.5 2,-1.5 -2,0.5 2,2.5 -1,4 0,5"
          fill="none" stroke={stroke} strokeWidth={sw}
        />
      )}
      {(comp.symbol === 'C' || comp.symbol === 'CP') && (
        <>
          <line x1={-2.4} y1={-2.6} x2={2.4} y2={-2.6} stroke={stroke} strokeWidth={sw * 1.3} />
          <line x1={-2.4} y1={-1.2} x2={2.4} y2={-1.2} stroke={stroke} strokeWidth={sw * 1.3} />
          {comp.symbol === 'CP' && (
            <path d="M -2.2,-1 Q 0,0.4 2.2,-1" fill="none" stroke={stroke} strokeWidth={sw} transform="translate(0,1.4)" />
          )}
          <line x1={0} y1={-3.5} x2={0} y2={-2.6} stroke={stroke} strokeWidth={sw} />
          <line x1={0} y1={-1.2} x2={0} y2={comp.symbol === 'CP' ? 0.4 : 3.5} stroke={stroke} strokeWidth={sw} />
        </>
      )}
      {(comp.symbol === 'LED' || comp.symbol === 'D') && (
        <>
          <polygon points="-2,-2 2,-2 0,0.8" fill="none" stroke={stroke} strokeWidth={sw} />
          <line x1={-2} y1={0.8} x2={2} y2={0.8} stroke={stroke} strokeWidth={sw * 1.3} />
          <line x1={0} y1={-4} x2={0} y2={-2} stroke={stroke} strokeWidth={sw} />
          <line x1={0} y1={0.8} x2={0} y2={4} stroke={stroke} strokeWidth={sw} />
        </>
      )}
      {isBox && (
        <rect
          x={-boxW / 2}
          y={-boxH / 2}
          width={boxW}
          height={boxH}
          rx={0.6}
          fill="#141720" stroke={stroke} strokeWidth={sw}
        />
      )}
      {comp.symbol === 'Y' && (
        <>
          <rect x={-1.6} y={-2.4} width={3.2} height={4.8} fill="none" stroke={stroke} strokeWidth={sw} />
          <line x1={0} y1={-3.5} x2={0} y2={-2.4} stroke={stroke} strokeWidth={sw} />
          <line x1={0} y1={2.4} x2={0} y2={3.5} stroke={stroke} strokeWidth={sw} />
        </>
      )}
      {/* Bornes */}
      {comp.pins.map((pin) => {
        const px = pin.x
        const py = pin.y
        const len = Math.min(2, Math.hypot(pin.x, pin.y) / 2 + 0.4)
        const dirX = pin.x === 0 ? 0 : pin.x > 0 ? -1 : 1
        const dirY = pin.x === 0 && pin.y !== 0 ? (pin.y > 0 ? -1 : 1) : 0
        return (
          <g key={pin.id}>
            <line x1={px} y1={py} x2={px + dirX * len} y2={py + dirY * len} stroke={stroke} strokeWidth={sw * 0.8} />
            <circle cx={px} cy={py} r={0.42} fill={selected ? '#34d399' : '#64748b'} />
            <title>{`${comp.ref} borne ${pin.id}`}</title>
          </g>
        )
      })}
      <text
        x={0}
        y={-boxH / 2 - 1.6}
        textAnchor="middle"
        fontSize={2.2}
        fill={selected ? '#34d399' : '#8b93a3'}
        style={{ userSelect: 'none' }}
      >
        {comp.ref} · {comp.value}
      </text>
    </g>
  )
}

function STool({ active, onClick, title, icon }: { active: boolean; onClick: () => void; title: string; icon: React.ReactNode }) {
  return (
    <button
      title={title}
      onClick={onClick}
      className={`flex h-7 w-7 items-center justify-center rounded transition-colors ${
        active ? 'bg-emerald-600/25 text-emerald-400' : 'text-zinc-500 hover:bg-zinc-800'
      }`}
    >
      {icon}
    </button>
  )
}
