/**
 * pkg/logger — Journalisation structurée légère (analogue Zap, sans dépendance).
 */

type Level = 'debug' | 'info' | 'warn' | 'error'

const LEVELS: Record<Level, number> = { debug: 10, info: 20, warn: 30, error: 40 }

const currentLevel = (): Level =>
  (process.env.YAHRIACAD_LOG_LEVEL as Level) || (process.env.NODE_ENV === 'production' ? 'info' : 'debug')

function emit(scope: string, level: Level, msg: string, data?: unknown) {
  if (LEVELS[level] < LEVELS[currentLevel()]) return
  const ts = new Date().toISOString()
  const line = `[${ts}] [${level.toUpperCase()}] [${scope}] ${msg}`
  const payload = data === undefined ? line : [line, data]
  if (level === 'error') console.error(...[].concat(payload as never))
  else if (level === 'warn') console.warn(...[].concat(payload as never))
  else console.log(...[].concat(payload as never))
}

export function createLogger(scope: string) {
  return {
    debug: (msg: string, data?: unknown) => emit(scope, 'debug', msg, data),
    info: (msg: string, data?: unknown) => emit(scope, 'info', msg, data),
    warn: (msg: string, data?: unknown) => emit(scope, 'warn', msg, data),
    error: (msg: string, data?: unknown) => emit(scope, 'error', msg, data),
  }
}

export type Logger = ReturnType<typeof createLogger>
