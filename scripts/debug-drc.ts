/** Debug DRC : affiche les violations après routage du projet démo. */
import { buildLedChaserDesign } from '../src/lib/yahriacad/library/samples/led-chaser'
import { runRouting } from '../src/lib/yahriacad/ai-engine'
import { runDrc } from '../src/lib/yahriacad/application/verification/drc'

const design = buildLedChaserDesign('dbg')
const stats = await runRouting(design, { maxPasses: 2 }, () => {})
console.log(`routage : ${stats.routedNets}/${stats.totalNets}, ${stats.vias} vias`)
console.log(`tracks: ${design.layout.tracks.length}, nets: ${design.nets.length}`)
const report = runDrc(design)
console.log(`violations: ${report.violations.length}, ok=${report.ok}`)
for (const v of report.violations.slice(0, 15)) {
  console.log(`[${v.severity}] ${v.type} @ (${v.at.x.toFixed(2)}, ${v.at.y.toFixed(2)}) — ${v.message}`)
}
