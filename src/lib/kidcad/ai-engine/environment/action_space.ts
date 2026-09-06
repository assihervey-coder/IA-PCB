/**
 * ai-engine/environment/action_space — Espace d'actions du routeur
 * (8 directions + changement de couche via). Analogue de `action_space.py`.
 */
export interface Action {
  dx: -1 | 0 | 1
  dy: -1 | 0 | 1
  via: boolean
}

/** 4 directions orthogonales (garantie DRC sur grille) puis l'action "via".
 * Les diagonales sont volontairement désactivées : deux couloirs diagonaux
 * parallèles espacés d'une cellule violent l'isolement minimal. */
export const ACTIONS: Action[] = [
  { dx: 1, dy: 0, via: false },
  { dx: -1, dy: 0, via: false },
  { dx: 0, dy: 1, via: false },
  { dx: 0, dy: -1, via: false },
  { dx: 0, dy: 0, via: true },
]

export const N_ACTIONS = ACTIONS.length
export const VIA_ACTION = ACTIONS.length - 1

/** Coûts (multiplicateurs du pas de grille). */
export const COST = {
  straight: 1.0,
  diagonal: 1.4142, // conservé pour comparaison, non utilisé (4-connexe)
  turn: 0.12, // pénalité de coude (pistes plus propres)
  via: 10.0, // pénalité de via (surcoût fabrication + fiabilité)
}

export function actionCost(a: Action): number {
  if (a.via) return COST.via
  return a.dx !== 0 && a.dy !== 0 ? COST.diagonal : COST.straight
}
