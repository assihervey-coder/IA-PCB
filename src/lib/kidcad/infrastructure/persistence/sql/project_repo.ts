/**
 * infrastructure/persistence/sql/project_repo — Adaptateur Prisma (SQLite)
 * du port ProjectRepository. Les designs sont sérialisés en JSON (format natif).
 */
import { db } from '@/lib/db'
import {
  CreateProjectInput,
  ProjectProps,
  ProjectRepository,
  ProjectSummary,
  UpdateProjectInput,
} from '@/lib/kidcad/domain/project/entity'
import { DEFAULT_RULES, Design, DesignRules } from '@/lib/kidcad/shared/types'
import { buildLedChaserDesign } from '@/lib/kidcad/library/samples/led-chaser'
import { createLogger } from '@/lib/kidcad/pkg/logger'

const log = createLogger('project-repo')

type ProjectRow = {
  id: string
  name: string
  description: string
  template: string
  boardWidth: number
  boardHeight: number
  layers: number
  schematicJson: string
  netsJson: string
  layoutJson: string
  rulesJson: string
  createdAt: Date
  updatedAt: Date
}

export function rowToDesign(row: ProjectRow): Design {
  let schematic: unknown = {}
  let layout: unknown = {}
  let nets: unknown = []
  let rules: unknown = {}
  try { schematic = JSON.parse(row.schematicJson) } catch { /* défaut */ }
  try { layout = JSON.parse(row.layoutJson) } catch { /* défaut */ }
  try { nets = JSON.parse(row.netsJson) } catch { /* défaut */ }
  try { rules = JSON.parse(row.rulesJson) } catch { /* défaut */ }
  const schematicData = schematic as Design['schematic']
  const layoutData = layout as Design['layout']
  return {
    id: row.id,
    name: row.name,
    description: row.description,
    board: { width: row.boardWidth, height: row.boardHeight, layers: 2 },
    rules: { ...DEFAULT_RULES, ...(rules as Partial<DesignRules>) },
    schematic: {
      components: schematicData.components ?? [],
      wires: schematicData.wires ?? [],
    },
    nets: Array.isArray(nets) ? (nets as Design['nets']) : [],
    layout: {
      footprints: layoutData.footprints ?? [],
      tracks: layoutData.tracks ?? [],
      vias: layoutData.vias ?? [],
    },
  }
}

export function designToRowData(design: Design) {
  return {
    name: design.name,
    description: design.description,
    boardWidth: design.board.width,
    boardHeight: design.board.height,
    layers: 2,
    schematicJson: JSON.stringify(design.schematic ?? { components: [], wires: [] }),
    netsJson: JSON.stringify(design.nets ?? []),
    layoutJson: JSON.stringify(
      design.layout ?? { footprints: [], tracks: [], vias: [] },
    ),
    rulesJson: JSON.stringify(design.rules ?? DEFAULT_RULES),
  }
}

export class PrismaProjectRepository implements ProjectRepository {
  async list(): Promise<ProjectSummary[]> {
    const rows = await db.project.findMany({ orderBy: { updatedAt: 'desc' } })
    return rows.map((row) => {
      const design = rowToDesign(row as ProjectRow)
      return {
        id: row.id,
        name: row.name,
        description: row.description,
        template: row.template,
        boardWidth: row.boardWidth,
        boardHeight: row.boardHeight,
        components: design.layout.footprints.length,
        tracks: design.layout.tracks.length,
        vias: design.layout.vias.length,
        updatedAt: row.updatedAt.toISOString(),
        createdAt: row.createdAt.toISOString(),
      }
    })
  }

  async get(id: string): Promise<ProjectProps | null> {
    const row = await db.project.findUnique({ where: { id } })
    if (!row) return null
    return {
      id: row.id,
      name: row.name,
      description: row.description,
      template: row.template,
      createdAt: row.createdAt,
      updatedAt: row.updatedAt,
      design: rowToDesign(row as ProjectRow),
    }
  }

  async create(input: CreateProjectInput): Promise<ProjectProps> {
    const template = input.template === 'led-chaser' ? 'led-chaser' : 'empty'
    let design: Design
    if (template === 'led-chaser') {
      design = buildLedChaserDesign()
      design.name = input.name
      design.description = input.description ?? design.description
    } else {
      design = {
        id: '',
        name: input.name,
        description: input.description ?? '',
        board: { width: 80, height: 50, layers: 2 },
        rules: { ...DEFAULT_RULES },
        schematic: { components: [], wires: [] },
        nets: [],
        layout: { footprints: [], tracks: [], vias: [] },
      }
    }
    const row = await db.project.create({
      data: { ...designToRowData(design), template },
    })
    log.info(`Projet créé : ${row.name} (${row.id})`)
    return {
      id: row.id,
      name: row.name,
      description: row.description,
      template: row.template,
      createdAt: row.createdAt,
      updatedAt: row.updatedAt,
      design: rowToDesign(row as ProjectRow),
    }
  }

  async save(id: string, patch: UpdateProjectInput): Promise<ProjectProps | null> {
    const existing = await db.project.findUnique({ where: { id } })
    if (!existing) return null
    const data: Record<string, unknown> = {}
    if (patch.name !== undefined) data.name = patch.name
    if (patch.description !== undefined) data.description = patch.description
    if (patch.design) {
      Object.assign(data, designToRowData(patch.design))
    }
    const row = await db.project.update({ where: { id }, data })
    return {
      id: row.id,
      name: row.name,
      description: row.description,
      template: row.template,
      createdAt: row.createdAt,
      updatedAt: row.updatedAt,
      design: rowToDesign(row as ProjectRow),
    }
  }

  async delete(id: string): Promise<boolean> {
    const existing = await db.project.findUnique({ where: { id } })
    if (!existing) return false
    await db.project.delete({ where: { id } })
    return true
  }
}

/** Instance partagée. */
export const projectRepo: ProjectRepository = new PrismaProjectRepository()
