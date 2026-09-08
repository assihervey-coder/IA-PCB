/**
 * application/export/bom — Nomenclature (BOM) CSV, groupée par valeur+empreinte.
 */
import { Design } from '../../shared/types'

function csvEscape(v: string): string {
  if (/[",;\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`
  return v
}

export function generateBom(design: Design): string {
  const groups = new Map<string, { refs: string[]; value: string; footprint: string }>()
  for (const fp of design.layout.footprints) {
    const key = `${fp.value}|${fp.footprintName}`
    if (!groups.has(key)) groups.set(key, { refs: [], value: fp.value, footprint: fp.footprintName })
    groups.get(key)!.refs.push(fp.ref)
  }

  const lines: string[] = [
    'Reference,Qty,Value,Footprint,Description',
  ]
  for (const g of [...groups.values()].sort((a, b) => a.refs[0].localeCompare(b.refs[0], 'fr', { numeric: true }))) {
    lines.push(
      [
        g.refs.sort((a, b) => a.localeCompare(b, 'fr', { numeric: true })).join(';'),
        String(g.refs.length),
        csvEscape(g.value),
        csvEscape(g.footprint),
        csvEscape(`${g.value} — ${g.footprint}`),
      ].join(','),
    )
  }
  return lines.join('\n') + '\n'
}
