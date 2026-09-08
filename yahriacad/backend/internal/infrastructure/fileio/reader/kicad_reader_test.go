package reader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
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

// --------------------------------------------------------------
// Support multi-versions KiCad 6 → 10
// --------------------------------------------------------------

// kicad6Fixture : KiCad 6.0 (20211030) — référence/valeur encore en
// fp_text, nets numérotés déclarés à la racine, contour en gr_rect.
const kicad6Fixture = `(kicad_pcb
        (version 20211030)
        (generator pcbnew)
        (generator_version "6.0")
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (net 1 "GND")
        (net 2 "VCC")
        (gr_rect
                (start 0 0)
                (end 50 30)
                (layer "Edge.Cuts")
                (width 0.1)
        )
        (footprint "Resistor_SMD:R_0603"
                (layer "F.Cu")
                (at 10 10)
                (fp_text reference "R5" (at 0 -1) (layer "F.SilkS"))
                (fp_text value "4k7" (at 0 1) (layer "F.Fab"))
                (pad "1" smd rect
                        (at -0.75 0)
                        (size 0.8 0.8)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net 2 "VCC")
                )
                (pad "2" smd rect
                        (at 0.75 0)
                        (size 0.8 0.8)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (net 1 "GND")
                )
        )
)`

// kicad7Fixture : KiCad 7.0 (20221206) — Reference/Value passent en
// (property ...), contour tracé en gr_line (cas réel des cartes v6-v10).
const kicad7Fixture = `(kicad_pcb
        (version 20221206)
        (generator pcbnew)
        (generator_version "7.0")
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (net 3 "SDA")
        (footprint "Capacitor_SMD:C_0805"
                (layer "F.Cu")
                (at 20 15)
                (property "Reference" "C9" (at 0 -1.2 0) (layer "F.SilkS"))
                (property "Value" "100n" (at 0 1.2 0) (layer "F.Fab"))
                (property "Datasheet" "" (at 0 0 0) (layer "F.Fab") (hide yes))
                (pad "1" smd roundrect
                        (at -0.9 0)
                        (size 1 1.2)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (roundrect_rratio 0.25)
                        (net 3 "SDA")
                )
                (pad "2" smd roundrect
                        (at 0.9 0)
                        (size 1 1.2)
                        (layers "F.Cu" "F.Paste" "F.Mask")
                        (roundrect_rratio 0.25)
                        (net 1 "GND")
                )
        )
        (gr_line
                (start 5 5)
                (end 45 5)
                (stroke (width 0.1) (type solid))
                (layer "Edge.Cuts")
        )
        (gr_line
                (start 45 5)
                (end 45 35)
                (stroke (width 0.1) (type solid))
                (layer "Edge.Cuts")
        )
        (gr_line
                (start 45 35)
                (end 5 35)
                (stroke (width 0.1) (type solid))
                (layer "Edge.Cuts")
        )
        (gr_line
                (start 5 35)
                (end 5 5)
                (stroke (width 0.1) (type solid))
                (layer "Edge.Cuts")
        )
        (segment
                (start 20 15)
                (end 30 15)
                (width 0.3)
                (layer "F.Cu")
                (net 3)
        )
        (via
                (at 30 15)
                (size 0.8)
                (drill 0.4)
                (layers "F.Cu" "B.Cu")
                (net 3)
        )
)`

// kicad8Fixture : KiCad 8.0 (20240108) — noeuds inconnus tolérés
// (embedded_image, table) et net anonyme (net 0 "" / segment sans nom).
const kicad8Fixture = `(kicad_pcb
        (version 20240108)
        (generator "pcbnew")
        (generator_version "8.0")
        (embedded_fonts no)
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (net 1 "GND")
        (net 0 "")
        (footprint "Inductor_SMD:L_1210"
                (layer "B.Cu")
                (at 15 20)
                (property "Reference" "L1" (at 0 -1.5 180) (layer "B.SilkS"))
                (property "Value" "10u" (at 0 1.5 180) (layer "B.Fab"))
                (pad "1" smd rect
                        (at -0.9 0 180)
                        (size 1.2 2)
                        (layers "B.Cu" "B.Paste" "B.Mask")
                        (net 1 "GND")
                )
                (pad "" smd rect
                        (at 1.2 0 180)
                        (size 0.6 0.8)
                        (layers "B.Cu" "B.Paste" "B.Mask")
                )
        )
        (segment
                (start 15 20)
                (end 25 20)
                (width 0.25)
                (layer "B.Cu")
                (net 1)
        )
        (table
                (rows 2)
                (columns 1)
                (cells (0 0) (1 0))
        )
        (embedded_image
                (uuid "9c1d1a6d-2c58-4c39-b6aa-b3fcaa11d5c6")
                (data "iVBORw0KGgoAAAANSUhEUg==")
        )
)`

