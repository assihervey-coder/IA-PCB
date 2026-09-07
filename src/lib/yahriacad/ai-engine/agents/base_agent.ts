/**
 * ai-engine/agents/base_agent — Classe de base des agents IA
 * (analogue de `base_agent.py`). Chaque agent expose un nom, une description
 * de stratégie et une méthode `run` asynchrone avec callbacks de progression.
 */

export interface ProgressFn {
  (stage: string, progress: number, message: string): void
}

export abstract class BaseAgent<TIn, TOut> {
  abstract readonly name: string
  abstract readonly strategy: string

  protected progress: ProgressFn = () => {}

  withProgress(fn: ProgressFn): this {
    this.progress = fn
    return this
  }

  abstract run(input: TIn): Promise<TOut>
}
