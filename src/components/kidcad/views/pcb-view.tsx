'use client'

/**
 * views/pcb-view — Éditeur de layout PCB (canvas 2D) :
 * couches, empreintes, pistes, vias, ratsnest, lancement des jobs IA
 * (placement / routage / optimisation) avec journal temps réel.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Crosshair, Layers, Loader2, Magnet, RotateCw, Route, Sparkles, SquareStack, Trash, Wand2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Design, FootprintInst } from '@/lib/kidcad/shared/types'
import { useProjectStore } from '@/lib/kidcad/client/stores/project-store'
import { useUiStore } from '@/lib/kidcad/client/stores/ui-store'
import { useAiStore } from '@/lib/kidcad/client/stores/ai-store'

const COL = {
  bg: '#101013',
  grid: '#1c1d22',
  edge: '#c8c3a6',
  body: '#3c4658',
  bodySel: '#7c93b8',
  padF: '#d4b24c',
  padB: '#b08d3a',
  trackF: '#c14d76',
  trackB: '#4da6a0',
  via: '#d4b24c',
  ratsnest: '#9aa4b2',
  padText: '#0f1013',
}

export default function PcbView() {
  const { design, update } = useProjectStore()
  const view = useUiStore()
  const ai = useAiStore()
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const wrapRef = useRef<HTMLDivElement>(null)
  const [cam, setCam] = useState({ x: -10, y: -10, zoom: 10 }) // zoom px/mm
  const dragRef = useRef<{ kind: 'pan' | 'fp'; fpId?: string; startX: number; startY: number; camX: number; camY: number } | null>(null)
  const [, setTick] = useState(0)

  // ------------------------------------------------------------------
  // Rendu canvas
  // ------------------------------------------------------------------
  const render = useCallback(() => {
    const canvas = canvasRef.current
    const wrap = wrapRef.current
    if (!canvas || !wrap || !design) return
    const dpr = window.devicePixelRatio || 1
    const w = wrap.clientWidth
    const h = wrap.clientHeight
    if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
      canvas.width = w * dpr
      canvas.height = h * dpr
      canvas.style.width = `${w}px`
      canvas.style.height = `${h}px`
    }
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.fillStyle = COL.bg
    ctx.fillRect(0, 0, w, h)

    const toPx = (x: number, y: number): [number, number] => [
      (x - cam.x) * cam.zoom,
      (y - cam.y) * cam.zoom,
    ]

    // Grille
    const step = design.rules.gridStep * 4
    if (cam.zoom * step > 6) {
      ctx.strokeStyle = COL.grid
      ctx.lineWidth = 1
      ctx.beginPath()
      for (let gx = 0; gx <= design.board.width + 1e-9; gx += step) {
        const [px] = toPx(gx, 0)
        ctx.moveTo(px, 0)
        ctx.lineTo(px, h)
      }
      for (let gy = 0; gy <= design.board.height + 1e-9; gy += step) {
        const [, py] = toPx(0, gy)
        ctx.moveTo(0, py)
        ctx.lineTo(w, py)
      }
      ctx.stroke()
    }

    // Carte
    const [bx, by] = toPx(0, 0)
    ctx.fillStyle = '#141419'
    ctx.fillRect(bx, by, design.board.width * cam.zoom, design.board.height * cam.zoom)
    ctx.strokeStyle = COL.edge
    ctx.lineWidth = 2
    ctx.strokeRect(bx, by, design.board.width * cam.zoom, design.board.height * cam.zoom)

    // Pistes
    const drawTracks = (layer: 'F.Cu' | 'B.Cu', visible: boolean, color: string) => {
      if (!visible) return
      ctx.strokeStyle = color
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      for (const t of design.layout.tracks) {
        if (t.layer !== layer) continue
        ctx.lineWidth = Math.max(1, t.width * cam.zoom)
        ctx.beginPath()
        t.pts.forEach((p, i) => {
          const [px, py] = toPx(p.x, p.y)
          if (i === 0) ctx.moveTo(px, py)
          else ctx.lineTo(px, py)
        })
        ctx.stroke()
      }
    }
    drawTracks('B.Cu', view.showBack, COL.trackB)
    drawTracks('F.Cu', view.showFront, COL.trackF)

    // Vias
    if (view.showFront || view.showBack) {
      for (const v of design.layout.vias) {
        const [px, py] = toPx(v.x, v.y)
        ctx.beginPath()
        ctx.arc(px, py, Math.max(2, (v.diameter / 2) * cam.zoom), 0, Math.PI * 2)
        ctx.fillStyle = COL.via
        ctx.fill()
        ctx.beginPath()
        ctx.arc(px, py, Math.max(1, (v.drill / 2) * cam.zoom), 0, Math.PI * 2)
        ctx.fillStyle = COL.bg
        ctx.fill()
      }
    }

    // Ratsnest (nets non routés)
    if (view.showRatsnest) {
      const routed = new Set(design.layout.tracks.map((t) => t.netId))
      const fpByComp = new Map(design.layout.footprints.map((f) => [f.componentId, f]))
      ctx.strokeStyle = COL.ratsnest
      ctx.setLineDash([4, 4])
      ctx.lineWidth = 1
      ctx.beginPath()
      for (const net of design.nets) {
        if (routed.has(net.id) || net.pins.length < 2) continue
        const pts: { x: number; y: number }[] = []
        for (const pin of net.pins) {
          const pad = fpByComp.get(pin.componentId)?.pads.find((p) => p.number === pin.pad)
          if (pad) pts.push({ x: pad.x, y: pad.y })
        }
        // Chaîne gloutonne (visuel)
        if (pts.length >= 2) {
          let cur = pts[0]
          const rest = pts.slice(1)
          while (rest.length) {
            let bi = 0
            let bd = Infinity
            rest.forEach((p, i) => {
              const d = Math.hypot(p.x - cur.x, p.y - cur.y)
              if (d < bd) { bd = d; bi = i }
            })
            const [ax, ay] = toPx(cur.x, cur.y)
            const [bx2, by2] = toPx(rest[bi].x, rest[bi].y)
            ctx.moveTo(ax, ay)
            ctx.lineTo(bx2, by2)
            cur = rest.splice(bi, 1)[0]
          }
        }
      }
      ctx.stroke()
      ctx.setLineDash([])
    }

    // Empreintes
    for (const fp of design.layout.footprints) {
      const selected = fp.id === view.selectedFootprintId
      const [fx, fy] = toPx(fp.x, fp.y)
      // corps
      ctx.save()
      ctx.translate(fx, fy)
      ctx.rotate((fp.rotation * Math.PI) / 180)
      ctx.fillStyle = selected ? COL.bodySel : COL.body
      ctx.globalAlpha = 0.85
      ctx.fillRect((-fp.bodyW / 2) * cam.zoom, (-fp.bodyH / 2) * cam.zoom, fp.bodyW * cam.zoom, fp.bodyH * cam.zoom)
      ctx.globalAlpha = 1
      ctx.strokeStyle = selected ? '#ffffff' : '#59637a'
      ctx.lineWidth = selected ? 2 : 1
      ctx.strokeRect((-fp.bodyW / 2) * cam.zoom, (-fp.bodyH / 2) * cam.zoom, fp.bodyW * cam.zoom, fp.bodyH * cam.zoom)
      ctx.restore()
      // pads
      for (const pad of fp.pads) {
        const [px, py] = toPx(pad.x, pad.y)
        const pw = pad.w * cam.zoom
        const ph = pad.h * cam.zoom
        ctx.fillStyle = pad.layer === 'F.Cu' ? (view.showFront ? COL.padF : COL.padF + '55') : view.showBack ? COL.padB : COL.padB + '55'
        if (pad.shape === 'circle') {
          ctx.beginPath()
          ctx.arc(px, py, Math.max(1.5, pw / 2), 0, Math.PI * 2)
          ctx.fill()
        } else {
          ctx.save()
          ctx.translate(px, py)
          ctx.rotate((fp.rotation * Math.PI) / 180)
          ctx.fillRect(-pw / 2, -ph / 2, pw, ph)
          ctx.restore()
        }
      }
      // repère pin 1 + référence
      ctx.fillStyle = COL.padText
      ctx.font = `${Math.min(11, Math.max(7, cam.zoom * 0.9))}px ui-sans-serif`
      ctx.textAlign = 'center'
      ctx.fillText(fp.ref, fx, fy + 3)
    }
  }, [cam, design, view])

  useEffect(() => {
    render()
    const onResize = () => setTick((t) => t + 1)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [render])

  // ------------------------------------------------------------------
  // Interactions
  // ------------------------------------------------------------------
  const toWorld = (e: React.MouseEvent): { x: number; y: number } => {
    const rect = canvasRef.current!.getBoundingClientRect()
    return {
      x: (e.clientX - rect.left) / cam.zoom + cam.x,
      y: (e.clientY - rect.top) / cam.zoom + cam.y,
    }
  }

  const hitFootprint = (p: { x: number; y: number }): FootprintInst | null => {
    if (!design) return null
    for (let i = design.layout.footprints.length - 1; i >= 0; i--) {
      const fp = design.layout.footprints[i]
      const r = Math.max(fp.bodyW, fp.bodyH) / 2 + 0.6
      if (Math.hypot(p.x - fp.x, p.y - fp.y) <= r) return fp
    }
    return null
  }

  const onMouseDown = (e: React.MouseEvent) => {
    if (!design) return
    const p = toWorld(e)
    const fp = hitFootprint(p)
    if (fp && e.button === 0) {
      view.setSelectedFootprintId(fp.id)
      dragRef.current = { kind: 'fp', fpId: fp.id, startX: p.x - fp.x, startY: p.y - fp.y, camX: 0, camY: 0 }
    } else {
      view.setSelectedFootprintId(null)
      dragRef.current = { kind: 'pan', startX: e.clientX, startY: e.clientY, camX: cam.x, camY: cam.y }
    }
  }

  const onMouseMove = (e: React.MouseEvent) => {
    const drag = dragRef.current
    if (!drag || !design) return
    if (drag.kind === 'pan') {
      setCam((c) => ({
        ...c,
        x: drag.camX - (e.clientX - drag.startX) / c.zoom,
        y: drag.camY - (e.clientY - drag.startY) / c.zoom,
      }))
    } else if (drag.fpId) {
      const p = toWorld(e)
      const nx = p.x - drag.startX
      const ny = p.y - drag.startY
      const grid = design.rules.gridStep
      update((d) => {
        const fp = d.layout.footprints.find((f) => f.id === drag.fpId)
        if (!fp) return
        const dx = Math.round(nx / grid) * grid - fp.x
        const dy = Math.round(ny / grid) * grid - fp.y
        fp.x += dx
        fp.y += dy
        for (const pad of fp.pads) {
          pad.x += dx
          pad.y += dy
        }
      })
    }
  }

  const onMouseUp = () => {
    dragRef.current = null
  }

  const onWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    const rect = canvasRef.current!.getBoundingClientRect()
    const mx = e.clientX - rect.left
    const my = e.clientY - rect.top
    const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15
    setCam((c) => {
      const zoom = Math.min(120, Math.max(1.5, c.zoom * factor))
      return { zoom, x: c.x + mx / c.zoom - mx / zoom, y: c.y + my / c.zoom - my / zoom }
    })
  }

  const rotateSelected = () => {
    const id = view.selectedFootprintId
    if (!id || !design) return
    update((d) => {
      const fp = d.layout.footprints.find((f) => f.id === id)
      if (!fp) return
      fp.rotation = (fp.rotation + 90) % 360
      for (const pad of fp.pads) {
        const rel = { x: pad.x - fp.x, y: pad.y - fp.y }
        const r = (90 * Math.PI) / 180
        pad.x = fp.x + rel.x * Math.cos(r) + rel.y * Math.sin(r)
        pad.y = fp.y - rel.x * Math.sin(r) + rel.y * Math.cos(r)
      }
    })
  }

  const deleteSelected = () => {
    const id = view.selectedFootprintId
    if (!id) return
    update((d) => {
      d.layout.footprints = d.layout.footprints.filter((f) => f.id !== id)
    })
    view.setSelectedFootprintId(null)
  }

  const stats = useMemo(() => {
    if (!design) return null
    const routed = new Set(design.layout.tracks.map((t) => t.netId))
    let len = 0
    for (const t of design.layout.tracks) {
      for (let i = 1; i < t.pts.length; i++) len += Math.hypot(t.pts[i].x - t.pts[i - 1].x, t.pts[i].y - t.pts[i - 1].y)
    }
    return {
      comp: design.layout.footprints.length,
      nets: design.nets.length,
      routed: Math.min(routed.size, design.nets.length),
      tracks: design.layout.tracks.length,
      vias: design.layout.vias.length,
      len,
    }
  }, [design])

  if (!design) return null
  const selected = design.layout.footprints.find((f) => f.id === view.selectedFootprintId)

  return (
    <div className="flex h-full">
      {/* Canvas */}
      <div ref={wrapRef} className="relative min-w-0 flex-1">
        <canvas
          ref={canvasRef}
          className="block h-full w-full cursor-crosshair"
          onMouseDown={onMouseDown}
          onMouseMove={onMouseMove}
          onMouseUp={onMouseUp}
          onMouseLeave={onMouseUp}
          onWheel={onWheel}
        />
        {/* Barre outils flottante */}
        <div className="absolute left-3 top-3 flex items-center gap-1 rounded-md border border-zinc-800 bg-zinc-900/90 p-1 shadow-lg">
          <ToolBtn active={view.showFront} onClick={() => view.toggleLayer('F.Cu')} title="Couche F.Cu" label="F.Cu" color={COL.trackF} />
          <ToolBtn active={view.showBack} onClick={() => view.toggleLayer('B.Cu')} title="Couche B.Cu" label="B.Cu" color={COL.trackB} />
          <div className="mx-1 h-5 w-px bg-zinc-800" />
          <ToolBtn active={view.showRatsnest} onClick={view.toggleRatsnest} title="Ratsnest" icon={<Crosshair className="h-4 w-4" />} />
          <ToolBtn active={false} onClick={() => setCam({ x: -10, y: -10, zoom: 10 })} title="Recadrer" icon={<Magnet className="h-4 w-4" />} />
        </div>
        <div className="absolute bottom-3 left-3 rounded bg-zinc-900/80 px-2 py-1 text-[10px] text-zinc-500">
          molette : zoom · glisser : déplacer la vue · cliquer une empreinte : sélectionner
        </div>
      </div>

      {/* Panneau droit */}
      <aside className="flex w-80 shrink-0 flex-col border-l border-zinc-800 bg-zinc-900">
        <ScrollArea className="flex-1">
          <div className="space-y-4 p-4">
            {/* Actions IA */}
            <section>
              <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-zinc-500">
                <Sparkles className="h-3.5 w-3.5 text-amber-400" /> Assistant IA
              </h3>
              <div className="grid gap-2">
                <Button
                  disabled={ai.running}
                  onClick={() => ai.run('place', { iterations: 3500 })}
                  className="justify-start gap-2 bg-amber-600/90 hover:bg-amber-500"
                >
                  {ai.running && ai.task === 'place' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Wand2 className="h-4 w-4" />}
                  Placement IA <span className="ml-auto text-[10px] opacity-70">recuit simulé</span>
                </Button>
                <Button
                  disabled={ai.running || design.layout.footprints.length === 0}
                  onClick={() => ai.run('route', { maxPasses: 3, allowRipup: true })}
                  className="justify-start gap-2 bg-emerald-600 hover:bg-emerald-500"
                >
                  {ai.running && ai.task === 'route' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Route className="h-4 w-4" />}
                  Routage IA <span className="ml-auto text-[10px] opacity-70">A* multi-couches</span>
                </Button>
                <Button
                  disabled={ai.running || design.layout.tracks.length === 0}
                  onClick={() => ai.run('optimize', { maxPasses: 2 })}
                  variant="outline"
                  className="justify-start gap-2"
                >
                  {ai.running && ai.task === 'optimize' ? <Loader2 className="h-4 w-4 animate-spin" /> : <SquareStack className="h-4 w-4" />}
                  Optimiser les vias
                </Button>
              </div>
              {ai.error && (
                <p className="mt-2 rounded border border-red-900 bg-red-950/40 px-2 py-1.5 text-xs text-red-300">{ai.error}</p>
              )}
            </section>

            {/* Statistiques */}
            {stats && (
              <section className="rounded-md border border-zinc-800 bg-zinc-950 p-3">
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">État du routage</h3>
                <div className="grid grid-cols-2 gap-y-1.5 text-xs">
                  <span className="text-zinc-500">Nets routés</span>
                  <span className={stats.routed === stats.nets ? 'text-emerald-400' : 'text-amber-400'}>
                    {stats.routed}/{stats.nets}
                  </span>
                  <span className="text-zinc-500">Pistes</span><span>{stats.tracks}</span>
                  <span className="text-zinc-500">Vias</span><span>{stats.vias}</span>
                  <span className="text-zinc-500">Cuivre</span><span>{(stats.len / 10).toFixed(1)} cm</span>
                  <span className="text-zinc-500">Composants</span><span>{stats.comp}</span>
                </div>
                <div className="mt-2 h-1.5 overflow-hidden rounded bg-zinc-800">
                  <div
                    className="h-full bg-emerald-500 transition-all"
                    style={{ width: `${stats.nets ? (stats.routed / stats.nets) * 100 : 0}%` }}
                  />
                </div>
              </section>
            )}

            {/* Journal IA */}
            {ai.logs.length > 0 && (
              <section>
                <div className="mb-1 flex items-center justify-between">
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-500">Journal IA</h3>
                  <button className="text-[10px] text-zinc-600 hover:text-zinc-400" onClick={ai.clearLogs}>effacer</button>
                </div>
                <div className="max-h-40 overflow-y-auto rounded border border-zinc-800 bg-zinc-950 p-2 font-mono text-[10px] leading-relaxed text-zinc-400">
                  {ai.logs.slice(-40).map((l, i) => (
                    <div key={i} className={l.includes('ÉCHEC') ? 'text-red-400' : l.startsWith('Terminé') ? 'text-emerald-400' : ''}>
                      {l}
                    </div>
                  ))}
                </div>
              </section>
            )}

            {/* Sélection */}
            <section>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">Sélection</h3>
              {selected ? (
                <div className="rounded-md border border-zinc-800 bg-zinc-950 p-3 text-xs">
                  <div className="mb-1 font-semibold text-zinc-200">{selected.ref} — {selected.value}</div>
                  <div className="text-zinc-500">{selected.footprintName} · {selected.pads.length} pads</div>
                  <div className="mt-1 text-zinc-500">
                    x={selected.x.toFixed(2)} · y={selected.y.toFixed(2)} · {selected.rotation}°
                  </div>
                  <div className="mt-2 flex gap-1.5">
                    <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={rotateSelected}>
                      <RotateCw className="h-3 w-3" /> 90°
                    </Button>
                    <Button size="sm" variant="outline" className="h-7 gap-1 text-xs text-red-400" onClick={deleteSelected}>
                      <Trash className="h-3 w-3" /> Retirer
                    </Button>
                  </div>
                </div>
              ) : (
                <p className="text-xs text-zinc-600">Cliquez sur une empreinte pour voir ses propriétés.</p>
              )}
            </section>

            {/* Règles */}
            <section className="rounded-md border border-zinc-800 bg-zinc-950 p-3">
              <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-zinc-500">
                <Layers className="h-3.5 w-3.5" /> Règles de conception
              </h3>
              <div className="grid grid-cols-2 gap-y-1 text-xs">
                <span className="text-zinc-500">Piste</span><span>{design.rules.trackWidth} mm</span>
                <span className="text-zinc-500">Isolement</span><span>{design.rules.minClearance} mm</span>
                <span className="text-zinc-500">Via</span><span>Ø {design.rules.viaDiameter} / {design.rules.viaDrill} mm</span>
                <span className="text-zinc-500">Grille</span><span>{design.rules.gridStep} mm</span>
              </div>
            </section>
          </div>
        </ScrollArea>
      </aside>
    </div>
  )
}

function ToolBtn({ active, onClick, title, label, color, icon }: {
  active: boolean
  onClick: () => void
  title: string
  label?: string
  color?: string
  icon?: React.ReactNode
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className={`flex h-7 items-center gap-1 rounded px-2 text-[10px] font-semibold transition-colors ${
        active ? 'bg-zinc-700/70 text-zinc-100' : 'text-zinc-500 hover:bg-zinc-800'
      }`}
    >
      {color && <span className="h-2 w-2 rounded-full" style={{ background: color }} />}
      {icon}
      {label}
    </button>
  )
}
