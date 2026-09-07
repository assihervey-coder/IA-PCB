/**
 * application/export/step — Export STEP AP214 simplifié (boîtes) :
 * carte FR4 + corps de composants comme MANIFOLD_SOLID_BREP.
 * Pour un STEP riche (pastilles, perçages), le STL est fourni en complément.
 */
import { Design } from '../../shared/types'

class StepWriter {
  private id = 0
  private lines: string[] = []

  add(body: string): number {
    const id = ++this.id
    this.lines.push(`#${id}= ${body};`)
    return id
  }

  content(header: string): string {
    return [
      'ISO-10303-21;',
      'HEADER;',
      `FILE_DESCRIPTION(('YahriaCad export'),'2;1');`,
      `FILE_NAME('${header}.stp','${new Date().toISOString()}',('YahriaCad'),('YahriaCad'),'YahriaCad STEP writer','YahriaCad','');`,
      "FILE_SCHEMA(('AUTOMOTIVE_DESIGN { 1 0 10303 214 1 1 1 1 }'));",
      'ENDSEC;',
      'DATA;',
      ...this.lines,
      'ENDSEC;',
      'END-ISO-10303-21;',
    ].join('\n')
  }
}

interface Box {
  cx: number
  cy: number
  cz: number
  w: number
  h: number
  d: number
  name: string
}

/** Génère un STEP AP214 : une boîte (MANIFOLD_SOLID_BREP) par produit. */
export function generateStep(design: Design): string {
  const w = new StepWriter()
  const BOARD_T = 1.6

  const boxes: Box[] = [
    {
      cx: design.board.width / 2,
      cy: design.board.height / 2,
      cz: BOARD_T / 2,
      w: design.board.width,
      h: design.board.height,
      d: BOARD_T,
      name: 'BOARD_FR4',
    },
  ]
  for (const fp of design.layout.footprints) {
    const bodyH = Math.max(1.2, Math.min(4, Math.max(fp.bodyW, fp.bodyH) * 0.45))
    boxes.push({
      cx: fp.x,
      cy: fp.y,
      cz: BOARD_T + bodyH / 2,
      w: fp.bodyW,
      h: fp.bodyH,
      d: bodyH,
      name: fp.ref.replace(/[^\w-]/g, '_') || 'COMP',
    })
  }

  for (const box of boxes) {
    writeBox(w, box)
  }

  return w.content(design.name.replace(/[^\w-]/g, '_') || 'yahriacad')
}

function writeBox(w: StepWriter, box: Box): void {
  const x0 = box.cx - box.w / 2, x1 = box.cx + box.w / 2
  const y0 = box.cy - box.h / 2, y1 = box.cy + box.h / 2
  const z0 = box.cz - box.d / 2, z1 = box.cz + box.d / 2

  // 8 sommets — index [z][y*2+x]
  const vIds: number[][] = []
  for (const z of [z0, z1]) {
    const layer: number[] = []
    for (const y of [y0, y1]) {
      for (const x of [x0, x1]) {
        layer.push(w.add(`CARTESIAN_POINT('',(${x},${y},${z}))`))
      }
    }
    vIds.push(layer)
  }
  const V = (xi: 0 | 1, yi: 0 | 1, zi: 0 | 1) => vIds[zi][yi * 2 + xi]

  const dx = w.add(`DIRECTION('',(1,0,0))`)
  const dy = w.add(`DIRECTION('',(0,1,0))`)
  const dz = w.add(`DIRECTION('',(0,0,1))`)
  const nx = w.add(`DIRECTION('',(-1,0,0))`)
  const ny = w.add(`DIRECTION('',(0,-1,0))`)
  const nz = w.add(`DIRECTION('',(0,0,-1))`)

  const faces: number[] = []
  const face = (
    pts: number[], // 4 sommets, anti-horaire vu de l'extérieur
    originId: number,
    refDir: number,
    normal: number,
  ) => {
    const placement = w.add(`AXIS2_PLACEMENT_3D('',#${originId},#${normal},#${refDir})`)
    const plane = w.add(`PLANE('',#${placement})`)
    const vector = w.add(`VECTOR('',#${refDir},1)`)
    const edges: number[] = []
    for (let i = 0; i < 4; i++) {
      const a = pts[i]
      const b = pts[(i + 1) % 4]
      const vpA = w.add(`VERTEX_POINT('',#${a})`)
      const vpB = w.add(`VERTEX_POINT('',#${b})`)
      const edgeCurve = w.add(`EDGE_CURVE('',#${vpA},#${vpB},#${vector},.T.)`)
      edges.push(edgeCurve)
    }
    const oriented = edges.map((e) => w.add(`ORIENTED_EDGE('',*,*,#${e},.T.)`))
    const loop = w.add(`EDGE_LOOP('',(${oriented.map((o) => `#${o}`).join(',')}))`)
    const fb = w.add(`FACE_OUTER_BOUND('',#${loop},.T.)`)
    const advFace = w.add(`ADVANCED_FACE('',(#${fb}),#${plane},.T.)`)
    faces.push(advFace)
  }

  face([V(0, 0, 1), V(1, 0, 1), V(1, 1, 1), V(0, 1, 1)], V(0, 0, 1), dx, dz) // dessus
  face([V(0, 1, 0), V(1, 1, 0), V(1, 0, 0), V(0, 0, 0)], V(0, 1, 0), dx, nz) // dessous
  face([V(0, 0, 0), V(1, 0, 0), V(1, 0, 1), V(0, 0, 1)], V(0, 0, 0), dx, ny) // avant
  face([V(1, 1, 0), V(0, 1, 0), V(0, 1, 1), V(1, 1, 1)], V(1, 1, 0), nx, ny) // arrière
  face([V(1, 0, 0), V(1, 1, 0), V(1, 1, 1), V(1, 0, 1)], V(1, 0, 0), dy, dx) // droite
  face([V(0, 1, 0), V(0, 0, 0), V(0, 0, 1), V(0, 1, 1)], V(0, 1, 0), ny, nx) // gauche

  const shell = w.add(`CLOSED_SHELL('',(${faces.map((f) => `#${f}`).join(',')}))`)
  w.add(`MANIFOLD_SOLID_BREP('corps_${box.name}',#${shell})`)
}
