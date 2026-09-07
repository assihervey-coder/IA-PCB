package writer

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/fileio/reader"
)

// TestKicadWriterStructure vérifie les invariants du fichier généré :
// version pcbnew 9, bloc layers avec ids KiCad 9, net 0 vide, attr smd /
// through_hole, coordonnées pads locales.
func TestKicadWriterStructure(t *testing.T) {
	board := odbTestBoard(t) // 2 composants (thru_hole + smd), 1 piste 3 points, 1 via
	w := NewKicadWriter()
	out := t.TempDir()
	path, err := w.Write(board, "Carte Round-Trip", out)
	if err != nil {
		t.Fatalf("Write : %v", err)
	}
	if filepath.Base(path) != "carte-round-trip.kicad_pcb" {
		t.Errorf("nom de fichier inattendu : %s", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"(kicad_pcb (version 20241229)",
		`(generator "yahriacad")`,
		`(0 "F.Cu" signal)`,
		`(2 "B.Cu" signal)`,
		`(net 0 "")`,
		`(net 1 "GND")`,
		`(net 2 "SIG")`,
		`(footprint "DIP8"`,
		`(property "Reference" "U1"`,
		"(attr through_hole)",
		"(attr smd)",
		`(pad "1" thru_hole rect (at -2 -1)`,
		`(pad "1" smd rect (at -0.8 0)`,
		`(gr_rect (start 0 0) (end 40 30)`,
		"(layer \"Edge.Cuts\")",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("fichier généré sans %q", want)
		}
	}
	// la piste 3 points doit produire 2 segments
	if got := strings.Count(s, "(segment "); got != 2 {
		t.Errorf("segments attendus 2, obtenus %d", got)
	}
	if got := strings.Count(s, "(via "); got != 1 {
		t.Errorf("vias attendus 1, obtenus %d", got)
	}
}

// TestKicadWriterRoundTrip boucle complète : écriture .kicad_pcb puis
// relecture par le Registry (le même chemin que l'import REST). Toute la
// géométrie doit survivre au cycle.
func TestKicadWriterRoundTrip(t *testing.T) {
	board := odbTestBoard(t)
	w := NewKicadWriter()
	path, err := w.Write(board, "roundtrip", t.TempDir())
	if err != nil {
		t.Fatalf("Write : %v", err)
	}

	res, err := reader.NewRegistry(slog.Default()).Read(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if res.Board == nil {
		t.Fatal("relecture : carte absente")
	}
	rt := res.Board

	if rt.WidthMM != board.WidthMM || rt.HeightMM != board.HeightMM {
		t.Errorf("dimensions : attendu %gx%g, obtenu %gx%g",
			board.WidthMM, board.HeightMM, rt.WidthMM, rt.HeightMM)
	}
	if len(rt.Components) != len(board.Components) {
		t.Fatalf("composants : attendu %d, obtenu %d", len(board.Components), len(rt.Components))
	}
	for _, orig := range board.Components {
		var got *struct {
			x, y, rot float64
			pads      int
		}
		for i := range rt.Components {
			if rt.Components[i].Ref == orig.Ref {
				c := &rt.Components[i]
				got = &struct {
					x, y, rot float64
					pads      int
				}{c.X, c.Y, c.Rotation, len(c.Footprint.Pads)}
				if c.X != orig.X || c.Y != orig.Y {
					t.Errorf("%s : position attendue (%g,%g), obtenue (%g,%g)",
						orig.Ref, orig.X, orig.Y, c.X, c.Y)
				}
				if c.Rotation != orig.Rotation {
					t.Errorf("%s : rotation attendue %g, obtenue %g", orig.Ref, orig.Rotation, c.Rotation)
				}
				if len(c.Footprint.Pads) != len(orig.Footprint.Pads) {
					t.Errorf("%s : pads attendus %d, obtenus %d",
						orig.Ref, len(orig.Footprint.Pads), len(c.Footprint.Pads))
				}
				for _, op := range orig.Footprint.Pads {
					gp, ok := c.PadByName(op.Name)
					if !ok {
						t.Fatalf("%s : pad %s perdu au round-trip", orig.Ref, op.Name)
					}
					if gp.X != op.X || gp.Y != op.Y {
						t.Errorf("%s.%s : pad local attendu (%g,%g), obtenu (%g,%g)",
							orig.Ref, op.Name, op.X, op.Y, gp.X, gp.Y)
					}
					if gp.Net != op.Net {
						t.Errorf("%s.%s : net attendu %q, obtenu %q", orig.Ref, op.Name, op.Net, gp.Net)
					}
				}
				break
			}
		}
		if got == nil {
			t.Errorf("composant %s introuvable après round-trip", orig.Ref)
		}
	}

	// chaque piste multi-points ressort en segments de 2 points : le
	// nombre total de segments doit être conservé
	wantSegs := 0
	for _, tr := range board.Tracks {
		wantSegs += len(tr.Points) - 1
	}
	gotSegs := 0
	for _, tr := range rt.Tracks {
		gotSegs += len(tr.Points) - 1
	}
	if gotSegs != wantSegs {
		t.Errorf("segments : attendu %d, obtenu %d", wantSegs, gotSegs)
	}
	// net de la piste conservé
	netSeen := map[string]bool{}
	for _, tr := range rt.Tracks {
		netSeen[tr.Net] = true
	}
	if !netSeen["SIG"] {
		t.Error("net SIG perdu sur les pistes")
	}
	if len(rt.Vias) != len(board.Vias) {
		t.Fatalf("vias : attendu %d, obtenu %d", len(board.Vias), len(rt.Vias))
	}
	v := &rt.Vias[0]
	if v.X != 20 || v.Y != 10 || v.Diameter != 0.6 || v.Drill != 0.3 || v.Net != "SIG" {
		t.Errorf("via modifié : %+v", v)
	}
}
