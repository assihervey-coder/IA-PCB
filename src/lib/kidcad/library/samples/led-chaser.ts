/**
 * library/samples/led-chaser — Projet démo : "Kit LED Chaser 10 voies"
 * NE555 en astable + CD4017 décadique + 10 LEDs. Netlist explicite
 * (style import KiCad) : 27 composants, 27 nets, carte 90×60 mm.
 */
import { Design, Net, SchematicComponent, SchematicData } from '../../shared/types'
import { DEFAULT_RULES } from '../../shared/types'
import { instantiateFootprint } from '../footprints'
import { makePins } from '../symbols'

export function buildLedChaserDesign(id = '', name = 'Kit LED Chaser 10 voies'): Design {
  // ------------------------------------------------------------------
  // Composants du schéma (positions pour l'affichage)
  // ------------------------------------------------------------------
  const comps: SchematicComponent[] = []
  let cid = 0
  const addComp = (c: Omit<SchematicComponent, 'id'>): SchematicComponent => {
    const comp = { ...c, id: `sc_${++cid}` }
    comps.push(comp)
    return comp
  }

  const u1 = addComp({ ref: 'U1', value: 'NE555', symbol: 'IC', pins: makePins('IC', 8), x: 60, y: 90, rotation: 0, footprintName: 'DIP-8' })
  const u2 = addComp({ ref: 'U2', value: 'CD4017', symbol: 'IC', pins: makePins('IC', 16), x: 130, y: 90, rotation: 0, footprintName: 'DIP-16' })
  const r1 = addComp({ ref: 'R1', value: '4.7k', symbol: 'R', pins: makePins('R'), x: 35, y: 60, rotation: 0, footprintName: 'R-0805' })
  const r2 = addComp({ ref: 'R2', value: '1k', symbol: 'R', pins: makePins('R'), x: 60, y: 60, rotation: 0, footprintName: 'R-0805' })
  const c1 = addComp({ ref: 'C1', value: '10µF', symbol: 'CP', pins: makePins('CP'), x: 35, y: 130, rotation: 0, footprintName: 'CP-ELEC-8' })
  const c2 = addComp({ ref: 'C2', value: '10nF', symbol: 'C', pins: makePins('C'), x: 60, y: 130, rotation: 0, footprintName: 'C-0805' })
  const j1 = addComp({ ref: 'J1', value: 'Alim 5V', symbol: 'J', pins: makePins('J', 2), x: 20, y: 90, rotation: 0, footprintName: 'HDR-1x2' })
  const leds: SchematicComponent[] = []
  const rs: SchematicComponent[] = []
  for (let i = 1; i <= 10; i++) {
    leds.push(addComp({ ref: `D${i}`, value: 'LED', symbol: 'LED', pins: makePins('LED'), x: 190, y: 40 + (i - 1) * 13, rotation: 0, footprintName: 'LED-5' }))
    rs.push(addComp({ ref: `R${i + 2}`, value: '330R', symbol: 'R', pins: makePins('R'), x: 235, y: 40 + (i - 1) * 13, rotation: 90, footprintName: 'R-0805' }))
  }

  const schematic: SchematicData = { components: comps, wires: [] }

  // ------------------------------------------------------------------
  // Netlist explicite (identique à tests/fixtures/led-chaser.netlist.kicad.net)
  // ------------------------------------------------------------------
  const nets: Net[] = []
  let netId = 0
  const net = (name: string, pins: { componentId: string; pad: string }[]) => {
    nets.push({ id: `n${++netId}`, name, pins })
  }
  const p = (comp: SchematicComponent, pad: string) => ({ componentId: comp.id, pad })

  net('VCC', [p(j1, '1'), p(u1, '8'), p(u1, '4'), p(u2, '16'), p(r1, '1')])
  net('GND', [
    p(j1, '2'), p(u1, '1'), p(c1, '-'), p(c2, '2'), p(u2, '8'), p(u2, '13'), p(u2, '15'),
    ...rs.map((r) => p(r, '2')),
  ])
  net('CLK', [p(u1, '3'), p(u2, '14')])
  net('DISCH', [p(r1, '2'), p(r2, '1'), p(u1, '7')])
  net('THRESH', [p(r2, '2'), p(u1, '6'), p(u1, '2'), p(c1, '+')])
  net('CTRL', [p(u1, '5'), p(c2, '1')])
  const qPins = ['3', '2', '4', '7', '10', '1', '5', '6', '9', '11']
  leds.forEach((led, i) => {
    net(`OUT${i}`, [p(u2, qPins[i]), p(led, 'A')])
    net(`K${i}`, [p(led, 'K'), p(rs[i], '1')])
  })

  // ------------------------------------------------------------------
  // Layout pré-placé (le placement IA peut tout réorganiser)
  // ------------------------------------------------------------------
  const layout = { footprints: [], tracks: [], vias: [] as never[] }
  const place = (
    comp: SchematicComponent, x: number, y: number, rotation = 0,
  ) => {
    const fp = instantiateFootprint(comp.footprintName, {
      componentId: comp.id, ref: comp.ref, value: comp.value, x, y, rotation,
    })
    if (fp) layout.footprints.push(fp)
  }

  place(j1, 8, 30)
  place(u1, 30, 30)
  place(u2, 58, 32)
  place(r1, 14, 12)
  place(r2, 20, 12)
  place(c1, 30, 52)
  place(c2, 42, 52)
  leds.forEach((led, i) => place(led, 82, 8 + i * 5.2))
  rs.forEach((r, i) => place(r, 74, 8 + i * 5.2))

  return {
    id,
    name,
    description: 'Chenillard 10 LEDs — NE555 horloge + CD4017 compteur décade. Projet de démonstration KidCAD-Pro-IA.',
    board: { width: 90, height: 60, layers: 2 },
    rules: { ...DEFAULT_RULES },
    schematic,
    nets,
    layout,
  }
}
