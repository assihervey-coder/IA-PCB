package reader

import (
        "strings"
        "testing"

        schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
)

// kicad10Fixture : extrait minimal au format KiCad 10 (version 20260206) —
// les nets y sont référencés PAR NOM, sans numéro, et ne sont déclarés
// nulle part au niveau racine.
const kicad10Fixture = `(kicad_pcb
        (version 20260206)
        (generator "pcbnew")
        (generator_version "10.0")
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (footprint "Resistor_SMD:R_0805"
                (layer "F.Cu")
                (at 10 10)
                (property "Reference" "R1")
                (property "Value" "10k")
                (pad "1" smd roundrect
                        (at 0 -0.5)
                        (size 0.9 1.0)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net "VCC")
                )
                (pad "2" smd roundrect
                        (at 0 0.5)
                        (size 0.9 1.0)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net "GND")
                )
        )
        (segment
                (start 10 10.5)
                (end 20 10.5)
                (width 0.25)
                (layer "F.Cu")
                (net "GND")
        )
)`

// kicad9Fixture : même carte au format KiCad <= 9 (nets numérotés, avec
// déclarations racine) — garde-fou de rétrocompatibilité.
const kicad9Fixture = `(kicad_pcb
        (version 20241229)
        (generator "pcbnew")
        (generator_version "9.0")
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (net 1 "GND")
        (net 2 "VCC")
        (footprint "Resistor_SMD:R_0805"
                (layer "F.Cu")
                (at 10 10)
                (property "Reference" "R1")
                (property "Value" "10k")
                (pad "1" smd roundrect
                        (at 0 -0.5)
                        (size 0.9 1.0)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net 2 "VCC")
                )
                (pad "2" smd roundrect
                        (at 0 0.5)
                        (size 0.9 1.0)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net 1 "GND")
                )
        )
        (segment
                (start 10 10.5)
                (end 20 10.5)
                (width 0.25)
                (layer "F.Cu")
                (net 1)
        )
)`

func readFixture(t *testing.T, src string) schematicapp.ImportResult {
        t.Helper()
        res, err := readKiCadPCB([]byte(src), "test.kicad_pcb")
        if err != nil {
                t.Fatalf("lecture : %v", err)
        }
        return res
}

func TestKiCad10NameOnlyNets(t *testing.T) {
        res := readFixture(t, kicad10Fixture)

        if got := res.Schematic.NetCount(); got != 2 {
                t.Fatalf("nets attendus 2, obtenus %d", got)
        }
        gnd, ok := res.Schematic.NetByName("GND")
        if !ok {
                t.Fatal("net GND introuvable")
        }
        if len(gnd.Connections) != 1 || gnd.Connections[0].ComponentRef != "R1" ||
                gnd.Connections[0].PinNumber != "2" {
                t.Fatalf("connexions GND inattendues : %+v", gnd.Connections)
        }
        if _, ok := res.Schematic.NetByName("VCC"); !ok {
                t.Fatal("net VCC introuvable")
        }

        comp := res.Board.Components[0]
        if comp.Ref != "R1" {
                t.Fatalf("référence composant : %s", comp.Ref)
        }
        if comp.Footprint.Pads[1].Net != "GND" ||
                comp.Footprint.Pads[0].Net != "VCC" {
                t.Fatalf("nets des pastilles : %+v", comp.Footprint.Pads)
        }
        if len(res.Board.Tracks) != 1 || res.Board.Tracks[0].Net != "GND" {
                t.Fatalf("piste : %+v", res.Board.Tracks)
        }
}

func TestKiCad9NumberedNets(t *testing.T) {
        res := readFixture(t, kicad9Fixture)

        if got := res.Schematic.NetCount(); got != 2 {
                t.Fatalf("nets attendus 2, obtenus %d", got)
        }
        if _, ok := res.Schematic.NetByName("GND"); !ok {
                t.Fatal("net GND introuvable (format numéroté cassé)")
        }
        comp := res.Board.Components[0]
        if comp.Footprint.Pads[1].Net != "GND" ||
                comp.Footprint.Pads[0].Net != "VCC" {
                t.Fatalf("nets des pastilles : %+v", comp.Footprint.Pads)
        }
        if len(res.Board.Tracks) != 1 || res.Board.Tracks[0].Net != "GND" {
                t.Fatalf("piste : %+v", res.Board.Tracks)
        }
}

func TestKiCad10SyntheticNumbersAreStable(t *testing.T) {
        // Deux nets homonymes rencontrés dans des ordres différents doivent
        // partager le même numéro synthétique (unicité par nom).
        res := readFixture(t, strings.ReplaceAll(kicad10Fixture,
                `(start 10 10.5)`, `(start 11 10.5)`))
        if got := res.Schematic.NetCount(); got != 2 {
                t.Fatalf("nets attendus 2, obtenus %d", got)
        }
        for _, name := range []string{"GND", "VCC"} {
                if _, ok := res.Schematic.NetByName(name); !ok {
                        t.Fatalf("net %s introuvable après re-parse", name)
                }
        }
}
