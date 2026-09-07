/**
 * application/verification/erc — Electrical Rule Check (schéma + netlist).
 */
import { Design, ErcReport, Violation } from '../../shared/types'
import { extractNetlist, validateSchematic } from '../../domain/schematic/schematic'

export function runErc(design: Design): ErcReport {
  const violations: Violation[] = []
  const sch = design.schematic

  // 1) Validation structurelle du schéma
  const structural = validateSchematic(sch)
  for (const problem of structural.problems) {
    violations.push({ type: 'DUPLICATE_REF', severity: 'error', message: problem, at: { x: 0, y: 0 } })
  }

  // 2) Bornes non connectées (extraction wires)
  const extracted = extractNetlist(sch)
  for (const pin of extracted.unconnectedPins) {
    const comp = sch.components.find((c) => c.id === pin.componentId)
    if (!comp) continue
    violations.push({
      type: 'UNCONNECTED_PIN', severity: 'warning',
      message: `Borne ${pin.pin} de ${comp.ref} non connectée dans le schéma`,
      at: { x: comp.x, y: comp.y }, items: [comp.id],
    })
  }

  // 3) Nets à borne unique + références introuvables dans le layout
  const compIds = new Set(sch.components.map((c) => c.id))
  const fpComps = new Set(design.layout.footprints.map((f) => f.componentId))
  for (const net of design.nets) {
    if (net.pins.length === 1) {
      violations.push({
        type: 'SINGLE_PIN_NET', severity: 'warning',
        message: `Net « ${net.name} » ne relie qu'une seule borne`,
        at: { x: 0, y: 0 }, items: [net.id],
      })
    }
    for (const pin of net.pins) {
      if (!compIds.has(pin.componentId) && !fpComps.has(pin.componentId)) {
        violations.push({
          type: 'SINGLE_PIN_NET', severity: 'error',
          message: `Net « ${net.name} » référence un composant inexistant (${pin.componentId})`,
          at: { x: 0, y: 0 }, items: [net.id],
        })
      }
    }
  }

  // 4) Composants du schéma sans empreinte placée
  for (const c of sch.components) {
    if (!fpComps.has(c.id)) {
      violations.push({
        type: 'UNCONNECTED_PIN', severity: 'warning',
        message: `${c.ref} n'a pas d'empreinte dans le layout`,
        at: { x: c.x, y: c.y }, items: [c.id],
      })
    }
  }

  return {
    ok: violations.filter((v) => v.severity === 'error').length === 0,
    violations,
    checkedAt: new Date().toISOString(),
    netCount: design.nets.length,
  }
}
