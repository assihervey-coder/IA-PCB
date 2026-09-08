package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// odbTestBoard builds a 2-layer board with two components (one through-hole
// pad, one SMD pad on the top layer), one 2-segment track and one via.
func odbTestBoard(t *testing.T) *domainlayout.Board {
	t.Helper()
	board, err := domainlayout.NewBoard(40, 30, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	thFP := domainlayout.Footprint{
		Name: "DIP8", BodyWidthMM: 6, BodyHeightMM: 4, HeightMM: 2,
		Pads: []domainlayout.Pad{
			{Name: "1", Shape: domainlayout.ShapeRect, X: -2, Y: -1, Width: 1.2, Height: 0.8, Layer: -1, Net: "GND"},
			{Name: "2", Shape: domainlayout.ShapeCircle, X: 2, Y: -1, Width: 1.2, Height: 1.2, Layer: -1, Net: "SIG"},
		},
	}
	smdFP := domainlayout.Footprint{
		Name: "0603", BodyWidthMM: 2, BodyHeightMM: 1.2, HeightMM: 0.5,
		Pads: []domainlayout.Pad{
			{Name: "1", Shape: domainlayout.ShapeRect, X: -0.8, Y: 0, Width: 0.6, Height: 0.8, Layer: 0, Net: "SIG"},
		},
	}
	for _, c := range []domainlayout.PlacedComponent{
		{Ref: "U1", Footprint: thFP, X: 10, Y: 10},
		{Ref: "R1", Footprint: smdFP, X: 28, Y: 10},
	} {
		if err := board.AddComponent(c); err != nil {
			t.Fatalf("AddComponent %s : %v", c.Ref, err)
		}
	}
	board.Tracks = append(board.Tracks, domainlayout.Track{
		Net: "SIG", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 8, Y: 10}, {X: 20, Y: 10}, {X: 27.2, Y: 10}},
	})
	board.Vias = append(board.Vias, domainlayout.Via{X: 20, Y: 10, FromLayer: 0, ToLayer: 1, Diameter: 0.6, Drill: 0.3, Net: "SIG"})
	return board
}

// TestODBPPWriterTree : l'arborescence ODB++ attendue est produite.
func TestODBPPWriterTree(t *testing.T) {
	board := odbTestBoard(t)
	outDir := t.TempDir()

	w := NewODBPPWriter()
	files, err := w.Write(board, "Carte Test", outDir)
	if err != nil {
		t.Fatalf("Write : %v", err)
	}

	expected := []string{
		"matrix/matrix",
		"netlist/netlist",
		"steps/pcb/general/general",
		"steps/pcb/symbols/symbols",
		"steps/pcb/layers/pclk1/features",
		"steps/pcb/layers/pclk1/components",
		"steps/pcb/layers/pclk2/features",
		"steps/pcb/layers/pclk2/components",
	}
	for _, rel := range expected {
		path := filepath.Join(outDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("fichier attendu manquant : %s", rel)
		}
	}
	if len(files) != len(expected) {
		t.Errorf("%d fichiers attendus, obtenu %d", len(expected), len(files))
	}
}

// TestODBPPWriterFeatures : les features cuivre (pastilles, pistes, via)
// et la netlist portent le bon contenu.
func TestODBPPWriterFeatures(t *testing.T) {
	board := odbTestBoard(t)
	outDir := t.TempDir()

	w := NewODBPPWriter()
	if _, err := w.Write(board, "carte", outDir); err != nil {
		t.Fatalf("Write : %v", err)
	}

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatalf("lecture %s : %v", rel, err)
		}
		return string(b)
	}

	// Couche 1 : 1 pastille traversant + 1 pastille SMD + 2 segments + via.
	f1 := read("steps/pcb/layers/pclk1/features")
	if !strings.Contains(f1, "#Class = copper") || !strings.Contains(f1, "UNITS=UM") {
		t.Fatalf("en-tête features invalide : %q", f1)
	}
	if got := strings.Count(f1, "\nL "); got != 2 {
		t.Errorf("2 segments L attendus en couche 1, obtenu %d", got)
	}
	if got := strings.Count(f1, "\nV "); got != 1 {
		t.Errorf("1 via V attendu en couche 1, obtenu %d", got)
	}

	// Couche 2 : seulement les 2 pastilles traversant de U1 + le via.
	f2 := read("steps/pcb/layers/pclk2/features")
	if got := strings.Count(f2, "\nP "); got != 2 {
		t.Errorf("2 pastilles P attendues en couche 2 (traversant), obtenu %d", got)
	}
	if !strings.Contains(f2, "\nV ") {
		t.Errorf("via attendu en couche 2")
	}

	// Netlist : deux nets, noeuds U1.1/R1.1 (GND) et U1.2/R1.1 (SIG).
	nl := read("netlist/netlist")
	for _, want := range []string{"NET 'GND'", "NET 'SIG'", "COMP 'U1' PIN '1'", "COMP 'R1' PIN '1'"} {
		if !strings.Contains(nl, want) {
			t.Errorf("netlist : %q manquant", want)
		}
	}

	// Matrix : noms canoniques de couches.
	mx := read("matrix/matrix")
	if !strings.Contains(mx, `"F.Cu"`) || !strings.Contains(mx, `"B.Cu"`) {
		t.Errorf("matrix : noms de couches canoniques attendus : %q", mx)
	}

	// Composants : U1 et R1 sur la couche top, aucun en couche bottom.
	comp1 := read("steps/pcb/layers/pclk1/components")
	if !strings.Contains(comp1, "COMP U1") || !strings.Contains(comp1, "COMP R1") {
		t.Errorf("composants top : U1 et R1 attendus : %q", comp1)
	}
	comp2 := read("steps/pcb/layers/pclk2/components")
	if strings.Contains(comp2, "COMP U1") || strings.Contains(comp2, "COMP R1") {
		t.Errorf("aucun composant attendu en bottom : %q", comp2)
	}

	// Symboles : largeur 250 µm + pastilles rect/circle présentes.
	sy := read("steps/pcb/symbols/symbols")
	for _, want := range []string{"w 250", "r 1200 800", "c 1200"} {
		if !strings.Contains(sy, want) {
			t.Errorf("symboles : %q manquant dans %q", want, sy)
		}
	}
}

// TestODBPPWriterRejectsInvalidBoard : garde-fous d'entrée.
func TestODBPPWriterRejectsInvalidBoard(t *testing.T) {
	w := NewODBPPWriter()
	if _, err := w.Write(nil, "x", t.TempDir()); err == nil {
		t.Fatal("carte nil : erreur attendue")
	}
	board, err := domainlayout.NewBoard(10, 10, 1)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	board.LayerCount = 0
	if _, err := w.Write(board, "x", t.TempDir()); err == nil {
		t.Fatal("nombre de couches invalide : erreur attendue")
	}
}
