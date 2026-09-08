/**
 * domain/project — Agrégat racine Project + port de persistance (repository).
 */
import { BoardSpec, Design, DesignRules, LayoutData, SchematicData } from '../../shared/types'

export interface ProjectProps {
  id: string
  name: string
  description: string
  template: string
  createdAt?: Date
  updatedAt?: Date
  design: Design
}

/** Entité Project — agrégat racine, garantit les invariants du design. */
export class ProjectEntity {
  private props: ProjectProps

  constructor(props: ProjectProps) {
    this.props = props
    this.ensureInvariants()
  }

  private ensureInvariants() {
    const d = this.props.design
    if (!d.board || d.board.width <= 0 || d.board.height <= 0) {
      d.board = { width: 80, height: 50, layers: 2 }
    }
    d.board.layers = 2
    if (!d.schematic) d.schematic = { components: [], wires: [] }
    if (!d.layout) d.layout = { footprints: [], tracks: [], vias: [] }
    if (!d.rules) d.rules = {} as DesignRules
  }

  get props_(): ProjectProps {
    return this.props
  }

  get design(): Design {
    return this.props.design
  }

  rename(name: string) {
    this.props.name = name.trim() || 'Sans nom'
  }

  setBoard(spec: Partial<BoardSpec>) {
    this.props.design.board = { ...this.props.design.board, ...spec, layers: 2 }
  }

  static empty(name: string, description = ''): ProjectEntity {
    return new ProjectEntity({
      id: '',
      name,
      description,
      template: 'empty',
      design: {
        id: '',
        name,
        description,
        board: { width: 80, height: 50, layers: 2 },
        rules: {} as DesignRules,
        schematic: { components: [], wires: [] },
        layout: { footprints: [], tracks: [], vias: [] },
      },
    })
  }
}

/** Port de persistance (infrastructure/sql implémente avec Prisma). */
export interface ProjectRepository {
  list(): Promise<ProjectSummary[]>
  get(id: string): Promise<ProjectProps | null>
  create(input: CreateProjectInput): Promise<ProjectProps>
  save(id: string, patch: UpdateProjectInput): Promise<ProjectProps | null>
  delete(id: string): Promise<boolean>
}

export interface ProjectSummary {
  id: string
  name: string
  description: string
  template: string
  boardWidth: number
  boardHeight: number
  components: number
  tracks: number
  vias: number
  updatedAt: string
  createdAt: string
}

export interface CreateProjectInput {
  name: string
  description?: string
  template?: string
}

export interface UpdateProjectInput {
  name?: string
  description?: string
  design?: Design
}
