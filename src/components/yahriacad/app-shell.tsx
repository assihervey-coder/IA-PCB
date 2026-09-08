'use client'

/**
 * app-shell — Coquille YahriaCad : en-tête, barre d'activités, vues.
 * Rendu au-dessus d'un fond sombre (style CAO), responsive.
 */
import { useEffect } from 'react'
import dynamic from 'next/dynamic'
import { useTheme } from 'next-themes'
import {
  CircuitBoard,
  Download,
  FolderOpen,
  GitBranch,
  Loader2,
  Moon,
  Save,
  ShieldCheck,
  Sun,
  Boxes,
  Wifi,
  WifiOff,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useProjectStore } from '@/lib/yahriacad/client/stores/project-store'
import { useUiStore, ViewId } from '@/lib/yahriacad/client/stores/ui-store'
import { useAiStore } from '@/lib/yahriacad/client/stores/ai-store'

const loadingView = () => (
  <div className="flex h-full w-full items-center justify-center text-muted-foreground">
    <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Chargement du module…
  </div>
)

const ProjectsView = dynamic(() => import('./views/projects-view'), { ssr: false, loading: loadingView })
const SchematicView = dynamic(() => import('./views/schematic-view'), { ssr: false, loading: loadingView })
const PcbView = dynamic(() => import('./views/pcb-view'), { ssr: false, loading: loadingView })
const Viewer3dView = dynamic(() => import('./views/viewer3d-view'), { ssr: false, loading: loadingView })
const VerifyView = dynamic(() => import('./views/verify-view'), { ssr: false, loading: loadingView })
const ExportView = dynamic(() => import('./views/export-view'), { ssr: false, loading: loadingView })

const VIEWS: { id: ViewId; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { id: 'projects', label: 'Projets', icon: FolderOpen },
  { id: 'schematic', label: 'Schéma', icon: GitBranch },
  { id: 'pcb', label: 'PCB', icon: CircuitBoard },
  { id: 'viewer3d', label: '3D', icon: Boxes },
  { id: 'verify', label: 'Vérif.', icon: ShieldCheck },
  { id: 'export', label: 'Export', icon: Download },
]

export default function AppShell() {
  const view = useUiStore((s) => s.view)
  const setView = useUiStore((s) => s.setView)
  const { design, projectId, dirty, saving, saveProject } = useProjectStore()
  const running = useAiStore((s) => s.running)
  const { resolvedTheme, setTheme } = useTheme()

  // Raccourci Ctrl/Cmd+S
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
        e.preventDefault()
        if (projectId && !saving) saveProject()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [projectId, saving, saveProject])

  const needProject = view !== 'projects'

  return (
    <div className="flex h-screen flex-col bg-zinc-950 text-zinc-100">
      {/* En-tête */}
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-zinc-800 bg-zinc-900 px-4">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-emerald-600 font-black text-white">K</div>
          <div className="leading-tight">
            <div className="text-sm font-bold tracking-wide">YahriaCad-Pro <span className="text-emerald-400">IA</span></div>
            <div className="text-[10px] text-zinc-500">CAO électronique · placement &amp; routage IA</div>
          </div>
        </div>
        <div className="mx-2 h-6 w-px bg-zinc-800" />
        <div className="min-w-0 flex-1 truncate text-sm text-zinc-300">
          {design ? design.name : <span className="text-zinc-500">Aucun projet ouvert</span>}
          {dirty && <span className="ml-2 text-amber-400">• modifié</span>}
        </div>
        <EngineStatus />
        {design && (
          <Button size="sm" variant="outline" onClick={() => saveProject()} disabled={saving || !dirty} className="gap-1.5">
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            Enregistrer
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          aria-label="Basculer le thème"
          onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
        >
          {resolvedTheme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </Button>
      </header>

      <div className="flex min-h-0 flex-1">
        {/* Barre d'activités */}
        <nav aria-label="Vues" className="flex w-14 shrink-0 flex-col items-center gap-1 border-r border-zinc-800 bg-zinc-900 py-3">
          <TooltipProvider delayDuration={200}>
            {VIEWS.map(({ id, label, icon: Icon }) => (
              <Tooltip key={id}>
                <TooltipTrigger asChild>
                  <button
                    aria-label={label}
                    onClick={() => setView(id)}
                    disabled={needProject && !design && id !== 'projects'}
                    className={`flex h-10 w-10 items-center justify-center rounded-md transition-colors disabled:opacity-30 ${
                      view === id ? 'bg-emerald-600/20 text-emerald-400' : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200'
                    }`}
                  >
                    <Icon className="h-5 w-5" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="right">{label}</TooltipContent>
              </Tooltip>
            ))}
          </TooltipProvider>
        </nav>

        {/* Contenu */}
        <main className="min-w-0 flex-1 overflow-hidden">
          {view === 'projects' && <ProjectsView />}
          {view === 'schematic' && design && <SchematicView />}
          {view === 'pcb' && design && <PcbView />}
          {view === 'viewer3d' && design && <Viewer3dView />}
          {view === 'verify' && design && <VerifyView />}
          {view === 'export' && design && <ExportView />}
          {needProject && !design && view !== 'projects' && (
            <div className="flex h-full items-center justify-center text-sm text-zinc-500">
              Ouvrez d&apos;abord un projet depuis l&apos;onglet Projets.
            </div>
          )}
        </main>
      </div>

      {/* Bandeau job IA */}
      <AiJobBar />
    </div>
  )
}

function AiJobBar() {
  const running = useAiStore((s) => s.running)
  const task = useAiStore((s) => s.task)
  const message = useAiStore((s) => s.message)
  const progress = useAiStore((s) => s.progress)
  if (!running) return null
  return (
    <footer className="flex h-9 shrink-0 items-center gap-3 border-t border-emerald-900 bg-emerald-950/60 px-4 text-xs text-emerald-300">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      <span className="font-medium uppercase">{task}</span>
      <span className="truncate text-emerald-200/80">{message}</span>
      <div className="ml-auto h-1.5 w-40 overflow-hidden rounded bg-emerald-900">
        <div className="h-full bg-emerald-500 transition-all" style={{ width: `${progress}%` }} />
      </div>
      <span className="w-10 text-right">{Math.round(progress)}%</span>
    </footer>
  )
}

function EngineStatus() {
  const running = useAiStore((s) => s.running)
  return (
    <div className="flex items-center gap-1.5 rounded-md border border-zinc-800 px-2 py-1 text-[11px] text-zinc-400">
      {running ? <Wifi className="h-3.5 w-3.5 animate-pulse text-emerald-400" /> : <WifiOff className="h-3.5 w-3.5" />}
      Moteur IA · 3010
    </div>
  )
}
