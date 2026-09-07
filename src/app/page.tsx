'use client'

import dynamic from 'next/dynamic'

/**
 * YahriaCad — application mono-page (la sandbox n'expose que la route /).
 * Toutes les vues (Projets, Schéma, PCB, 3D, Vérification, Export) vivent ici.
 * Rendu 100 % client (WebGL, canvas, socket.io).
 */
const AppShell = dynamic(() => import('@/components/yahriacad/app-shell'), {
  ssr: false,
  loading: () => (
    <div className="flex h-screen items-center justify-center bg-zinc-950 text-sm text-zinc-500">
      YahriaCad-Pro IA — chargement…
    </div>
  ),
})

export default function Home() {
  return <AppShell />
}
