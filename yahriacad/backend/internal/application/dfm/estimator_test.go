package dfm

import (
	"context"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

func newDFMFixture(t *testing.T, trackWidth, drill float64) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Costed", "test dfm", 4)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	b, err := domainlayout.NewBoard(60, 40, 4)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8}
	_ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R1", Footprint: fp, X: 30, Y: 20})
	b.AddTrack(domainlayout.Track{Net: "N1", Layer: 0, Width: trackWidth,
		Points: []domainlayout.TrackPoint{{X: 10, Y: 10}, {X: 50, Y: 10}}})
	for i := 0; i < 30; i++ {
		_ = b.AddVia(domainlayout.Via{Net: "N1", X: float64(5 + i), Y: 30,
			FromLayer: 0, ToLayer: 1, Diameter: drill + 0.3, Drill: drill})
	}
	p.SetBoard(b)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestEstimateStandardProcess(t *testing.T) {
	repo, id := newDFMFixture(t, 0.25, 0.3)
	svc := NewService(repo, nil)

	est, err := svc.Estimate(context.Background(), id)
	if err != nil {
		t.Fatalf("Estimate : %v", err)
	}
	if len(est.UnitPrices) != 3 {
		t.Fatalf("3 quantités attendues : %+v", est.UnitPrices)
	}
	for _, u := range est.UnitPrices {
		if u.UnitEUR <= 0 || u.TotalEUR < u.UnitEUR {
			t.Fatalf("prix incohérent : %+v", u)
		}
	}
	// Économies d'échelle : proto >= pilote >= série.
	if est.UnitPrices[0].UnitEUR < est.UnitPrices[1].UnitEUR ||
		est.UnitPrices[1].UnitEUR < est.UnitPrices[2].UnitEUR {
		t.Fatalf("remises de volume attendues : %+v", est.UnitPrices)
	}
	if est.YieldPct < 35 || est.YieldPct > 99.9 {
		t.Fatalf("rendement hors bornes : %v", est.YieldPct)
	}
	if est.Layers != 4 || est.ViaCount != 30 || est.Components != 1 {
		t.Fatalf("métriques inattendues : %+v", est)
	}
	if len(est.RiskFlags) != 0 {
		t.Fatalf("aucun risque attendu en process standard : %+v", est.RiskFlags)
	}
}

func TestEstimateFlagsFineGeometry(t *testing.T) {
	repo, id := newDFMFixture(t, 0.1, 0.2)
	svc := NewService(repo, nil)

	est, err := svc.Estimate(context.Background(), id)
	if err != nil {
		t.Fatalf("Estimate : %v", err)
	}
	codes := map[string]bool{}
	for _, rf := range est.RiskFlags {
		codes[rf.Code] = true
	}
	if !codes["fine_track"] || !codes["laser_drill"] {
		t.Fatalf("risques fine_track + laser_drill attendus : %+v", est.RiskFlags)
	}
	if len(est.Surcharges) < 2 {
		t.Fatalf("surtaxes détaillées attendues : %+v", est.Surcharges)
	}
	if est.DefectRisk == "faible" {
		t.Fatalf("géométrie agressive : risque %q inattendu", est.DefectRisk)
	}
}
