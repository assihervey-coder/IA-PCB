package stats

import (
	"context"
	"testing"

	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
)

func newStatsFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Mesuré", "test stats", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}

	sch := domainschematic.New("stats-test")
	_ = sch.AddComponent(domainschematic.Component{Ref: "R1", Value: "10k", Footprint: "0603"})
	_ = sch.AddComponent(domainschematic.Component{Ref: "R2", Value: "10k", Footprint: "0603"})
	_ = sch.AddNet(domainschematic.Net{Name: "N1", Class: domainschematic.ClassDefault})
	_ = sch.AddNet(domainschematic.Net{Name: "N2", Class: domainschematic.ClassPower})
	p.SetSchematic(sch)

	b, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8,
		Pads: []domainlayout.Pad{
			{Name: "1", X: -0.8, Y: 0, Width: 0.6, Height: 0.6, Net: "N1"},
			{Name: "2", X: 0.8, Y: 0, Width: 0.6, Height: 0.6, Net: "N2"},
		}}
	_ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R1", Footprint: fp, X: 10, Y: 10})
	_ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R2", Footprint: fp, X: 40, Y: 30})
	// N1 : 20 mm sur F.Cu + 5 mm sur B.Cu ; N2 non routé.
	b.AddTrack(domainlayout.Track{Net: "N1", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 10, Y: 10}, {X: 30, Y: 10}}})
	b.AddTrack(domainlayout.Track{Net: "N1", Layer: 1, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 30, Y: 10}, {X: 35, Y: 10}}})
	_ = b.AddVia(domainlayout.Via{Net: "N1", X: 30, Y: 10, FromLayer: 0, ToLayer: 1,
		Diameter: 0.6, Drill: 0.3})
	p.SetBoard(b)

	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestReportAggregates(t *testing.T) {
	repo, id := newStatsFixture(t)
	svc := NewService(repo, nil)

	rep, err := svc.Report(context.Background(), id)
	if err != nil {
		t.Fatalf("Report : %v", err)
	}
	if rep.Components != 2 || rep.Pads != 4 {
		t.Fatalf("composants/pads inattendus : %d/%d", rep.Components, rep.Pads)
	}
	if rep.Nets != 2 || rep.TrackedNets != 1 {
		t.Fatalf("nets inattendus : %d total / %d routés", rep.Nets, rep.TrackedNets)
	}
	if rep.Tracks != 2 || rep.Vias != 1 {
		t.Fatalf("cuivre inattendu : %d pistes / %d vias", rep.Tracks, rep.Vias)
	}
	if rep.TotalLengthMM < 24.9 || rep.TotalLengthMM > 25.1 {
		t.Fatalf("longueur totale attendue 25 mm : %v", rep.TotalLengthMM)
	}
	if len(rep.TopNets) != 1 || rep.TopNets[0].Name != "N1" || rep.TopNets[0].Vias != 1 {
		t.Fatalf("top nets inattendu : %+v", rep.TopNets)
	}
	if len(rep.LayerUsage) != 2 {
		t.Fatalf("2 couches actives attendues : %+v", rep.LayerUsage)
	}
	if rep.UtilizationPct <= 0 || rep.UtilizationPct >= 100 {
		t.Fatalf("occupation hors bornes : %v", rep.UtilizationPct)
	}
	if rep.Board.AreaCM2 != 20 {
		t.Fatalf("surface attendue 20 cm² : %v", rep.Board.AreaCM2)
	}
	if len(rep.NetClasses) == 0 {
		t.Fatal("agrégat par classe attendu")
	}
}

func TestReportRequiresBoard(t *testing.T) {
	repo := memory.NewProjectRepository()
	p, _ := domainproject.New("Vide", "", 2)
	_ = repo.Create(context.Background(), p)
	svc := NewService(repo, nil)
	if _, err := svc.Report(context.Background(), p.ID()); err == nil {
		t.Fatal("erreur attendue sans carte")
	}
}
