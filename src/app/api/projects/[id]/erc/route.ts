/**
 * API REST — POST /api/projects/{id}/erc : rapport Electrical Rule Check.
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/kidcad/infrastructure/persistence/sql/project_repo'
import { runErc } from '@/lib/kidcad/application/verification/erc'
import { createLogger } from '@/lib/kidcad/pkg/logger'

const log = createLogger('api:erc')

export async function POST(_req: NextRequest, ctx: { params: Promise<{ id: string }> }) {
  try {
    const { id } = await ctx.params
    const project = await projectRepo.get(id)
    if (!project) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    const report = runErc(project.design)
    return NextResponse.json({ report })
  } catch (e) {
    log.error('ERC échoué', e)
    return NextResponse.json({ error: 'ERC impossible' }, { status: 500 })
  }
}
