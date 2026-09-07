package doctor

import (
	"context"
	"testing"

	verificationapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/verification"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
)

func newDoctorFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Audited", "test doctor", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	b, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8}
	_ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R1", Footprint: fp, X: 25, Y: 20})
	_ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R2", Footprint: fp, X: 30, Y: 20})
	p.SetBoard(b)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestExamineProducesSaneReport(t *testing.T) {
	repo, id := newDoctorFixture(t)
	drc := verificationapp.NewDRCChecker(repo)
	erc := verificationapp.NewERCChecker(repo)
	thermal := verificationapp.NewThermalChecker(repo, 25.0)
	si := verificationapp.NewSIChecker(repo)
	svc := NewDoctorService(repo, drc, erc, thermal, si, nil)

	rep, err := svc.Examine(context.Background(), id)
	if err != nil {
		t.Fatalf("Examine : %v", err)
	}
	if len(rep.Axes) != 5 {
		t.Fatalf("5 axes attendus, obtenu %d", len(rep.Axes))
	}
	for _, axe := range rep.Axes {
		if axe.Score < 0 || axe.Score > 100 {
			t.Fatalf("axe %s hors bornes : %v", axe.Axe, axe.Score)
		}
	}
	if rep.Score <= 0 || rep.Score >= 100 {
		t.Fatalf("score global hors bornes : %v", rep.Score)
	}
	if rep.Grade == "" || rep.Verdict == "" {
		t.Fatalf("note/verdict manquants : %s / %s", rep.Grade, rep.Verdict)
	}
	if rep.Metrics.Components != 2 {
		t.Fatalf("2 composants attendus : %+v", rep.Metrics)
	}
	// Carte sans pistes : 100 % des nets non routés → au moins une
	// ordonnance de routage attendue.
	if len(rep.Prescriptions) == 0 {
		t.Fatal("des ordonnances sont attendues pour une carte vierge")
	}
	for i, p := range rep.Prescriptions {
		if i > 0 && rep.Prescriptions[i-1].Priority > p.Priority {
			t.Fatal("ordonnances non triées par priorité")
		}
	}
}

func TestExamineRejectsBoardlessProject(t *testing.T) {
	repo := memory.NewProjectRepository()
	p, _ := domainproject.New("Empty", "", 2)
	_ = repo.Create(context.Background(), p)
	svc := NewDoctorService(repo, nil, nil, nil, nil, nil)
	if _, err := svc.Examine(context.Background(), p.ID()); err == nil {
		t.Fatal("erreur attendue pour un projet sans carte")
	}
}

func TestGradeScale(t *testing.T) {
	cases := map[float64]string{95: "A+", 88: "A", 78: "B", 65: "C", 30: "D"}
	for score, want := range cases {
		if got := gradeOf(score); got != want {
			t.Fatalf("gradeOf(%.0f) = %s, attendu %s", score, got, want)
		}
	}
}
