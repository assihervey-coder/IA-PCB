package reader

import (
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// mkTrack construit une piste élémentaire (2 points) pour les tests.
func mkTrack(net string, layer int, width float64, x1, y1, x2, y2 float64) domainlayout.Track {
	return domainlayout.Track{
		Net:   net,
		Layer: layer,
		Width: width,
		Points: []domainlayout.TrackPoint{
			{X: x1, Y: y1},
			{X: x2, Y: y2},
		},
	}
}

func ptsOf(t domainlayout.Track) [][2]float64 {
	out := make([][2]float64, 0, len(t.Points))
	for _, p := range t.Points {
		out = append(out, [2]float64{p.X, p.Y})
	}
	return out
}

func TestMergeTracksCollinearChain(t *testing.T) {
	// A(0,0)->B(1,0)->C(2,0)->D(3,0) : une seule piste, points extrêmes.
	tracks := []domainlayout.Track{
		mkTrack("GND", 0, 0.25, 0, 0, 1, 0),
		mkTrack("GND", 0, 0.25, 1, 0, 2, 0),
		mkTrack("GND", 0, 0.25, 2, 0, 3, 0),
	}
	got := mergeTracks(tracks)
	if len(got) != 1 {
		t.Fatalf("1 piste attendue, obtenue %d", len(got))
	}
	if pts := ptsOf(got[0]); len(pts) != 2 || pts[0] != [2]float64{0, 0} ||
		pts[1] != [2]float64{3, 0} {
		t.Fatalf("polyligne attendue [(0,0) (3,0)], obtenue %v", pts)
	}
}

func TestMergeTracksCornerKept(t *testing.T) {
	// L-shape (0,0)->(2,0)->(2,3) : le coude est conservé.
	tracks := []domainlayout.Track{
		mkTrack("VCC", 0, 0.25, 0, 0, 2, 0),
		mkTrack("VCC", 0, 0.25, 2, 0, 2, 3),
	}
	got := mergeTracks(tracks)
	if len(got) != 1 {
		t.Fatalf("1 piste attendue, obtenue %d", len(got))
	}
	if pts := ptsOf(got[0]); len(pts) != 3 || pts[1] != [2]float64{2, 0} {
		t.Fatalf("coude attendu en (2,0), obtenu %v", pts)
	}
}

func TestMergeTracksJunctionProtected(t *testing.T) {
	// Jonction en T en (2,0) : 3 branches, aucune fusion à travers elle.
	tracks := []domainlayout.Track{
		mkTrack("SIG", 0, 0.25, 0, 0, 2, 0),
		mkTrack("SIG", 0, 0.25, 2, 0, 4, 0),
		mkTrack("SIG", 0, 0.25, 2, 0, 2, 2),
	}
	got := mergeTracks(tracks)
	if len(got) != 3 {
		t.Fatalf("3 pistes attendues (jonction protégée), obtenues %d", len(got))
	}
}

func TestMergeTracksDifferentKeysNeverMerged(t *testing.T) {
	base := [][2]float64{{0, 0}, {1, 0}}
	cases := []domainlayout.Track{
		mkTrack("A", 0, 0.25, 1, 0, 2, 0), // net différent
		mkTrack("X", 1, 0.25, 1, 0, 2, 0), // couche différente
		mkTrack("X", 0, 0.5, 1, 0, 2, 0),  // largeur différente
	}
	for i, cont := range cases {
		tracks := []domainlayout.Track{
			mkTrack("X", 0, 0.25, base[0][0], base[0][1], base[1][0], base[1][1]),
			cont,
		}
		if got := mergeTracks(tracks); len(got) != 2 {
			t.Fatalf("cas %d : aucune fusion attendue, obtenue %d piste(s)", i, len(got)-2+2)
		}
	}
}

func TestMergeTracksDuplicateStaysSeparate(t *testing.T) {
	// Deux segments géométriquement identiques : jamais refermés l'un sur
	// l'autre (le chaînage produirait un aller-retour nul).
	tracks := []domainlayout.Track{
		mkTrack("X", 0, 0.25, 0, 0, 1, 0),
		mkTrack("X", 0, 0.25, 0, 0, 1, 0),
	}
	if got := mergeTracks(tracks); len(got) != 2 {
		t.Fatalf("2 pistes distinctes attendues, obtenues %d", len(got))
	}
}

func TestMergeTracksClosedLoop(t *testing.T) {
	// Anneau 1x1 : une seule piste, point de fermeture répété.
	tracks := []domainlayout.Track{
		mkTrack("X", 0, 0.25, 0, 0, 1, 0),
		mkTrack("X", 0, 0.25, 1, 0, 1, 1),
		mkTrack("X", 0, 0.25, 1, 1, 0, 1),
		mkTrack("X", 0, 0.25, 0, 1, 0, 0),
	}
	got := mergeTracks(tracks)
	if len(got) != 1 {
		t.Fatalf("1 piste (anneau) attendue, obtenue %d", len(got))
	}
	pts := ptsOf(got[0])
	if len(pts) != 5 || pts[0] != pts[4] {
		t.Fatalf("anneau fermé attendu (5 points, extrémités égales), obtenu %v", pts)
	}
}

func TestMergeTracksBacktrackPointKept(t *testing.T) {
	// (0,0)->(1,0)->(0.5,0) : demi-tour réel (pas un doublon), le point
	// intermédiaire est conservé ; les doublons parfaits restent séparés
	// (TestMergeTracksDuplicateStaysSeparate).
	tracks := []domainlayout.Track{
		mkTrack("X", 0, 0.25, 0, 0, 1, 0),
		mkTrack("X", 0, 0.25, 1, 0, 0.5, 0),
	}
	got := mergeTracks(tracks)
	if len(got) != 1 {
		t.Fatalf("1 piste attendue, obtenue %d", len(got))
	}
	if pts := ptsOf(got[0]); len(pts) != 3 || pts[1] != [2]float64{1, 0} {
		t.Fatalf("demi-tour conservé attendu, obtenu %v", pts)
	}
}

func TestImportMergesExportedSegments(t *testing.T) {
	// Intégration lecteur : une suite de segments élémentaires dans un
	// .kicad_pcb est ré-importée en pistes multi-points fusionnées.
	src := `(kicad_pcb
        (version 20241229)
        (layers
                (0 "F.Cu" signal)
                (31 "B.Cu" signal)
        )
        (net 1 "GND")
        (segment (start 0 0) (end 1 0) (width 0.25) (layer "F.Cu") (net 1))
        (segment (start 1 0) (end 2 0) (width 0.25) (layer "F.Cu") (net 1))
        (segment (start 2 0) (end 2 2) (width 0.25) (layer "F.Cu") (net 1))
)`
	res, err := readKiCadPCB([]byte(src), "fusion.kicad_pcb")
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	tracks := res.Board.Tracks
	if len(tracks) != 1 {
		t.Fatalf("1 piste fusionnée attendue, obtenue %d", len(tracks))
	}
	if pts := ptsOf(tracks[0]); len(pts) != 3 || pts[2] != [2]float64{2, 2} {
		t.Fatalf("polyligne [(0,0) (2,0) (2,2)] attendue, obtenue %v", pts)
	}
}
