/**
 * application/export/stl — Export STL ASCII (carte + corps de composants).
 * Échange 3D robuste, visualisable partout.
 */
import { Design } from '../../shared/types'

interface Box {
  x: number; y: number; z: number // centre
  w: number; h: number; d: number
  name: string
}

function boxTriangles(b: Box): string {
  const x0 = b.x - b.w / 2, x1 = b.x + b.w / 2
  const y0 = b.y - b.h / 2, y1 = b.y + b.h / 2
  const z0 = b.z - b.d / 2, z1 = b.z + b.d / 2
  const v = [
    [x0, y0, z0], [x1, y0, z0], [x1, y1, z0], [x0, y1, z0],
    [x0, y0, z1], [x1, y0, z1], [x1, y1, z1], [x0, y1, z1],
  ]
  const quads = [
    [4, 5, 6, 7, 0, 0, 1], // top
    [1, 0, 3, 2, 0, 0, -1], // bottom
    [0, 1, 5, 4, 0, -1, 0], // front
    [2, 3, 7, 6, 0, 1, 0], // back
    [1, 2, 6, 5, 1, 0, 0], // right
    [3, 0, 4, 7, -1, 0, 0], // left
  ]
  const out: string[] = []
  for (const [a, b1, c, d, nx, ny, nz] of quads) {
    out.push(
      `  facet normal ${nx} ${ny} ${nz}`,
      `    outer loop`,
      `      vertex ${v[a][0]} ${v[a][1]} ${v[a][2]}`,
      `      vertex ${v[b1][0]} ${v[b1][1]} ${v[b1][2]}`,
      `      vertex ${v[c][0]} ${v[c][1]} ${v[c][2]}`,
      `    endloop`,
      `  endfacet`,
      `  facet normal ${nx} ${ny} ${nz}`,
      `    outer loop`,
      `      vertex ${v[a][0]} ${v[a][1]} ${v[a][2]}`,
      `      vertex ${v[c][0]} ${v[c][1]} ${v[c][2]}`,
      `      vertex ${v[d][0]} ${v[d][1]} ${v[d][2]}`,
      `    endloop`,
      `  endfacet`,
    )
  }
  return out.join('\n')
}

const BOARD_T = 1.6 // épaisseur standard FR4

export function generateStl(design: Design): string {
  const boxes: Box[] = [
    { x: design.board.width / 2, y: design.board.height / 2, z: BOARD_T / 2, w: design.board.width, h: design.board.height, d: BOARD_T, name: 'board' },
  ]
  for (const fp of design.layout.footprints) {
    const bodyH = Math.max(1.2, Math.min(4, Math.max(fp.bodyW, fp.bodyH) * 0.45))
    boxes.push({
      x: fp.x, y: fp.y, z: BOARD_T + bodyH / 2,
      w: fp.bodyW, h: fp.bodyH, d: bodyH, name: fp.ref,
    })
  }
  const parts: string[] = [`solid kidcad_${design.name.replace(/[^\w-]/g, '_')}`]
  for (const b of boxes) {
    parts.push(boxTriangles(b))
  }
  parts.push('endsolid kidcad')
  return parts.join('\n') + '\n'
}
