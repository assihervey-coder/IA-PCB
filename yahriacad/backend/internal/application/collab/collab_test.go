package collab

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

// newFixture builds a repo with one project (board 60x40, 2 couches,
// composants R1/R2, composant U1).
func newFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Collab", "test crdt", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	b, err := domainlayout.NewBoard(60, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "R0603", BodyWidthMM: 1.6, BodyHeightMM: 0.8}
	for _, ref := range []string{"R1", "R2"} {
		if err := b.AddComponent(domainlayout.PlacedComponent{Ref: ref, Footprint: fp, X: 10, Y: 10}); err != nil {
			t.Fatalf("AddComponent %s : %v", ref, err)
		}
	}
	p.SetBoard(b)
	p.SetSchematic(domainschematic.New("sch"))
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestApplyMovesAndTracks(t *testing.T) {
	ctx := context.Background()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, "", nil)

	res, err := svc.Apply(ctx, id, ApplyRequest{
		Actor: "alice",
		Ops: []OpRequest{
			{ClientID: "op1", Kind: OpComponentMove, Target: "R1",
				Payload: Payload{X: 20, Y: 25}},
			{ClientID: "op2", Kind: OpTrackAdd, Target: "N1",
				Payload: Payload{Net: "N1", Layer: 0, Width: 0.25,
					Points: []Point{{X: 20, Y: 25}, {X: 40, Y: 25}}}},
		},
	})
	if err != nil {
		t.Fatalf("Apply : %v", err)
	}
	if res.Applied != 2 || len(res.Rejected) != 0 {
		t.Fatalf("2 ops attendues : %+v", res)
	}

	p, _ := repo.FindByID(ctx, id)
	r1, _ := p.Board().ComponentByRef("R1")
	if r1.X != 20 || r1.Y != 25 {
		t.Fatalf("R1 non déplacé : (%.1f, %.1f)", r1.X, r1.Y)
	}
	if len(p.Board().TracksForNet("N1")) != 1 {
		t.Fatalf("piste N1 attendue")
	}
	if res.Seq != 2 {
		t.Fatalf("seq 2 attendue : %d", res.Seq)
	}
}

func TestLWWConvergenceOutOfOrder(t *testing.T) {
	ctx := context.Background()

	// alice déplace R1 vers A (lamport 5), bob vers B (lamport 5, acteur
	// lexicalement supérieur : bob gagne) — quelle que soit l'ordre
	// d'arrivée, la même carte finale est observée.
	moveA := OpRequest{ClientID: "a1", Lamport: 5, Kind: OpComponentMove, Target: "R1", Payload: Payload{X: 30, Y: 5}}
	moveB := OpRequest{ClientID: "b1", Lamport: 5, Kind: OpComponentMove, Target: "R1", Payload: Payload{X: 30, Y: 8}}
	type step struct {
		actor string
		op    OpRequest
	}
	for _, scenario := range [][]step{
		{{"alice", moveA}, {"bob", moveB}}, // alice d'abord
		{{"bob", moveB}, {"alice", moveA}}, // bob d'abord
	} {
		repo2, id2 := newFixture(t)
		svc2 := NewService(repo2, nil, "", nil)
		for _, st := range scenario {
			if _, err := svc2.Apply(ctx, id2, ApplyRequest{Actor: st.actor, Ops: []OpRequest{st.op}}); err != nil {
				t.Fatalf("Apply %s : %v", st.actor, err)
			}
		}
		p, _ := repo2.FindByID(ctx, id2)
		r1, _ := p.Board().ComponentByRef("R1")
		if r1.Y != 8 {
			t.Fatalf("convergence LWW attendue sur Y=8 (bob > alice) : %.1f (scénario %v)", r1.Y, scenario)
		}
	}
}

