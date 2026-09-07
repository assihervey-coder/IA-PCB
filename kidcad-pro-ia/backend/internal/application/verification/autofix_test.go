package verificationapp

import (
	"context"
	"testing"

	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
)

// newViolationBoard builds a 2-layer board with one too-thin track, one
// undersized via and one track too close to the edge.
func newViolationBoard(t *testing.T) *domainlayout.Board {
	t.Helper()
	b, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	// Piste trop fine (min 0.2 mm).
	b.AddTrack(domainlayout.Track{Net: "GND", Layer: 0, Width: 0.1,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 5}, {X: 20, Y: 5}}})
	// Via sous-dimensionné (min 0.6/0.3, anneau 0.15).
	if err := b.AddVia(domainlayout.Via{Net: "VCC", X: 30, Y: 30,
		FromLayer: 0, ToLayer: 1, Diameter: 0.4, Drill: 0.2}); err != nil {
		t.Fatalf("AddVia : %v", err)
	}
	// Piste trop proche du bord gauche (x = 0.1 < 0.5 + 0.15).
	b.AddTrack(domainlayout.Track{Net: "SIG", Layer: 0, Width: 0.3,
		Points: []domainlayout.TrackPoint{{X: 0.1, Y: 10}, {X: 10, Y: 10}}})
	return b
}

// newAutoFixFixture persists a project with the violation board.
func newAutoFixFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("HealMe", "test autofix", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	p.SetBoard(newViolationBoard(t))
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestAutoFixDryRunLeavesBoardUntouched(t *testing.T) {
	repo, id := newAutoFixFixture(t)
	drc := NewDRCChecker(repo)
	svc := NewAutoFixService(repo, drc, nil)

	res, err := svc.Run(context.Background(), id, true)
	if err != nil {
		t.Fatalf("Run dry-run : %v", err)
	}
	if res.PassedBefore {
		t.Fatal("le DRC doit échouer avant réparation")
	}
	if len(res.Fixes) == 0 {
		t.Fatalf("des correctifs doivent être proposés : %+v", res)
	}
	if res.BoardChanged {
		t.Fatal("dry-run ne doit pas signaler de modification persistée")
	}

	// La carte persistée ne doit PAS avoir été modifiée.
	p, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	if p.Board().Tracks[0].Width != 0.1 {
		t.Fatalf("dry-run a muté le dépôt : largeur = %v", p.Board().Tracks[0].Width)
	}
	if p.Board().Vias[0].Diameter != 0.4 {
		t.Fatalf("dry-run a muté le dépôt : via = %v", p.Board().Vias[0].Diameter)
	}
}

func TestAutoFixHealsAllFamilies(t *testing.T) {
	repo, id := newAutoFixFixture(t)
	drc := NewDRCChecker(repo)
	svc := NewAutoFixService(repo, drc, nil)

	res, err := svc.Run(context.Background(), id, false)
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if !res.BoardChanged {
		t.Fatal("la carte doit être modifiée")
	}
	if res.ViolationsAfter != 0 {
		t.Fatalf("DRC doit être conforme après réparation, reste %d : %v",
			res.ViolationsAfter, res.Remaining)
	}
	if res.Fixed != res.ViolationsBefore {
		t.Fatalf("fixed=%d, before=%d", res.Fixed, res.ViolationsBefore)
	}

	// Vérifications physiques sur la carte persistée.
	p, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	if w := p.Board().TracksForNet("GND")[0].Width; w < 0.2 {
		t.Fatalf("piste GND non élargie : %v", w)
	}
	via := p.Board().ViasForNet("VCC")[0]
	if via.Diameter < 0.6 || via.Drill < 0.3 || via.AnnularRing()+1e-9 < 0.15 {
		t.Fatalf("via non réparé : %+v (anneau %.3f)", via, via.AnnularRing())
	}
	sig := p.Board().TracksForNet("SIG")[0]
	if sig.Points[0].X < 0.65 {
		t.Fatalf("piste SIG toujours trop proche du bord : x=%.3f", sig.Points[0].X)
	}

	// Deuxième passage : carte déjà saine.
	again, err := svc.Run(context.Background(), id, false)
	if err != nil {
		t.Fatalf("Run #2 : %v", err)
	}
	if !again.PassedBefore || !again.PassedAfter || len(again.Fixes) != 0 {
		t.Fatalf("carte déjà saine : %+v", again)
	}
}

func TestCloneBoardForFixIsDeep(t *testing.T) {
	b := newViolationBoard(t)
	clone := cloneBoardForFix(b)
	clone.Tracks[0].Width = 42
	clone.Vias[0].Diameter = 42
	clone.Tracks[1].Points[0].X = 42
	if b.Tracks[0].Width == 42 || b.Vias[0].Diameter == 42 || b.Tracks[1].Points[0].X == 42 {
		t.Fatal("le clone partage des structures avec l'original")
	}
}
