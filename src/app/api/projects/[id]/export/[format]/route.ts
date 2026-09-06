/**
 * API REST — GET /api/projects/{id}/export/{format}
 * format ∈ gerber (ZIP) | bom (CSV) | step | stl | json | netlist-kicad
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/kidcad/infrastructure/persistence/sql/project_repo'
import { generateGerberZip } from '@/lib/kidcad/application/export/gerber'
import { generateBom } from '@/lib/kidcad/application/export/bom'
import { generateStl } from '@/lib/kidcad/application/export/stl'
import { generateStep } from '@/lib/kidcad/application/export/step'
import { generateKicadNetlist } from '@/lib/kidcad/application/export/netlist'
import { syncNetlist } from '@/lib/kidcad/ai-engine'
import { createLogger } from '@/lib/kidcad/pkg/logger'

const log = createLogger('api:export')

const slug = (s: string) => s.normalize('NFKD').replace(/[^\w-]+/g, '_') || 'projet'

export async function GET(
  _req: NextRequest,
  ctx: { params: Promise<{ id: string; format: string }> },
) {
  try {
    const { id, format } = await ctx.params
    const project = await projectRepo.get(id)
    if (!project) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    const design = project.design
    syncNetlist(design)
    const base = slug(design.name)

    switch (format) {
      case 'gerber': {
        const bundle = await generateGerberZip(design)
        return new NextResponse(new Uint8Array(bundle.zip), {
          headers: {
            'Content-Type': 'application/zip',
            'Content-Disposition': `attachment; filename="${base}-gerber.zip"`,
          },
        })
      }
      case 'bom':
        return textFile(generateBom(design), 'text/csv', `${base}-bom.csv`)
      case 'step':
        return textFile(generateStep(design), 'application/step', `${base}.stp`)
      case 'stl':
        return textFile(generateStl(design), 'model/stl', `${base}.stl`)
      case 'json':
        return textFile(JSON.stringify(design, null, 2), 'application/json', `${base}.kidcad.json`)
      case 'netlist-kicad':
        return textFile(generateKicadNetlist(design), 'text/plain', `${base}-netlist.kicad.net`)
      default:
        return NextResponse.json(
          { error: `Format inconnu : ${format} (gerber | bom | step | stl | json | netlist-kicad)` },
          { status: 400 },
        )
    }
  } catch (e) {
    log.error('Export échoué', e)
    return NextResponse.json({ error: 'Export impossible' }, { status: 500 })
  }
}

function textFile(content: string, type: string, filename: string) {
  return new NextResponse(content, {
    headers: {
      'Content-Type': `${type}; charset=utf-8`,
      'Content-Disposition': `attachment; filename="${filename}"`,
    },
  })
}
