package arena

import (
	"context"
	"errors"
	"strings"
	"testing"

	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

// fakeAIStub is a controllable AIEngine stub: it reports whether an RL
// checkpoint is loaded and routes every requested net with one straight
// 10 mm track (always completed).
type fakeAIStub struct {
	loaded bool
}

func (f *fakeAIStub) EngineInfo(ctx context.Context) (layoutapp.EngineInfo, error) {
	return layoutapp.EngineInfo{Status: "ok", Device: "cpu", ModelLoaded: f.loaded, Version: "0.1.0"}, nil
}

func (f *fakeAIStub) RouteBoard(ctx context.Context, b *domainlayout.Board, nets []domainschematic.Net,
	cs *domainconstraints.ConstraintSet, strategy string,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {

	outcome := &layoutapp.RouteOutcome{Strategy: strategy, Nets: []layoutapp.NetRoute{}}
	for _, n := range nets {
		track := domainlayout.Track{
			Net: n.Name, Layer: 0, Width: 0.25,
			Points: []domainlayout.TrackPoint{{X: 0, Y: 0}, {X: 10, Y: 0}},
		}
		outcome.Nets = append(outcome.Nets, layoutapp.NetRoute{
			Net: n.Name, Tracks: []domainlayout.Track{track},
			LengthMM: 10, Completed: true,
		})
	}
	return outcome, nil
}

// newBenchmarkProject persists a project carrying the shared test board +
// schematic so the benchmark use case can load it.
func newBenchmarkProject(t *testing.T, ctx context.Context, repo domainproject.Repository) *domainproject.Project {
	t.Helper()
	board, sch := testBoardAndSchematic(t)
	p, err := domainproject.New("benchmark-test", "carte de test", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	p.SetBoard(board)
	p.SetSchematic(sch)
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return p
}

// TestBenchmarkRequiresModelLoaded : sans checkpoint RL chargé, le
// benchmark est refusé avec ErrModelNotLoaded (jamais un duel A* contre A*).
func TestBenchmarkRequiresModelLoaded(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()
	svc := NewArenaService(repo, nil)
	p := newBenchmarkProject(t, ctx, repo)

	svc.AttachAI(&fakeAIStub{loaded: false})
	_, err := svc.Benchmark(ctx, p.ID(), nil)
	if !errors.Is(err, ErrModelNotLoaded) {
		t.Fatalf("ErrModelNotLoaded attendu, obtenu : %v", err)
	}
}

// TestBenchmarkRunsAstarVsRL : le benchmark route le même carnet avec les
// deux moteurs, complète les cartes et met à jour l'ELO astar/rl.
func TestBenchmarkRunsAstarVsRL(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()
	svc := NewArenaService(repo, nil)
	p := newBenchmarkProject(t, ctx, repo)

	svc.AttachAI(&fakeAIStub{loaded: true})
	rep, err := svc.Benchmark(ctx, p.ID(), nil)
	if err != nil {
		t.Fatalf("Benchmark : %v", err)
	}

	if rep.Astar.Completed != 2 {
		t.Fatalf("astar doit compléter les 2 nets : %+v", rep.Astar)
	}
	if rep.RL.Completed != 2 {
		t.Fatalf("rl doit compléter les 2 nets : %+v", rep.RL)
	}
	if len(rep.Nets) != 2 {
		t.Fatalf("2 nets attendus dans le rapport : %v", rep.Nets)
	}
	if rep.Winner != "astar" && rep.Winner != "rl" && rep.Winner != "draw" {
		t.Fatalf("verdict invalide : %q", rep.Winner)
	}
	if len(rep.Log) < 5 {
		t.Fatalf("récit trop court : %v", rep.Log)
	}
	if rep.Elo[0].Strategy != "astar" || rep.Elo[1].Strategy != "rl" {
		t.Fatalf("standings ELO attendus [astar, rl] : %+v", rep.Elo)
	}
	// L'ELO doit refléter le match.
	lb := svc.Leaderboard()
	astarMatches, rlMatches := -1, -1
	for _, st := range lb {
		switch st.Strategy {
		case "astar":
			astarMatches = st.Matches
		case "rl":
			rlMatches = st.Matches
		}
	}
	if astarMatches != 1 || rlMatches != 1 {
		t.Fatalf("ELO non mis à jour (astar=%d, rl=%d) : %+v", astarMatches, rlMatches, lb)
	}
}

// TestBenchmarkWithoutAI : sans moteur attaché, erreur explicite.
func TestBenchmarkWithoutAI(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()
	svc := NewArenaService(repo, nil)
	p := newBenchmarkProject(t, ctx, repo)

	_, err := svc.Benchmark(ctx, p.ID(), nil)
	if err == nil || !strings.Contains(err.Error(), "moteur IA non attaché") {
		t.Fatalf("erreur 'moteur IA non attaché' attendue : %v", err)
	}
}
