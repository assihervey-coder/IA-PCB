/**
 * application/export/netlist — Écriture d'une netlist au format KiCad (s-expression).
 */
import { Design } from '../../shared/types'

export function generateKicadNetlist(design: Design): string {
  const L: string[] = []
  L.push('(export (version "E")')
  L.push(`  (design`)
  L.push(`    (source "kidcad-pro-ia://${design.name}")`)
  L.push(`    (date "${new Date().toISOString()}")`)
  L.push(`    (tool "KidCAD-Pro-IA")`)
  L.push(`    (sheet (number "1") (name "/") (tstamps "/")`)
  L.push(`      (title_block (title "${design.name}") (company "KidCAD-Pro-IA") (rev "") (date ""))` )
  L.push(`    )`)
  L.push(`  )`)

  L.push(`  (components`)
  for (const comp of design.schematic.components) {
    L.push(`    (comp (ref "${comp.ref}")`)
    L.push(`      (value "${comp.value}")`)
    L.push(`      (footprint "KidCAD:${comp.footprintName}")`)
    L.push(`      (libsource (lib "kidcad") (part "${comp.symbol}") (description ""))`)
    L.push(`      (property (name "Sheetname") (value "Root"))`)
    L.push(`      (sheetpath (names "/") (tstamps "/"))`)
    L.push(`      (tstamps "${comp.id}")`)
    L.push(`    )`)
  }
  L.push(`  )`)

  L.push(`  (libparts)`)
  L.push(`  (nets`)
  design.nets.forEach((net, i) => {
    L.push(`    (net (code "${i + 1}") (name "${net.name}")`)
    for (const pin of net.pins) {
      const comp = design.schematic.components.find((c) => c.id === pin.componentId)
      if (comp) L.push(`      (node (ref "${comp.ref}") (pin "${pin.pad}") (pinfunction "${pin.pad}"))`)
    }
    L.push(`    )`)
  })
  L.push(`  )`)
  L.push(`)`)
  return L.join('\n') + '\n'
}