func TestKiCadVersions6To10(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantVersion string
		wantNets    int
	}{
		{"kicad6", kicad6Fixture, "KiCad 6", 2},
		{"kicad7", kicad7Fixture, "KiCad 7", 2},
		{"kicad8", kicad8Fixture, "KiCad 8", 1},
		{"kicad9", kicad9Fixture, "KiCad 9", 2},
		{"kicad10", kicad10Fixture, "KiCad 10", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := readFixture(t, tc.src)
			if res.FileVersion != tc.wantVersion {
				t.Fatalf("version détectée %q, attendu %q", res.FileVersion, tc.wantVersion)
			}
			if got := res.Schematic.NetCount(); got != tc.wantNets {
				t.Fatalf("nets attendus %d, obtenus %d", tc.wantNets, got)
			}
			if len(res.Board.Components) != 1 {
				t.Fatalf("composants attendus 1, obtenus %d", len(res.Board.Components))
			}
		})
	}
}

func TestKiCad6FpTextReferences(t *testing.T) {
	res := readFixture(t, kicad6Fixture)
	comp := res.Board.Components[0]
	if comp.Ref != "R5" || comp.Footprint.Value != "4k7" {
		t.Fatalf("référence/valeur v6 : %s / %s", comp.Ref, comp.Footprint.Value)
	}
}

func TestKiCad7PropertyOverFpText(t *testing.T) {
	res := readFixture(t, kicad7Fixture)
	comp := res.Board.Components[0]
	if comp.Ref != "C9" || comp.Footprint.Value != "100n" {
		t.Fatalf("référence/valeur v7 : %s / %s", comp.Ref, comp.Footprint.Value)
	}
	// Contour gr_line (5,5)-(45,35) + marge 1 mm par côté.
	w, h := res.Board.WidthMM, res.Board.HeightMM
	if w < 40 || w > 43 || h < 30 || h > 33 {
		t.Fatalf("contour gr_line : %g x %g mm (attendu ~42 x 32)", w, h)
	}
	// Net nommé via déclaration racine puis référencé par segment/via.
	if len(res.Board.Tracks) != 1 || res.Board.Tracks[0].Net != "SDA" {
		t.Fatalf("piste v7 : %+v", res.Board.Tracks)
	}
	if len(res.Board.Vias) != 1 || res.Board.Vias[0].Net != "SDA" {
		t.Fatalf("via v7 : %+v", res.Board.Vias)
	}
}

func TestKiCad8UnknownNodesTolerated(t *testing.T) {
	res := readFixture(t, kicad8Fixture)
	if got := res.Schematic.NetCount(); got != 1 {
		t.Fatalf("nets attendus 1 (GND), obtenus %d", got)
	}
	comp := res.Board.Components[0]
	if comp.Ref != "L1" {
		t.Fatalf("référence v8 : %s", comp.Ref)
	}
	// Pad anonyme (pad "") : toléré, sans connexion.
	anonymous := 0
	for _, p := range comp.Footprint.Pads {
		if p.Name == "" {
			anonymous++
		}
	}
	if anonymous != 1 {
		t.Fatalf("pad anonyme non importé : %+v", comp.Footprint.Pads)
	}
	// Empreinte montée au dos : couche basse.
	if comp.Footprint.Pads[0].Layer == 0 {
		t.Fatalf("pad au dos attendu en couche B.Cu, obtenu F.Cu")
	}
}

func TestKiCadFutureVersionBestEffort(t *testing.T) {
	future := strings.Replace(kicad10Fixture, "(version 20260206)", "(version 20990101)", 1)
	res := readFixture(t, future)
	if !strings.Contains(res.FileVersion, "non reconnue") {
		t.Fatalf("version future non signalée : %q", res.FileVersion)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "best-effort") {
			found = true
		}
	}
	if !found {
		t.Fatalf("avertissement best-effort absent : %v", res.Warnings)
	}
	if res.Schematic.NetCount() != 2 {
		t.Fatalf("lecture best-effort v11 cassée : %d nets", res.Schematic.NetCount())
	}
}

// kicad5Fixture : KiCad 5 (20171130) — (module ...) au lieu de (footprint ...).
const kicad5Fixture = `(kicad_pcb
        (version 20171130)
        (generator pcbnew)
        (layers
                (0 F.Cu signal)
                (31 B.Cu signal)
        )
        (net 1 GND)
        (module "Package_TO_SOT_SMD:SOT-23" (layer F.Cu) (at 12 12)
                (fp_text reference Q1 (at 0 -2) (layer F.SilkS))
                (fp_text value BC847 (at 0 2) (layer F.Fab))
                (pad 1 smd rect (at -0.95 0) (size 0.6 0.8) (layers F.Cu F.Paste F.Mask) (net 1))
                (pad 2 smd rect (at 0.95 0) (size 0.6 0.8) (layers F.Cu F.Paste F.Mask) (net 1))
        )
)`

func TestKiCad5LegacyModuleAlias(t *testing.T) {
	res := readFixture(t, kicad5Fixture)
	if res.FileVersion != "KiCad 5 (legacy)" {
		t.Fatalf("version v5 : %q", res.FileVersion)
	}
	if len(res.Board.Components) != 1 || res.Board.Components[0].Ref != "Q1" {
		t.Fatalf("module v5 non lu : %+v", res.Board.Components)
	}
	if got := res.Schematic.NetCount(); got != 1 {
		t.Fatalf("nets v5 attendus 1, obtenus %d", got)
	}
}

