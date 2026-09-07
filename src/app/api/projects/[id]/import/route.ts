/**
 * API REST — POST /api/projects/{id}/import : import de netlist
 * { format: "kicad" | "json", content: string }
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/yahriacad/infrastructure/persistence/sql/project_repo'
import { applyNetlistImport, importKicadNetlist } from '@/lib/yahriacad/application/schematic/import'
import { createLogger } from '@/lib/yahriacad/pkg/logger'

const log = createLogger('api:import')

export async function POST(req: NextRequest, ctx: { params: Promise<{ id: string }> }) {
  try {
    const { id } = await ctx.params
    const project = await projectRepo.get(id)
    if (!project) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })

    const body = (await req.json()) as { format?: string; content?: string }
    if (!body.content) {
      return NextResponse.json({ error: 'Contenu de netlist requis' }, { status: 400 })
    }
    if (body.format !== 'kicad' && body.format !== 'json') {
      return NextResponse.json(
        { error: 'Format non supporté (kicad | json)' },
        { status: 400 },
      )
    }

    const design = project.design
    if (body.format === 'json') {
      const imported = JSON.parse(body.content) as typeof design
      design.nets = imported.nets ?? []
      design.schematic = imported.schematic ?? design.schematic
      if (imported.layout) design.layout = imported.layout
      if (imported.board) design.board = imported.board
      if (imported.rules) design.rules = imported.rules
      await projectRepo.save(id, { design })
      return NextResponse.json({ ok: true, nets: design.nets.length, warnings: [] })
    }

    const result = importKicadNetlist(body.content)
    applyNetlistImport(design, result)
    await projectRepo.save(id, { design })
    log.info(`Netlist importée : ${result.nets.length} nets, ${result.footprintsCreated} composants`)
    return NextResponse.json({
      ok: true,
      nets: result.nets.length,
      components: result.footprintsCreated,
      warnings: result.warnings,
    })
  } catch (e) {
    log.error('Import échoué', e)
    return NextResponse.json(
      { error: `Import impossible : ${e instanceof Error ? e.message : 'erreur inconnue'}` },
      { status: 400 },
    )
  }
}
