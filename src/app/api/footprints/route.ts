/**
 * API REST — GET /api/footprints : bibliothèque d'empreintes disponibles.
 */
import { NextResponse } from 'next/server'
import { listFootprints } from '@/lib/kidcad/library/footprints'

export async function GET() {
  return NextResponse.json({ footprints: listFootprints() })
}
