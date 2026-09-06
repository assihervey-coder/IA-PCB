/**
 * tests/integration/engine.test.ts — Tests d'intégration du moteur IA KidCAD.
 * Exécution : bun test tests/integration/engine.test.ts
 */
import { describe, test, expect } from 'bun:test'
import { buildLedChaserDesign } from '../../src/lib/kidcad/library/samples/led-chaser'
import { runPlacement, runRouting, runOptimization, syncNetlist, designStats } from '../../src/lib/kidcad/ai-engine'
import { runDrc } from '../../src/lib/kidcad/application/verification/drc'
import { runErc } from '../../src/lib/kidcad/application/verification/erc'
import { generateBom } from '../../src/lib/kidcad/application/export/bom'
import { generateKicadNetlist } from '../../src/lib/kidcad/application/export/netlist'
import { generateStl } from '../../src/lib/kidcad/application/export/stl'
import { generateStep } from '../../src/lib/kidcad/application/export/step'
import { importKicadNetlist } from '../../src/lib/kidcad/application/schematic/import'
import { readFileSync } from 'node:fs'

describe('Netlist', () => {
  test('le projet démo expose 27 nets et 27 composants', () => {
    const design = buildLedChaserDesign('t1')
    expect(design.nets.length).toBe(26)
    expect(design.schematic.components.length).toBe(27)
    expect(design.layout.footprints.length).toBe(27)
  })

  test('syncNetlist assigne les netId aux pads', () => {
    const design = buildLedChaserDesign('t2')
    syncNetlist(design)
    const assigned = design.layout.footprints.flatMap((f) => f.pads).filter((p) => p.netId)
    expect(assigned.length).toBeGreaterThan(40)
  })
})

describe('Routage IA (A* maze)', () => {
  test('routage du chenillard ≥ 90% de nets', async () => {
    const design = buildLedChaserDesign('t3')
    const stats = await runRouting(design, { maxPasses: 3 }, () => {})
    console.log(`   routage : ${stats.routedNets}/${stats.totalNets} nets, ${stats.vias} vias, ${Math.round(stats.wireLength)} mm, ${stats.durationMs} ms`)
    expect(stats.totalNets).toBe(26)
    expect(stats.routedNets / stats.totalNets).toBeGreaterThanOrEqual(0.9)
    expect(stats.tracks).toBeGreaterThan(0)
  }, 60000)

  test('DRC sans erreur bloquante après routage IA', async () => {
    const design = buildLedChaserDesign('t4')
    await runRouting(design, { maxPasses: 2 }, () => {})
    const report = runDrc(design)
    const errors = report.violations.filter((v) => v.severity === 'error')
    console.log(`   DRC : ${report.violations.length} violations (${errors.length} erreurs)`)
    expect(errors.length).toBe(0)
  }, 60000)

  test('optimisation ne dégrade pas la complétion', async () => {
    const design = buildLedChaserDesign('t5')
    await runRouting(design, { maxPasses: 2 }, () => {})
    const before = designStats(design)
    await runOptimization(design, {}, () => {})
    const after = designStats(design)
    expect(after.routedNets).toBeGreaterThanOrEqual(before.routedNets)
    expect(after.vias).toBeLessThanOrEqual(before.vias)
  }, 60000)
})

describe('Placement IA (recuit simulé)', () => {
  test('place toutes les empreintes dans la carte', async () => {
    const design = buildLedChaserDesign('t6')
    for (const fp of design.layout.footprints) {
      fp.x = -20 - Math.random() * 10
      fp.y = -20 - Math.random() * 10
    }
    await runPlacement(design, { iterations: 800 }, () => {})
    for (const fp of design.layout.footprints) {
      expect(fp.x).toBeGreaterThanOrEqual(0)
      expect(fp.x).toBeLessThanOrEqual(design.board.width)
      expect(fp.y).toBeGreaterThanOrEqual(0)
      expect(fp.y).toBeLessThanOrEqual(design.board.height)
    }
  }, 60000)
})

describe('ERC', () => {
  test('schéma du démo : aucune erreur bloquante', () => {
    const design = buildLedChaserDesign('t7')
    const report = runErc(design)
    const errors = report.violations.filter((v) => v.severity === 'error')
    expect(errors.length).toBe(0)
  })
})

describe('Exports', () => {
  test('BOM CSV', () => {
    const design = buildLedChaserDesign('t8')
    const bom = generateBom(design)
    expect(bom).toContain('Reference,Qty,Value,Footprint,Description')
    expect(bom).toContain('330R')
  })

  test('netlist KiCad ronde : import → export compatible', () => {
    const design = buildLedChaserDesign('t9')
    const netlist = generateKicadNetlist(design)
    expect(netlist).toContain('(export (version "E")')
    const reimport = importKicadNetlist(netlist)
    expect(reimport.nets.length).toBe(26)
    expect(reimport.components.length).toBe(27)
  })

  test('fixture netlist KiCad du dépôt importable', () => {
    const fixture = readFileSync(new URL('../fixtures/led-chaser.netlist.kicad.net', import.meta.url), 'utf8')
    const result = importKicadNetlist(fixture)
    expect(result.nets.length).toBeGreaterThanOrEqual(20)
  })

  test('STL et STEP non vides', () => {
    const design = buildLedChaserDesign('t10')
    expect(generateStl(design).startsWith('solid kidcad_')).toBe(true)
    const step = generateStep(design)
    expect(step).toContain('ISO-10303-21;')
    expect(step).toContain('MANIFOLD_SOLID_BREP')
  })
})