// TestKiCadRealBoardsLocal rejoue l'import sur de vraies cartes exportées de
// pcbnew (KiCad 9 et 10) si elles sont présentes localement ; en CI, les
// fichiers absents font simplement sauter le test.
func TestKiCadRealBoardsLocal(t *testing.T) {
	cases := []struct {
		path       string
		comps      int
		nets       int
		minWidthMM float64
	}{
		{"/tmp/kicad-complex_hierarchy.kicad_pcb", 68, 52, 100},
		{"/tmp/kicad-video.kicad_pcb", 189, 588, 300},
		{"/tmp/kicad-pic_programmer.kicad_pcb", 63, 111, 155},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Skipf("carte réelle absente : %s", tc.path)
		}
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			res, err := readKiCadPCB(data, tc.path)
			if err != nil {
				t.Fatalf("lecture : %v", err)
			}
			if len(res.Board.Components) != tc.comps {
				t.Fatalf("composants : %d, attendu %d", len(res.Board.Components), tc.comps)
			}
			if res.Schematic.NetCount() != tc.nets {
				t.Fatalf("nets : %d, attendu %d", res.Schematic.NetCount(), tc.nets)
			}
			if res.Board.WidthMM < tc.minWidthMM {
				t.Fatalf("contour trop petit : %g mm", res.Board.WidthMM)
			}
			connexions := 0
			for i := range res.Schematic.Nets {
				connexions += len(res.Schematic.Nets[i].Connections)
			}
			if connexions == 0 {
				t.Fatal("netlist vide : aucune connexion pastille")
			}
		})
	}
}

// kicadBackSideFixture : empreintes dont le côté des pastilles SMD est
// explicite et contraire/indépendant du côté de l'empreinte — jumpers,
// points de test, ou fichier réécrit par un outil tiers (empreinte F.Cu
// avec pastilles B.Cu). Le côté explicite des pastilles doit gagner.
const kicadBackSideFixture = `(kicad_pcb
	(version 20241229)
	(generator "pcbnew")
	(generator_version "9.0")
	(layers
		(0 "F.Cu" signal)
		(31 "B.Cu" signal)
	)
	(net 1 "VCC")
	(net 2 "GND")
	(footprint "Jumper:SolderJumper-2"
		(layer "B.Cu")
		(at 20 20)
		(property "Reference" "JP1")
		(property "Value" "JUMPER")
		(pad "1" smd rect
			(at -0.725 0)
			(size 0.3 0.3)
			(layers "B.Cu" "B.Paste" "B.Mask")
			(net 1 "VCC")
		)
		(pad "2" smd rect
			(at 0.725 0)
			(size 0.3 0.3)
			(layers "F.Cu" "F.Paste" "F.Mask")
			(net 2 "GND")
		)
	)
	(footprint "TestPoint:TP"
		(layer "F.Cu")
		(at 30 20)
		(property "Reference" "TP1")
		(property "Value" "TEST")
		(pad "1" smd rect
			(at 0 0)
			(size 0.5 0.5)
			(layers "B.Cu" "B.Paste" "B.Mask")
			(net 1 "VCC")
		)
	)
)`

func TestKiCadBackSideSMDPads(t *testing.T) {
	res := readFixture(t, kicadBackSideFixture)
	if len(res.Board.Components) != 2 {
		t.Fatalf("composants : %d, attendu 2", len(res.Board.Components))
	}
	var jpPads, tpPads []domainlayout.Pad
	for i := range res.Board.Components {
		c := &res.Board.Components[i]
		switch c.Ref {
		case "JP1":
			jpPads = c.Footprint.Pads
		case "TP1":
			tpPads = c.Footprint.Pads
		}
	}
	if len(jpPads) != 2 || len(tpPads) != 1 {
		t.Fatalf("pads attendus JP1=2 TP1=1, obtenus JP1=%d TP1=%d", len(jpPads), len(tpPads))
	}
	// JP1 (empreinte B.Cu) : pad 1 explicite B.Cu → dernier cuivre ;
	// pad 2 explicite F.Cu → cuivre 0, malgré l'empreinte arrière.
	if jpPads[0].Layer != 1 {
		t.Errorf("JP1 pad 1 : couche %d, attendu 1 (B.Cu explicite)", jpPads[0].Layer)
	}
	if jpPads[1].Layer != 0 {
		t.Errorf("JP1 pad 2 : couche %d, attendu 0 (F.Cu explicite)", jpPads[1].Layer)
	}
	// TP1 (empreinte F.Cu) : pad explicite B.Cu → dernier cuivre.
	if tpPads[0].Layer != 1 {
		t.Errorf("TP1 pad 1 : couche %d, attendu 1 (B.Cu explicite)", tpPads[0].Layer)
	}
}