func TestUndoRedoCycle(t *testing.T) {
	ctx := context.Background()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, "", nil)

	_, err := svc.Apply(ctx, id, ApplyRequest{
		Actor: "alice",
		Ops: []OpRequest{
			{ClientID: "m1", Kind: OpComponentMove, Target: "R1", Payload: Payload{X: 33, Y: 12}},
		},
	})
	if err != nil {
		t.Fatalf("Apply : %v", err)
	}

	// Undo : R1 revient en (10, 10).
	res, err := svc.Undo(ctx, id, "alice")
	if err != nil {
		t.Fatalf("Undo : %v", err)
	}
	if !res.Undone {
		t.Fatalf("undo attendu : %+v", res)
	}
	p, _ := repo.FindByID(ctx, id)
	r1, _ := p.Board().ComponentByRef("R1")
	if r1.X != 10 || r1.Y != 10 {
		t.Fatalf("position d'origine attendue après undo : (%.1f, %.1f)", r1.X, r1.Y)
	}

	// Redo : R1 repart en (33, 12).
	res, err = svc.Redo(ctx, id, "alice")
	if err != nil {
		t.Fatalf("Redo : %v", err)
	}
	if !res.Undone {
		t.Fatalf("redo attendu : %+v", res)
	}
	p, _ = repo.FindByID(ctx, id)
	r1, _ = p.Board().ComponentByRef("R1")
	if r1.X != 33 || r1.Y != 12 {
		t.Fatalf("position modifiée attendue après redo : (%.1f, %.1f)", r1.X, r1.Y)
	}

	// Undo d'un autre acteur : rien à annuler.
	res, err = svc.Undo(ctx, id, "bob")
	if err != nil {
		t.Fatalf("Undo bob : %v", err)
	}
	if res.Undone {
		t.Fatalf("bob ne doit rien pouvoir annuler")
	}
}

func TestUndoTrackRemoveRestoresTracks(t *testing.T) {
	ctx := context.Background()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, "", nil)

	// Une piste ajoutée puis annulée puis... on teste surtout track.remove
	// suivi de son undo (restauration multi-pistes).
	b := mustBoard(t, repo, id)
	b.AddTrack(domainlayout.Track{Net: "N7", Layer: 1, Width: 0.3,
		Points: []domainlayout.TrackPoint{{X: 1, Y: 1}, {X: 5, Y: 1}}})
	b.AddTrack(domainlayout.Track{Net: "N7", Layer: 0, Width: 0.3,
		Points: []domainlayout.TrackPoint{{X: 1, Y: 2}, {X: 5, Y: 2}}})

	if _, err := svc.Apply(ctx, id, ApplyRequest{
		Actor: "carol",
		Ops:   []OpRequest{{ClientID: "rm1", Kind: OpTrackRemove, Payload: Payload{Net: "N7"}}},
	}); err != nil {
		t.Fatalf("Apply : %v", err)
	}
	if got := len(mustBoard(t, repo, id).TracksForNet("N7")); got != 0 {
		t.Fatalf("pistes N7 supprimées attendues : %d restantes", got)
	}

	if _, err := svc.Undo(ctx, id, "carol"); err != nil {
		t.Fatalf("Undo : %v", err)
	}
	restored := mustBoard(t, repo, id).TracksForNet("N7")
	if len(restored) != 2 {
		t.Fatalf("2 pistes restaurées attendues : %d", len(restored))
	}
}

func TestDurableLogReplayAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, dir, nil)

	if _, err := svc.Apply(ctx, id, ApplyRequest{
		Actor: "alice",
		Ops: []OpRequest{
			{ClientID: "m1", Kind: OpComponentMove, Target: "R1", Payload: Payload{X: 22, Y: 18}},
			{ClientID: "t1", Kind: OpTrackAdd, Payload: Payload{Net: "N2", Layer: 0, Width: 0.25,
				Points: []Point{{X: 22, Y: 18}, {X: 50, Y: 18}}}},
		},
	}); err != nil {
		t.Fatalf("Apply : %v", err)
	}

	// Le journal existe et contient les ops.
	journal := filepath.Join(dir, "collab", id+".jsonl")
	data, err := os.ReadFile(journal)
	if err != nil || len(data) == 0 {
		t.Fatalf("journal durable attendu : %v", err)
	}

	// Redémarrage : un nouveau service rejoue le journal (bookkeeping seul).
	svc2 := NewService(repo, nil, dir, nil)
	st, err := svc2.State(ctx, id, -1)
	if err != nil {
		t.Fatalf("State après redémarrage : %v", err)
	}
	if st.Seq != 2 {
		t.Fatalf("seq 2 attendue après replay : %d", st.Seq)
	}
	if st.Clock["alice"] == 0 {
		t.Fatalf("horloge d'alice non reconstruite")
	}

	// L'historique undo survit au redémarrage : alice annule d'abord son
	// track.add (dernière op), puis son move.
	res, err := svc2.Undo(ctx, id, "alice")
	if err != nil {
		t.Fatalf("Undo après replay : %v", err)
	}
	if !res.Undone {
		t.Fatalf("undo attendu après redémarrage : %+v", res)
	}
	p, _ := repo.FindByID(ctx, id)
	if got := len(p.Board().TracksForNet("N2")); got != 0 {
		t.Fatalf("track.add annulé attendu : %d pistes restantes", got)
	}
	if res, err = svc2.Undo(ctx, id, "alice"); err != nil || !res.Undone {
		t.Fatalf("second undo attendu : %v %+v", err, res)
	}
	p, _ = repo.FindByID(ctx, id)
	r1, _ := p.Board().ComponentByRef("R1")
	if r1.X != 10 || r1.Y != 10 {
		t.Fatalf("undo après redémarrage : position d'origine attendue, obtenu (%.1f, %.1f)", r1.X, r1.Y)
	}
}

func TestDuplicateOpsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, "", nil)

	req := ApplyRequest{
		Actor: "alice",
		Ops: []OpRequest{{ClientID: "same", Kind: OpTrackAdd, Payload: Payload{Net: "N9", Layer: 0, Width: 0.25,
			Points: []Point{{X: 1, Y: 1}, {X: 2, Y: 2}}}}},
	}
	if _, err := svc.Apply(ctx, id, req); err != nil {
		t.Fatalf("Apply 1 : %v", err)
	}
	res, err := svc.Apply(ctx, id, req)
	if err != nil {
		t.Fatalf("Apply 2 : %v", err)
	}
	if res.Applied != 0 || len(res.Rejected) != 1 {
		t.Fatalf("retransmission attendue rejetée : %+v", res)
	}
	if got := len(mustBoard(t, repo, id).TracksForNet("N9")); got != 1 {
		t.Fatalf("doublon interdit : %d pistes", got)
	}
}

func TestStateSinceCatchUp(t *testing.T) {
	ctx := context.Background()
	repo, id := newFixture(t)
	svc := NewService(repo, nil, "", nil)

	if _, err := svc.Apply(ctx, id, ApplyRequest{
		Actor: "alice",
		Ops: []OpRequest{
			{ClientID: "o1", Kind: OpComponentMove, Target: "R1", Payload: Payload{X: 11, Y: 11}},
			{ClientID: "o2", Kind: OpComponentMove, Target: "R2", Payload: Payload{X: 12, Y: 12}},
		},
	}); err != nil {
		t.Fatalf("Apply : %v", err)
	}

	st, err := svc.State(ctx, id, 1)
	if err != nil {
		t.Fatalf("State : %v", err)
	}
	if len(st.Ops) != 1 || st.Ops[0].Op.ID != "o2" {
		t.Fatalf("rattrapage depuis seq 1 : op o2 attendue, obtenu %+v", st.Ops)
	}
}

func mustBoard(t *testing.T, repo domainproject.Repository, id string) *domainlayout.Board {
	t.Helper()
	p, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	return p.Board()
}
