/**
 * domain/constraints — Règles de conception (DRC) et ensemble de contraintes.
 */
import { DesignRules, DEFAULT_RULES } from '../../shared/types'

export type RuleSeverity = 'error' | 'warning'

export interface RuleDefinition {
  key: keyof DesignRules
  label: string
  unit: 'mm'
  min: number
  max: number
  severity: RuleSeverity
  description: string
}

/** Catalogue des règles paramétrables d'un projet. */
export const RULE_CATALOG: RuleDefinition[] = [
  {
    key: 'minTrackWidth', label: 'Largeur de piste minimale', unit: 'mm', min: 0.1, max: 5,
    severity: 'error', description: 'Largeur de cuivre la plus fine autorisée par le fabricant.',
  },
  {
    key: 'minClearance', label: 'Isolation minimale', unit: 'mm', min: 0.1, max: 2,
    severity: 'error', description: 'Distance minimale entre deux objets cuivre de nets différents.',
  },
  {
    key: 'minViaDrill', label: 'Perçage via minimal', unit: 'mm', min: 0.2, max: 1.5,
    severity: 'error', description: 'Diamètre de perçage minimal d’un via.',
  },
  {
    key: 'minViaDiameter', label: 'Diamètre via minimal', unit: 'mm', min: 0.4, max: 3,
    severity: 'error', description: 'Diamètre extérieur (pastille) minimal d’un via.',
  },
  {
    key: 'edgeClearance', label: 'Distance au bord', unit: 'mm', min: 0.1, max: 2,
    severity: 'error', description: 'Distance minimale entre cuivre et bord de carte.',
  },
  {
    key: 'trackWidth', label: 'Largeur de routage IA', unit: 'mm', min: 0.15, max: 5,
    severity: 'warning', description: 'Largeur de piste utilisée par le routeur automatique.',
  },
  {
    key: 'gridStep', label: 'Pas de grille de routage', unit: 'mm', min: 0.2, max: 2.54,
    severity: 'warning', description: 'Résolution de la grille du moteur de routage IA.',
  },
]

/** Ensemble de contraintes d'un projet (valeur + héritage des défauts). */
export class ConstraintSet {
  private rules: DesignRules

  constructor(partial?: Partial<DesignRules>) {
    this.rules = { ...DEFAULT_RULES, ...(partial ?? {}) }
    this.sanitize()
  }

  get value(): DesignRules {
    return { ...this.rules }
  }

  /** Garantit la cohérence : largeur IA ≥ largeur minimale, via ≥ perçage… */
  private sanitize() {
    const r = this.rules
    r.trackWidth = Math.max(r.trackWidth, r.minTrackWidth)
    r.viaDiameter = Math.max(r.viaDiameter, r.minViaDiameter, r.viaDrill + 2 * r.minClearance)
    r.minViaDrill = Math.min(r.minViaDrill, r.viaDiameter - 0.1)
    r.gridStep = Math.max(r.gridStep, 0.2)
    r.minClearance = Math.max(r.minClearance, 0.1)
  }

  update(partial: Partial<DesignRules>): DesignRules {
    this.rules = { ...this.rules, ...partial }
    this.sanitize()
    return this.value
  }

  validate(): { ok: boolean; problems: string[] } {
    const problems: string[] = []
    for (const def of RULE_CATALOG) {
      const v = this.rules[def.key] as number
      if (v < def.min || v > def.max) {
        problems.push(`${def.label} hors bornes (${def.min}–${def.max} ${def.unit})`)
      }
    }
    if (this.rules.trackWidth < this.rules.minTrackWidth) {
      problems.push('La largeur de routage IA est inférieure à la largeur minimale DRC')
    }
    return { ok: problems.length === 0, problems }
  }

  static fromJSON(json: unknown): ConstraintSet {
    if (!json || typeof json !== 'object') return new ConstraintSet()
    return new ConstraintSet(json as Partial<DesignRules>)
  }
}
