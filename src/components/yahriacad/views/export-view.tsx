'use client'

/**
 * views/export-view — Exports de fabrication : Gerber RS-274X (ZIP),
 * BOM CSV, STEP, STL, JSON natif, netlist KiCad + import de netlist.
 */
import { useRef, useState } from 'react'
import {
  Boxes, Download, FileCode2, FileJson2, FileSpreadsheet, Loader2, Upload, Waypoints,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useProjectStore } from '@/lib/yahriacad/client/stores/project-store'
import { useToast } from '@/hooks/use-toast'

export default function ExportView() {
  const { projectId, design } = useProjectStore()
  const { toast } = useToast()
  const [busy, setBusy] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  if (!design || !projectId) return null

  const download = async (format: string, label: string) => {
    setBusy(format)
    try {
      const res = await fetch(`/api/projects/${projectId}/export/${format}`)
      if (!res.ok) {
        const j = await res.json().catch(() => ({}))
        throw new Error(j.error ?? `HTTP ${res.status}`)
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${design.name.replace(/[^\w-]+/g, '_')}-${format === 'netlist-kicad' ? 'netlist.kicad.net' : format === 'gerber' ? 'gerber.zip' : ''}`.replace(/-$/, '')
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      toast({ title: 'Export réussi', description: `${label} téléchargé.` })
    } catch (e) {
      toast({ title: 'Échec de l’export', description: e instanceof Error ? e.message : 'Erreur', variant: 'destructive' })
    } finally {
      setBusy(null)
    }
  }

  const onImportFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const content = await file.text()
    const format = file.name.endsWith('.json') ? 'json' : 'kicad'
    setBusy('import')
    try {
      const api = (await import('@/lib/yahriacad/client/api')).api
      const r = await api.importNetlist(projectId, format, content)
      toast({
        title: 'Netlist importée',
        description: `${r.nets} nets, ${r.components ?? '?'} composants. Empreintes placées aléatoirement — relancez le placement IA.`,
      })
      // Recharge le projet pour refléter l'import
      await useProjectStore.getState().openProject(projectId)
    } catch (err) {
      toast({ title: 'Import impossible', description: err instanceof Error ? err.message : 'Erreur', variant: 'destructive' })
    } finally {
      setBusy(null)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  const EXPORTS = [
    { id: 'gerber', icon: Waypoints, title: 'Gerber RS-274X (ZIP)', desc: 'F.Cu/B.Cu, masques, serigraphies, contour + perçage Excellon — prêts pour le fabricant.' },
    { id: 'bom', icon: FileSpreadsheet, title: 'Nomenclature (BOM CSV)', desc: 'Groupée par valeur/empreinte avec quantités, compatible tableurs.' },
    { id: 'step', icon: Boxes, title: 'Modèle 3D STEP', desc: 'AP214 simplifié (boîtes) pour l\'intégration mécanique ; STL fourni en complément.' },
    { id: 'stl', icon: Boxes, title: 'Modèle 3D STL', desc: 'Carte + composants, visualisable dans n\'importe quelle visionneuse 3D.' },
    { id: 'json', icon: FileJson2, title: 'Projet natif JSON', desc: 'Sauvegarde complète (schéma, netlist, layout, règles) — réimportable.' },
    { id: 'netlist-kicad', icon: FileCode2, title: 'Netlist KiCad', desc: 's-expression compatible KiCad et avec l\'import de YahriaCad.' },
  ]

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl">
        <div className="mb-6">
          <h1 className="text-xl font-bold">Export &amp; Import</h1>
          <p className="text-sm text-zinc-500">
            Fichiers de fabrication et d&apos;échange pour « {design.name} ».
          </p>
        </div>

        <div className="mb-6 rounded-lg border border-dashed border-zinc-800 bg-zinc-900/50 p-4">
          <div className="flex flex-wrap items-center gap-3">
            <Upload className="h-5 w-5 text-amber-400" />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">Importer une netlist KiCad (.net) ou un projet JSON</div>
              <div className="text-xs text-zinc-500">Remplace la netlist et les composants du projet courant.</div>
            </div>
            <input ref={fileRef} type="file" accept=".net,.json,.txt" className="hidden" onChange={onImportFile} />
            <Button variant="outline" className="gap-2" onClick={() => fileRef.current?.click()} disabled={busy !== null}>
              {busy === 'import' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
              Choisir un fichier
            </Button>
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          {EXPORTS.map((exp) => (
            <Card key={exp.id} className="border-zinc-800 bg-zinc-900">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-base">
                  <exp.icon className="h-4 w-4 text-emerald-500" /> {exp.title}
                </CardTitle>
                <CardDescription className="text-xs">{exp.desc}</CardDescription>
              </CardHeader>
              <CardContent>
                <Button
                  size="sm"
                  className="w-full gap-2 bg-emerald-600 hover:bg-emerald-500"
                  onClick={() => download(exp.id, exp.title)}
                  disabled={busy !== null}
                >
                  {busy === exp.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                  Télécharger
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    </div>
  )
}
