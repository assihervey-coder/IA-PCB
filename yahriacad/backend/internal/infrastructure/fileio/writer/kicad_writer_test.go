package writer

import (
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
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
					if gp.Layer != op.Layer {
						t.Errorf("%s.%s : couche attendue %d, obtenue %d", orig.Ref, op.Name, op.Layer, gp.Layer)
					}
				}
				break
			}
		}
		if got == nil {
			t.Errorf("composant %s introuvable après round-trip", orig.Ref)
		}
	}

	// la fusion des segments contigus/colinéaires à l'import modifie la
	// granularité (points redondants supprimés), pas la géométrie : la
	// longueur totale du cuivre est l'invariant du round-trip
	pathLen := func(tracks []domainlayout.Track) float64 {
		total := 0.0
		for _, tr := range tracks {
			for i := 1; i < len(tr.Points); i++ {
				dx := tr.Points[i].X - tr.Points[i-1].X
				dy := tr.Points[i].Y - tr.Points[i-1].Y
				total += math.Sqrt(dx*dx + dy*dy)
			}
		}
		return total
	}
	const epsLen = 1e-6
	if want, got := pathLen(board.Tracks), pathLen(rt.Tracks); math.Abs(want-got) > epsLen {
		t.Errorf("longueur de cuivre : attendu %g, obtenu %g", want, got)
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

// TestKicadWriterBackSideFootprint vérifie qu'une empreinte purement SMD
// dont les pastilles sont sur le dernier cuivre est écrite côté B.Cu, et
// qu'un aller-retour écriture→lecture conserve le côté des pastilles.
func TestKicadWriterBackSideFootprint(t *testing.T) {
	board, err := domainlayout.NewBoard(40, 30, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	backFP := domainlayout.Footprint{
		Name: "SolderJumper-2", BodyWidthMM: 2, BodyHeightMM: 1.5, HeightMM: 0.5,
		Pads: []domainlayout.Pad{
			{Name: "1", Shape: domainlayout.ShapeRect, X: -0.725, Y: 0, Width: 0.3, Height: 0.3, Layer: 1, Net: "VCC"},
			{Name: "2", Shape: domainlayout.ShapeRect, X: 0.725, Y: 0, Width: 0.3, Height: 0.3, Layer: 1, Net: "GND"},
		},
	}
	frontFP := domainlayout.Footprint{
		Name: "0603", BodyWidthMM: 2, BodyHeightMM: 1.2, HeightMM: 0.5,
		Pads: []domainlayout.Pad{
			{Name: "1", Shape: domainlayout.ShapeRect, X: -0.8, Y: 0, Width: 0.6, Height: 0.8, Layer: 0, Net: "VCC"},
		},
	}
	for _, c := range []domainlayout.PlacedComponent{
		{Ref: "JP1", Footprint: backFP, X: 20, Y: 15},
		{Ref: "R1", Footprint: frontFP, X: 32, Y: 20},
	} {
		if err := board.AddComponent(c); err != nil {
			t.Fatalf("AddComponent %s : %v", c.Ref, err)
		}
	}
	board.Tracks = append(board.Tracks, domainlayout.Track{
		Net: "VCC", Layer: 1, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 21, Y: 15}, {X: 31.2, Y: 20}},
	})

	w := NewKicadWriter()
	out := t.TempDir()
	path, err := w.Write(board, "Carte Back-Side", out)
	if err != nil {
		t.Fatalf("Write : %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `(layer "B.Cu")`) {
		t.Errorf("empreinte arrière JP1 non écrite en B.Cu :\n%s", s)
	}
	if !strings.Contains(s, `(layers "B.Cu" "B.Paste" "B.Mask")`) {
		t.Errorf("pastilles arrière sans liste B.Cu/B.Paste/B.Mask")
	}

	// aller-retour : la relecture conserve le côté des pastilles et des
	// empreintes (pastille F.Cu explicite dans une empreinte F.Cu).
	res, err := reader.NewRegistry(slog.Default()).Read(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	for i := range res.Board.Components {
		c := &res.Board.Components[i]
		switch c.Ref {
		case "JP1":
			for _, p := range c.Footprint.Pads {
				if p.Layer != 1 {
					t.Errorf("JP1.%s : couche %d, attendu 1 (B.Cu)", p.Name, p.Layer)
				}
			}
		case "R1":
			if c.Footprint.Pads[0].Layer != 0 {
				t.Errorf("R1.1 : couche %d, attendu 0 (F.Cu)", c.Footprint.Pads[0].Layer)
			}
		}
	}
}
