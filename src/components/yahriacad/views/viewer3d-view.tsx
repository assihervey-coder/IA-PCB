'use client'

/**
 * views/viewer3d-view — Visualisation 3D du PCB (Three.js) :
 * carte FR4, corps de composants, pads et pistes cuivre. OrbitControls.
 */
import { useEffect, useMemo, useRef } from 'react'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { Box, HelpCircle, Loader2 } from 'lucide-react'
import { useProjectStore } from '@/lib/yahriacad/client/stores/project-store'
import { useAiStore } from '@/lib/yahriacad/client/stores/ai-store'

const BOARD_T = 1.6

export default function Viewer3dView() {
  const mountRef = useRef<HTMLDivElement>(null)
  const { design } = useProjectStore()
  const running = useAiStore((s) => s.running)

  // Détection WebGL pendant le rendu (composant 100 % client)
  const hasWebGL = useMemo(() => {
    try {
      const c = document.createElement('canvas')
      return !!(window.WebGLRenderingContext && (c.getContext('webgl2') || c.getContext('webgl')))
    } catch {
      return false
    }
  }, [])

  useEffect(() => {
    const mount = mountRef.current
    if (!mount || !design || !hasWebGL) return

    const renderer = new THREE.WebGLRenderer({ antialias: true })
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1))
    renderer.setSize(mount.clientWidth, mount.clientHeight)
    mount.appendChild(renderer.domElement)

    const scene = new THREE.Scene()
    scene.background = new THREE.Color(0x0e0f12)

    const camera = new THREE.PerspectiveCamera(45, mount.clientWidth / mount.clientHeight, 0.1, 5000)
    const cx = design.board.width / 2
    const cy = design.board.height / 2
    camera.position.set(cx + design.board.width * 0.8, cy - design.board.height * 1.2, design.board.width * 1.1)
    camera.lookAt(cx, cy, 0)

    const controls = new OrbitControls(camera, renderer.domElement)
    controls.target.set(cx, cy, 0)
    controls.enableDamping = true

    scene.add(new THREE.AmbientLight(0xffffff, 0.55))
    const dir = new THREE.DirectionalLight(0xffffff, 1.1)
    dir.position.set(cx, cy - 80, 120)
    scene.add(dir)
    const dir2 = new THREE.DirectionalLight(0x88ffcc, 0.35)
    dir2.position.set(cx - 60, cy + 80, -60)
    scene.add(dir2)

    // Carte FR4
    const board = new THREE.Mesh(
      new THREE.BoxGeometry(design.board.width, design.board.height, BOARD_T),
      new THREE.MeshStandardMaterial({ color: 0x0d5c34, roughness: 0.55, metalness: 0.15 }),
    )
    board.position.set(cx, cy, BOARD_T / 2)
    scene.add(board)

    // Pads cuivre
    const padCount = design.layout.footprints.reduce((a, f) => a + f.pads.length, 0)
    if (padCount > 0) {
      const padMat = new THREE.MeshStandardMaterial({ color: 0xd4b24c, metalness: 0.9, roughness: 0.25 })
      const inst = new THREE.InstancedMesh(new THREE.BoxGeometry(1, 1, 0.04), padMat, padCount)
      const m = new THREE.Matrix4()
      let idx = 0
      for (const fp of design.layout.footprints) {
        for (const pad of fp.pads) {
          m.makeScale(pad.w, pad.h, 1)
          m.setPosition(pad.x, pad.y, BOARD_T + 0.02)
          inst.setMatrixAt(idx++, m)
        }
      }
      inst.count = idx
      scene.add(inst)
    }

    // Pistes cuivre (segments en boîtes)
    const segs = design.layout.tracks.flatMap((t) => {
      const out: { a: { x: number; y: number }; b: { x: number; y: number }; w: number; layer: string }[] = []
      for (let i = 1; i < t.pts.length; i++) out.push({ a: t.pts[i - 1], b: t.pts[i], w: t.width, layer: t.layer })
      return out
    }).slice(0, 1200)
    if (segs.length > 0) {
      const trackMat = new THREE.MeshStandardMaterial({ color: 0xc14d76, metalness: 0.8, roughness: 0.35 })
      const trackInst = new THREE.InstancedMesh(new THREE.BoxGeometry(1, 1, 1), trackMat, segs.length)
      const m = new THREE.Matrix4()
      let ti = 0
      for (const s of segs) {
        const len = Math.hypot(s.b.x - s.a.x, s.b.y - s.a.y)
        if (len < 0.01) continue
        const ang = Math.atan2(s.b.y - s.a.y, s.b.x - s.a.x)
        m.makeRotationZ(ang)
        m.scale(new THREE.Vector3(len, s.w, 0.03))
        m.setPosition((s.a.x + s.b.x) / 2, (s.a.y + s.b.y) / 2, s.layer === 'F.Cu' ? BOARD_T + 0.01 : -0.01)
        trackInst.setMatrixAt(ti++, m)
      }
      trackInst.count = ti
      scene.add(trackInst)
    }

    // Corps de composants
    for (const fp of design.layout.footprints) {
      const bodyH = Math.max(1.2, Math.min(4, Math.max(fp.bodyW, fp.bodyH) * 0.45))
      const box = new THREE.Mesh(
        new THREE.BoxGeometry(fp.bodyW, fp.bodyH, bodyH),
        new THREE.MeshStandardMaterial({ color: 0x2c3444, roughness: 0.6, metalness: 0.1 }),
      )
      box.position.set(fp.x, fp.y, BOARD_T + bodyH / 2)
      box.rotation.z = (fp.rotation * Math.PI) / 180
      scene.add(box)
    }

    // Sol
    const grid = new THREE.GridHelper(Math.max(design.board.width, design.board.height) * 4, 40, 0x23252c, 0x1a1c22)
    grid.position.set(cx, cy, -4)
    scene.add(grid)

    let raf = 0
    const animate = () => {
      controls.update()
      renderer.render(scene, camera)
      raf = requestAnimationFrame(animate)
    }
    animate()

    const onResize = () => {
      if (!mount) return
      camera.aspect = mount.clientWidth / mount.clientHeight
      camera.updateProjectionMatrix()
      renderer.setSize(mount.clientWidth, mount.clientHeight)
    }
    window.addEventListener('resize', onResize)

    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', onResize)
      controls.dispose()
      renderer.dispose()
      if (renderer.domElement.parentElement === mount) mount.removeChild(renderer.domElement)
    }
  }, [design, hasWebGL])

  if (!hasWebGL) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-sm text-zinc-500">
        <HelpCircle className="h-6 w-6" />
        WebGL indisponible dans ce navigateur — exportez le modèle en STL/STEP depuis l&apos;onglet Export.
      </div>
    )
  }

  return (
    <div className="relative h-full">
      <div ref={mountRef} className="h-full w-full" />
      <div className="absolute bottom-3 left-3 flex items-center gap-2 rounded bg-zinc-900/85 px-3 py-1.5 text-xs text-zinc-400">
        <Box className="h-3.5 w-3.5 text-emerald-500" />
        Glisser : orbiter · molette : zoom · clic droit : déplacer
        {running && <Loader2 className="ml-2 h-3.5 w-3.5 animate-spin text-emerald-400" />}
      </div>
    </div>
  )
}
