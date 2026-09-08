/**
 * API REST — GET /api/projects (liste), POST /api/projects (création).
 * infrastructure/api/rest/project_handler.go (équivalent route handler Next.js).
 */
import { NextRequest, NextResponse } from 'next/server'
import { projectRepo } from '@/lib/yahriacad/infrastructure/persistence/sql/project_repo'
import { createLogger } from '@/lib/yahriacad/pkg/logger'

const log = createLogger('api:projects')

export async function GET() {
  try {
    const projects = await projectRepo.list()
    return NextResponse.json({ projects })
  } catch (e) {
    log.error('Liste échouée', e)
    return NextResponse.json({ error: 'Impossible de lister les projets' }, { status: 500 })
  }
}

export async function POST(req: NextRequest) {
  try {
    const body = (await req.json().catch(() => ({}))) as {
      name?: string
      description?: string
      template?: string
    }
    const name = (body.name ?? '').trim()
    if (!name) {
      return NextResponse.json({ error: 'Le nom du projet est requis' }, { status: 400 })
    }
    const created = await projectRepo.create({
      name,
      description: body.description ?? '',
      template: body.template ?? 'empty',
    })
    return NextResponse.json({ project: created }, { status: 201 })
  } catch (e) {
    log.error('Création échouée', e)
    return NextResponse.json({ error: 'Création impossible' }, { status: 500 })
  }
}
