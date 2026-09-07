package magic

import (
	"strings"
	"testing"

	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// -------------------------------------------------------------- parser ----

func TestParsePlaceNearFR(t *testing.T) {
	got := Parse("place R1 et R2 près de U3")
	if len(got.Actions) != 2 {
		t.Fatalf("attendu 2 actions, obtenu %d (%+v)", len(got.Actions), got.Actions)
	}
	if got.Actions[0].Kind != KindMoveComponent || got.Actions[1].Kind != KindMoveComponent {
		t.Fatalf("mauvais kinds : %+v", got.Actions)
	}
	if got.Actions[0].Params["ref"] != "R1" || got.Actions[1].Params["near"] != "U3" {
		t.Fatalf("mauvais params : %+v", got.Actions)
	}
	if got.Confidence < 0.75 {
		t.Fatalf("confiance trop basse : %.2f", got.Confidence)
	}
	if !strings.Contains(got.Reply, "U3") {
		t.Fatalf("réponse sans ancre : %q", got.Reply)
	}
}

func TestParseTrackWidthUnits(t *testing.T) {
	mm := Parse("mets la largeur de piste à 12 mil sur la classe power")
	if len(mm.Actions) != 1 || mm.Actions[0].Kind != KindSetTrackWidth {
		t.Fatalf("action attendue set_track_width : %+v", mm.Actions)
	}
	w := mm.Actions[0].Params["width_mm"].(float64)
	if w < 0.3048-1e-9 || w > 0.3048+1e-9 {
		t.Fatalf("12 mil = 0.3048 mm, obtenu %v", w)
	}
	if mm.Actions[0].Params["target"] != "power" {
		t.Fatalf("cible power attendue, obtenu %v", mm.Actions[0].Params["target"])
	}

	si := Parse("set track width to 0.3 mm")
	if v := si.Actions[0].Params["width_mm"].(float64); v != 0.3 {
		t.Fatalf("0.3 mm attendu, obtenu %v", v)
	}
}

func TestParseMultiAction(t *testing.T) {
	got := Parse("drc puis exporte gerber et bom")
	if len(got.Actions) != 3 {
		t.Fatalf("attendu 3 actions (drc + gerber + bom), obtenu %d : %+v", len(got.Actions), got.Actions)
	}
	if got.Actions[0].Kind != KindRunDRC {
		t.Fatalf("première action drc attendue : %+v", got.Actions[0])
	}
	kinds := map[Kind]bool{}
	for _, a := range got.Actions {
		kinds[a.Kind] = true
	}
	if !kinds[KindExport] {
		t.Fatalf("export manquant : %+v", got.Actions)
	}
}

func TestParseUnknown(t *testing.T) {
	got := Parse("fais un câlin au microcontrôleur")
	if got.Confidence != 0 || len(got.Actions) != 0 {
		t.Fatalf("confiance 0 attendue : %+v", got)
	}
	if !strings.Contains(got.Reply, "place R1") {
		t.Fatalf("suggestion d'usage attendue : %q", got.Reply)
	}
}

func TestParseComponentWish(t *testing.T) {
	got := Parse("pose deux condensateurs près de U1")
	if len(got.Actions) != 1 || got.Actions[0].Kind != KindPlaceComponent {
		t.Fatalf("place_component attendu : %+v", got.Actions)
	}
	if got.Actions[0].Params["count"].(float64) != 2 {
		t.Fatalf("count=2 attendu : %+v", got.Actions[0].Params)
	}
	if got.Actions[0].Params["component_kind"] != "capacitor" {
		t.Fatalf("kind capacitor attendu : %+v", got.Actions[0].Params)
	}
}

func TestParseRouteStrategies(t *testing.T) {
	got := Parse("route tout en stratégie rapide")
	if len(got.Actions) != 1 || got.Actions[0].Kind != KindRoute {
		t.Fatalf("route attendu : %+v", got.Actions)
	}
	if got.Actions[0].Params["strategy"] != "fast" {
		t.Fatalf("stratégie fast attendue : %+v", got.Actions[0].Params)
	}
	nets := got.Actions[0].Params["nets"].([]string)
	if len(nets) != 1 || nets[0] != "*" {
		t.Fatalf("nets=[*] attendu : %+v", nets)
	}
}

// -------------------------------------------------------------- service ---

func newTestBoard(t *testing.T) *domainlayout.Board {
	t.Helper()
	b, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "SOIC-8", BodyWidthMM: 4, BodyHeightMM: 5, HeightMM: 1.6}
	if err := b.AddComponent(domainlayout.PlacedComponent{Ref: "U1", Footprint: fp, X: 25, Y: 20}); err != nil {
		t.Fatalf("AddComponent U1 : %v", err)
	}
	if err := b.AddComponent(domainlayout.PlacedComponent{Ref: "R7", Footprint: fp, X: 10, Y: 10}); err != nil {
		t.Fatalf("AddComponent R7 : %v", err)
	}
	return b
}

func TestApplyMoveNear(t *testing.T) {
	board := newTestBoard(t)
	svc := &MagicService{}

	a := Parse("déplace C1 près de U1").Actions[0]
	// C1 n'existe pas -> échec propre.
	if _, ok := svc.applyMove(board, a); ok {
		t.Fatal("échec attendu pour référence absente")
	}

	a = Parse("déplace R7 près de U1").Actions[0]
	msg, ok := svc.applyMove(board, a)
	if !ok {
		t.Fatalf("succès attendu : %s", msg)
	}
	r7, _ := board.ComponentByRef("R7")
	if r7.X != 29 || r7.Y != 20 {
		t.Fatalf("R7 attendu à (29,20) : (%.2f, %.2f)", r7.X, r7.Y)
	}
}

func TestApplySetTrackWidth(t *testing.T) {
	cs := domainconstraints.NewDefault()
	board := newTestBoard(t)
	board.AddTrack(domainlayout.Track{Net: "GND", Layer: 0, Width: 0.2,
		Points: []domainlayout.TrackPoint{{X: 0, Y: 0}, {X: 5, Y: 0}}})

	msg, ok := applyWidth(cs, board, "all", 0.3)
	if !ok || !strings.Contains(msg, "1 piste") {
		t.Fatalf("re-largeur attendue : %s", msg)
	}
	if v, exists := cs.Value(domainconstraints.RuleMinTrackWidth, "*", -1); !exists || v != 0.3 {
		t.Fatalf("règle wildcard non mise à jour : %v %v", v, exists)
	}
	if board.Tracks[0].Width != 0.3 {
		t.Fatalf("piste non re-largeée : %v", board.Tracks[0].Width)
	}
}

func TestApplyPlaceKindGeneratesFreeRefs(t *testing.T) {
	board := newTestBoard(t)
	svc := &MagicService{}
	a := Action{Kind: KindPlaceComponent, Params: map[string]any{
		"component_kind": "capacitor", "count": float64(3), "near": "U1", "has_near": true,
	}, Executable: true}
	msgs, ok := svc.applyPlaceKind(board, a)
	if !ok || len(msgs) != 3 {
		t.Fatalf("3 placements attendus : %+v", msgs)
	}
	for _, want := range []string{"C1", "C2", "C3"} {
		if _, ok := board.ComponentByRef(want); !ok {
			t.Fatalf("%s manquant sur la carte", want)
		}
	}
}
