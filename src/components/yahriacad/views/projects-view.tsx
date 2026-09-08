'use client'

/**
 * views/projects-view — Gestionnaire de projets : liste, création (vide ou
 * projet démo "Kit LED Chaser"), ouverture, suppression.
 */
import { useEffect, useState } from 'react'
import {
  CircuitBoard,
  Clock,
  FilePlus2,
  Layers,
  Loader2,
  Sparkles,
  Trash2,
  Waypoints,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle,
} from '@/components/ui/card'
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useProjectStore } from '@/lib/yahriacad/client/stores/project-store'
import { useUiStore } from '@/lib/yahriacad/client/stores/ui-store'

export default function ProjectsView() {
  const { projects, loading, error, fetchProjects, createProject, openProject, deleteProject } = useProjectStore()
  const setView = useUiStore((s) => s.setView)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [template, setTemplate] = useState<'empty' | 'led-chaser'>('empty')
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    fetchProjects().catch(() => {})
  }, [fetchProjects])

  const onCreate = async () => {
    if (!name.trim()) return
    setCreating(true)
    const id = await createProject(name.trim(), desc.trim(), template)
    setCreating(false)
    if (id) {
      setOpen(false)
      setName('')
      setDesc('')
      await openProject(id)
      setView('pcb')
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto max-w-5xl">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-bold">Projets PCB</h1>
            <p className="text-sm text-zinc-500">
              Créez un projet, importez une netlist KiCad, laissez l&apos;IA placer et router.
            </p>
          </div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button className="gap-2 bg-emerald-600 hover:bg-emerald-500">
                <FilePlus2 className="h-4 w-4" /> Nouveau projet
              </Button>
            </DialogTrigger>
            <DialogContent className="border-zinc-800 bg-zinc-900">
              <DialogHeader>
                <DialogTitle>Nouveau projet</DialogTitle>
                <DialogDescription>Choisissez un nom et un point de départ.</DialogDescription>
              </DialogHeader>
              <div className="grid gap-4 py-2">
                <div className="grid gap-2">
                  <Label htmlFor="pname">Nom du projet</Label>
                  <Input id="pname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Ex. CapteurTemp-v1" />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="pdesc">Description</Label>
                  <Input id="pdesc" value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Optionnel" />
                </div>
                <div className="grid gap-2">
                  <Label>Modèle de départ</Label>
                  <div className="grid grid-cols-2 gap-2">
                    <button
                      onClick={() => setTemplate('empty')}
                      className={`rounded-md border p-3 text-left text-sm ${template === 'empty' ? 'border-emerald-500 bg-emerald-950/40' : 'border-zinc-800 hover:border-zinc-700'}`}
                    >
                      <div className="font-medium">Projet vide</div>
                      <div className="text-xs text-zinc-500">Carte 80×50 mm, 2 couches</div>
                    </button>
                    <button
                      onClick={() => setTemplate('led-chaser')}
                      className={`rounded-md border p-3 text-left text-sm ${template === 'led-chaser' ? 'border-emerald-500 bg-emerald-950/40' : 'border-zinc-800 hover:border-zinc-700'}`}
                    >
                      <div className="flex items-center gap-1.5 font-medium"><Sparkles className="h-3.5 w-3.5 text-amber-400" /> Kit LED Chaser</div>
                      <div className="text-xs text-zinc-500">27 composants · 26 nets · démo IA</div>
                    </button>
                  </div>
                </div>
              </div>
              <DialogFooter>
                <Button variant="ghost" onClick={() => setOpen(false)}>Annuler</Button>
                <Button onClick={onCreate} disabled={!name.trim() || creating} className="bg-emerald-600 hover:bg-emerald-500">
                  {creating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                  Créer et ouvrir
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>

        {error && (
          <div className="mb-4 rounded-md border border-red-900 bg-red-950/40 px-4 py-3 text-sm text-red-300">{error}</div>
        )}

        {loading ? (
          <div className="flex h-40 items-center justify-center text-zinc-500">
            <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Chargement…
          </div>
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-zinc-800 py-16 text-center">
            <CircuitBoard className="mb-3 h-10 w-10 text-zinc-700" />
            <p className="text-sm text-zinc-400">Aucun projet pour l&apos;instant.</p>
            <p className="mb-4 text-xs text-zinc-600">Commencez par le projet démo pour voir l&apos;IA en action.</p>
            <Button
              variant="outline"
              className="gap-2"
              onClick={() => { setName('Kit LED Chaser 10 voies'); setDesc('Démo NE555 + CD4017'); setTemplate('led-chaser'); setOpen(true) }}
            >
              <Sparkles className="h-4 w-4 text-amber-400" /> Charger la démo
            </Button>
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {projects.map((p) => (
              <Card key={p.id} className="group border-zinc-800 bg-zinc-900 transition-colors hover:border-emerald-800">
                <CardHeader className="pb-2">
                  <CardTitle className="flex items-center justify-between text-base">
                    <span className="truncate">{p.name}</span>
                    {p.template === 'led-chaser' && <Sparkles className="h-4 w-4 shrink-0 text-amber-400" />}
                  </CardTitle>
                  <CardDescription className="line-clamp-2 min-h-8 text-xs">{p.description || '—'}</CardDescription>
                </CardHeader>
                <CardContent className="grid grid-cols-3 gap-2 pb-2 text-center text-xs text-zinc-400">
                  <div className="rounded bg-zinc-950 py-1.5">
                    <Layers className="mx-auto mb-1 h-3.5 w-3.5 text-emerald-500" />
                    {p.components} comp.
                  </div>
                  <div className="rounded bg-zinc-950 py-1.5">
                    <Waypoints className="mx-auto mb-1 h-3.5 w-3.5 text-emerald-500" />
                    {p.tracks} pistes
                  </div>
                  <div className="rounded bg-zinc-950 py-1.5">
                    <CircuitBoard className="mx-auto mb-1 h-3.5 w-3.5 text-emerald-500" />
                    {p.boardWidth}×{p.boardHeight}
                  </div>
                </CardContent>
                <CardFooter className="flex items-center justify-between pt-0">
                  <div className="flex items-center gap-1 text-[11px] text-zinc-600">
                    <Clock className="h-3 w-3" />
                    {new Date(p.updatedAt).toLocaleString('fr-FR', { dateStyle: 'short', timeStyle: 'short' })}
                  </div>
                  <div className="flex gap-1.5">
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={`Supprimer ${p.name}`}
                      onClick={() => { if (confirm(`Supprimer « ${p.name} » ?`)) deleteProject(p.id) }}
                      className="h-8 px-2 text-zinc-500 hover:text-red-400"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                    <Button
                      size="sm"
                      className="h-8 bg-emerald-600 hover:bg-emerald-500"
                      onClick={async () => { await openProject(p.id); setView('pcb') }}
                    >
                      Ouvrir
                    </Button>
                  </div>
                </CardFooter>
              </Card>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
