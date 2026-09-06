'use client'

/**
 * views/verify-view — Vérification DRC / ERC : rapports détaillés,
 * liste des violations par sévérité avec localisation.
 */
import { useState } from 'react'
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw, ShieldCheck, XCircle, Zap } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { DrcReport, ErcReport, Violation } from '@/lib/kidcad/shared/types'
import { useProjectStore } from '@/lib/kidcad/client/stores/project-store'

export default function VerifyView() {
  const { projectId, design } = useProjectStore()
  const [drc, setDrc] = useState<DrcReport | null>(null)
  const [erc, setErc] = useState<ErcReport | null>(null)
  const [busy, setBusy] = useState<'drc' | 'erc' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const runDrc = async () => {
    if (!projectId) return
    setBusy('drc')
    setError(null)
    try {
      const api = (await import('@/lib/kidcad/client/api')).api
      const { report } = await api.drc(projectId)
      setDrc(report)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Erreur DRC')
    } finally {
      setBusy(null)
    }
  }

  const runErc = async () => {
    if (!projectId) return
    setBusy('erc')
    setError(null)
    try {
      const api = (await import('@/lib/kidcad/client/api')).api
      const { report } = await api.erc(projectId)
      setErc(report)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Erreur ERC')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl space-y-6">
        <div>
          <h1 className="text-xl font-bold">Vérification du design</h1>
          <p className="text-sm text-zinc-500">
            Design Rule Check (isolements, largeurs, vias, bord) et Electrical Rule Check (schéma, netlist).
          </p>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          {/* DRC */}
          <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="flex items-center gap-2 font-semibold">
                <ShieldCheck className="h-4 w-4 text-emerald-500" /> DRC — Fabrication
              </h2>
              <Button size="sm" variant="outline" onClick={runDrc} disabled={busy !== null} className="gap-1.5">
                {busy === 'drc' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                Lancer
              </Button>
            </div>
            {drc ? <ReportSummary ok={drc.ok} count={drc.violations.length} label="violations" /> : (
              <p className="text-xs text-zinc-600">Vérifie largeur de pistes, isolements, vias et distance au bord sur le layout actuel.</p>
            )}
            {drc && <ViolationList violations={drc.violations} />}
          </div>

          {/* ERC */}
          <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="flex items-center gap-2 font-semibold">
                <Zap className="h-4 w-4 text-amber-400" /> ERC — Électrique
              </h2>
              <Button size="sm" variant="outline" onClick={runErc} disabled={busy !== null} className="gap-1.5">
                {busy === 'erc' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                Lancer
              </Button>
            </div>
            {erc ? <ReportSummary ok={erc.ok} count={erc.violations.length} label={`violations · ${erc.netCount} nets`} /> : (
              <p className="text-xs text-zinc-600">Vérifie bornes non connectées, nets à borne unique et cohérence schéma/layout.</p>
            )}
            {erc && <ViolationList violations={erc.violations} />}
          </div>
        </div>

        {error && (
          <div className="rounded-md border border-red-900 bg-red-950/40 px-4 py-3 text-sm text-red-300">{error}</div>
        )}

        {design && (
          <p className="text-xs text-zinc-600">
            Règles actives : piste ≥ {design.rules.minTrackWidth} mm · isolement ≥ {design.rules.minClearance} mm ·
            via Ø{design.rules.minViaDiameter}/{design.rules.minViaDrill} mm · bord ≥ {design.rules.edgeClearance} mm
          </p>
        )}
      </div>
    </div>
  )
}

function ReportSummary({ ok, count, label }: { ok: boolean; count: number; label: string }) {
  return (
    <div className="flex items-center gap-2 text-sm">
      {ok ? (
        <CheckCircle2 className="h-5 w-5 text-emerald-500" />
      ) : (
        <XCircle className="h-5 w-5 text-red-500" />
      )}
      <span className={ok ? 'text-emerald-400' : 'text-red-400'}>
        {ok ? 'Aucune erreur bloquante' : `${count} violation(s)`}
      </span>
      <span className="text-xs text-zinc-600">{label}</span>
    </div>
  )
}

function ViolationList({ violations }: { violations: Violation[] }) {
  if (violations.length === 0) {
    return <p className="mt-3 rounded bg-zinc-950 px-3 py-2 text-xs text-zinc-500">Aucune violation détectée.</p>
  }
  return (
    <ScrollArea className="mt-3 max-h-56">
      <div className="space-y-1.5 pr-2">
        {violations.map((v, i) => (
          <div key={i} className="flex items-start gap-2 rounded bg-zinc-950 px-3 py-2 text-xs">
            {v.severity === 'error' ? (
              <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-red-500" />
            ) : (
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-400" />
            )}
            <div className="min-w-0">
              <div className="text-zinc-300">{v.message}</div>
              <div className="mt-0.5 flex items-center gap-2 text-[10px] text-zinc-600">
                <Badge variant="outline" className="h-4 px-1 text-[9px]">{v.type}</Badge>
                x={v.at.x.toFixed(1)} · y={v.at.y.toFixed(1)} mm
              </div>
            </div>
          </div>
        ))}
      </div>
    </ScrollArea>
  )
}
