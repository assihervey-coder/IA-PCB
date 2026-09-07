/**
 * infrastructure/config — Configuration de l'application (env + défauts).
 */
export interface AppConfig {
  appName: string
  databaseUrl: string
  aiEnginePort: number
  logLevel: string
  drc: {
    minTrackWidth: number
    minClearance: number
    minViaDrill: number
    minViaDiameter: number
    edgeClearance: number
  }
}

export function loadConfig(): AppConfig {
  return {
    appName: 'YahriaCad',
    databaseUrl: process.env.DATABASE_URL ?? 'file:../db/custom.db',
    aiEnginePort: Number(process.env.AI_ENGINE_PORT ?? 3010),
    logLevel: process.env.YAHRIACAD_LOG_LEVEL ?? 'info',
    drc: {
      minTrackWidth: 0.25,
      minClearance: 0.2,
      minViaDrill: 0.35,
      minViaDiameter: 0.7,
      edgeClearance: 0.3,
    },
  }
}
