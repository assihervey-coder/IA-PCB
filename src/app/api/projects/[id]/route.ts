/**
 * API REST — GET / PUT / DELETE /api/projects/{id}
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/kidcad/infrastructure/persistence/sql/project_repo'
import { createLogger } from '@/lib/kidcad/pkg/logger'

const log = createLogger('api:project')

type Ctx = { params: Promise<{ id: string }> }

export async function GET(_req: NextRequest, ctx: Ctx) {
  try {
    const { id } = await ctx.params
    const project = await projectRepo.get(id)
    if (!project) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    return NextResponse.json({ project })
  } catch (e) {
    log.error('Lecture échouée', e)
    return NextResponse.json({ error: 'Lecture impossible' }, { status: 500 })
  }
}

export async function PUT(req: NextRequest, ctx: Ctx) {
  try {
    const { id } = await ctx.params
    const body = (await req.json()) as { name?: string; description?: string; design?: unknown }
    const saved = await projectRepo.save(id, {
      name: body.name,
      description: body.description,
      design: body.design as never,
    })
    if (!saved) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    return NextResponse.json({ project: saved })
  } catch (e) {
    log.error('Sauvegarde échouée', e)
    return NextResponse.json({ error: 'Sauvegarde impossible' }, { status: 500 })
  }
}

export async function DELETE(_req: NextRequest, ctx: Ctx) {
  try {
    const { id } = await ctx.params
    const ok = await projectRepo.delete(id)
    if (!ok) return NextResponse.json({ error: 'Projet introuvable' }, { status: 404 })
    return NextResponse.json({ ok: true })
  } catch (e) {
    log.error('Suppression échouée', e)
    return NextResponse.json({ error: 'Suppression impossible' }, { status: 500 })
  }
}
