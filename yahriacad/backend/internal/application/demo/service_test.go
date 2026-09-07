package demo

import (
	"context"
	"testing"

	doctorapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/doctor"
	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	verificationapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/verification"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

// routeAllAI is a mock RL engine: every net routed with one straight
// track between the two first pads, model "loaded" on cpu.
type routeAllAI struct{}

func (routeAllAI) Health(ctx context.Context) error { return nil }

func (routeAllAI) EngineInfo(ctx context.Context) (layoutapp.EngineInfo, error) {
	return layoutapp.EngineInfo{Status: "ok", Device: "cpu", ModelLoaded: true}, nil
}

func (routeAllAI) ModelInfo(ctx context.Context) (layoutapp.ModelInfo, error) {
	return layoutapp.ModelInfo{Loaded: true, Device: "cpu", Strategy: "rl"}, nil
}

func (routeAllAI) ReloadModel(ctx context.Context, checkpointPath string) (layoutapp.ModelInfo, string, error) {
	return layoutapp.ModelInfo{Loaded: true, Device: "cpu", Strategy: "rl"}, "modèle rechargé", nil
}

func (routeAllAI) PlanPlacement(ctx context.Context, b *domainlayout.Board,
	comps []domainschematic.Component, strategy string) ([]domainlayout.PlacedComponent, error) {
	return nil, layoutapp.ErrAIUnreachable
}

func (routeAllAI) RouteBoard(ctx context.Context, b *domainlayout.Board,
	nets []domainschematic.Net, cs *domainconstraints.ConstraintSet, strategy string,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	outcome := &layoutapp.RouteOutcome{Strategy: "rl", Nets: []layoutapp.NetRoute{}}
	for _, net := range nets {
		var pads []domainlayout.TrackPoint
		for i := range b.Components {
			for _, pad := range b.Components[i].Footprint.Pads {
				if pad.Net == net.Name {
					pos := b.Components[i].PadAbsolutePosition(pad)
					pads = append(pads, domainlayout.TrackPoint{X: pos.X, Y: pos.Y})
				}
			}
		}
		nr := layoutapp.NetRoute{Net: net.Name, Completed: len(pads) >= 2,
			Tracks: []domainlayout.Track{{
				Net: net.Name, Layer: 0, Width: 0.3, Points: pads,
			}}}
		outcome.Nets = append(outcome.Nets, nr)
	}
	return outcome, nil
}

func (routeAllAI) OptimizeRoutes(ctx context.Context, b *domainlayout.Board,
	outcome *layoutapp.RouteOutcome, cs *domainconstraints.ConstraintSet,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	return outcome, nil
}

func TestNightmareFullHealingStory(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()
	demo := NewService(repo, nil)

	// 1. Génération de la carte cauchemar.
	rep, err := demo.Nightmare(ctx)
	if err != nil {
		t.Fatalf("Nightmare : %v", err)
	}
	if len(rep.Faults) < 4 {
		t.Fatalf("au moins 4 fautes attendues : %d", len(rep.Faults))
	}
	if len(rep.NextSteps) == 0 {
		t.Fatalf("les prochaines étapes doivent être suggérées")
	}
	id := rep.ProjectID

	// 2. Le DRC doit détecter les fautes réelles (largeurs, vias, bord).
	drc := verificationapp.NewDRCChecker(repo)
	before, err := drc.Run(ctx, id)
	if err != nil {
		t.Fatalf("DRC avant : %v", err)
	}
	if before.Passed || len(before.Violations) < 3 {
		t.Fatalf("violations attendues sur la carte cauchemar : %d", len(before.Violations))
	}
	codes := map[string]bool{}
	for _, v := range before.Violations {
		codes[v.Code] = true
	}
	for _, expected := range []string{verificationapp.CodeDRCTrackWidth, verificationapp.CodeDRCDrill, verificationapp.CodeDRCEdgeClearance} {
		if !codes[expected] {
			t.Fatalf("violation %s attendue, codes obtenus : %v", expected, codes)
		}
	}

	// 3. Le Design Doctor (sans moteur IA) rend un verdict sévère.
	doctor := doctorapp.NewDoctorService(repo, drc, nil, nil, nil, nil, nil)
	repDoc, err := doctor.Examine(ctx, id)
	if err != nil {
		t.Fatalf("Doctor 1 : %v", err)
	}
	if repDoc.AI.Reachable {
		t.Fatalf("moteur IA absent : reachable=false attendu")
	}
	if repDoc.Score >= 85 {
		t.Fatalf("score catastrophique attendu sur la carte cauchemar : %.1f", repDoc.Score)
	}

	// 4. L'Auto-Healer répare en un appel.
	autofix := verificationapp.NewAutoFixService(repo, drc, nil)
	fix, err := autofix.Run(ctx, id, false)
	if err != nil {
		t.Fatalf("AutoFix : %v", err)
	}
	if len(fix.Fixes) == 0 {
		t.Fatalf("des réparations sont attendues")
	}
	if fix.ViolationsAfter >= fix.ViolationsBefore {
		t.Fatalf("le DRC doit s'améliorer : %d → %d", fix.ViolationsBefore, fix.ViolationsAfter)
	}

	// 5. Le Doctor branché au moteur RL (mock) chiffre le routage manquant.
	doctorAI := doctorapp.NewDoctorService(repo, drc, nil, nil, nil, routeAllAI{}, nil)
	repDoc2, err := doctorAI.Examine(ctx, id)
	if err != nil {
		t.Fatalf("Doctor 2 : %v", err)
	}
	if !repDoc2.AI.Reachable {
		t.Fatalf("moteur IA branché : reachable=true attendu")
	}
	if !repDoc2.AI.ModelLoaded || repDoc2.AI.Strategy != "rl" {
		t.Fatalf("modèle RL chargé attendu : %+v", repDoc2.AI)
	}
	if repDoc2.Rehearsal == nil {
		t.Fatalf("répétition de routage attendue (nets N1/N2 non routés)")
	}
	if repDoc2.Rehearsal.Completed == 0 {
		t.Fatalf("le mock route les nets manquants : %d/%d",
			repDoc2.Rehearsal.Completed, repDoc2.Rehearsal.Attempted)
	}

	// 6. La répétition n'a rien appliqué : N1/N2 restent non routés.
	p, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	if len(p.Board().TracksForNet("N1")) != 0 || len(p.Board().TracksForNet("N2")) != 0 {
		t.Fatalf("la répétition ne doit pas modifier la carte")
	}
}

func TestNightmareBoardIsPersistent(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()
	demo := NewService(repo, nil)

	rep, err := demo.Nightmare(ctx)
	if err != nil {
		t.Fatalf("Nightmare : %v", err)
	}
	p, err := repo.FindByID(ctx, rep.ProjectID)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	if p.Board() == nil || len(p.Board().Components) < 5 {
		t.Fatalf("composants attendus sur la carte persistée")
	}
	if p.Schematic() == nil || len(p.Schematic().Nets) < 6 {
		t.Fatalf("6 nets attendus dans le schéma")
	}
	if p.Constraints() != nil {
		t.Fatalf("aucune contrainte personnalisée attendue (faute volontaire)")
	}
}
