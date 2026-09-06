/**
 * API REST — POST /api/projects/{id}/drc : rapport Design Rule Check.
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/kidcad/infrastructure/persistence/sql/project_repo'
import { runDrc } from '@/lib/kidcad/application/verification/drc'
import { createLogger } from '@/lib/kidcad/pkg/logger'

const log = createLogger('api:drc')

export async function POST(_req: NextRequest, ctx: { params: Promise<{ id: string }> }) {
  try {
    const { id } = await ctx.params
    const project = await projectRepo.get(id)
    if (!project) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    const report = runDrc(project.design)
    return NextResponse.json({ report })
  } catch (e) {
    log.error('DRC échoué', e)
    return NextResponse.json({ error: 'DRC impossible' }, { status: 500 })
  }
}
