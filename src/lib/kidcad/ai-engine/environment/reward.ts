/**
 * ai-engine/environment/reward — Fonctions de récompense / coût
 * (analogue de `reward.py`) : progression vers la cible, pénalités
 * de vias, de coudes et de congestion.
 */
import { COST } from './action_space'

export interface RewardWeights {
  viaPenalty: number
  turnPenalty: number
  congestionPenalty: number
}

export const DEFAULT_REWARD_WEIGHTS: RewardWeights = {
  viaPenalty: COST.via,
  turnPenalty: COST.turn,
  congestionPenalty: 0.35,
}

/**
 * Récompense de progression : delta de distance euclidienne réduite à la cible.
 * Positive quand on se rapproche — utilisée pour l'heuristique A* (h).
 */
export function progressReward(from: { x: number; y: number }, to: { x: number; y: number }): number {
  return -Math.hypot(from.x - to.x, from.y - to.y)
}

/** Heuristique A* : distance octile (compatible mouvements 8-connexité). */
export function octile(dx: number, dy: number): number {
  const ax = Math.abs(dx)
  const ay = Math.abs(dy)
  return ax > ay
    ? ax - ay + COST.diagonal * ay
    : ay - ax + COST.diagonal * ax
}

/**
 * Pénalité de congestion : encourage la répartition des pistes en pénalisant
 * les cellules adjacentes déjà occupées (sommets de degré élevé).
 */
export function congestionPenalty(neighborOccupied: number): number {
  return neighborOccupied * 0.02
}
