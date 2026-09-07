/**
 * API REST — GET /api/health : état des services.
 */
import { NextResponse } from 'next/server'
import { db } from '@/lib/db'

export async function GET() {
  let dbOk = false
  try {
    await db.project.count()
    dbOk = true
  } catch {
    dbOk = false
  }
  return NextResponse.json({
    status: 'ok',
    app: 'YahriaCad',
    services: {
      api: true,
      database: dbOk,
      aiEngine: { port: 3010, transport: 'socket.io' },
    },
    time: new Date().toISOString(),
  })
}
