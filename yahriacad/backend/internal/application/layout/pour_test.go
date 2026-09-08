package layoutapp

import (
	"context"
	"math"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

func newPourFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Plans", "test pours", 4)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	b, err := domainlayout.NewBoard(60, 50, 4)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8,
		Pads: []domainlayout.Pad{
			{Name: "1", X: -0.8, Y: 0, Width: 0.6, Height: 0.6, Layer: 0, Net: "N1"},
			{Name: "2", X: 0.8, Y: 0, Width: 0.6, Height: 0.6, Layer: 0, Net: "N2"},
		}}
	if err := b.AddComponent(domainlayout.PlacedComponent{Ref: "R1", Footprint: fp, X: 20, Y: 20}); err != nil {
		t.Fatalf("AddComponent : %v", err)
	}
	b.AddTrack(domainlayout.Track{Net: "N1", Layer: 0, Width: 0.3,
		Points: []domainlayout.TrackPoint{{X: 20, Y: 20}, {X: 45, Y: 20}}})
	p.SetBoard(b)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestPourGenerateDefaults(t *testing.T) {
	ctx := context.Background()
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	rep, err := svc.Generate(ctx, id, PourGenerateRequest{})
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}

	// Carte 4 couches : les couches cibles par défaut excluent F.Cu.
	if len(rep.Pours) != 3 {
		t.Fatalf("3 pours attendues (In1, In2, B.Cu) : %d", len(rep.Pours))
	}
	for _, pr := range rep.Pours {
		if pr.Layer == 0 {
			t.Fatalf("F.Cu ne doit pas recevoir de pour par défaut")
		}
		if pr.AreaMM2 <= 0 || pr.FillPct <= 0 || pr.FillPct > 100 {
			t.Fatalf("statistiques invalides : %+v", pr)
		}
		if pr.FillPct < 95 {
			t.Fatalf("carte quasi vide : remplissage %.1f%% attendu > 95%%", pr.FillPct)
		}
	}
	if rep.Net != "GND" {
		t.Fatalf("net par défaut GND attendu : %q", rep.Net)
	}
	if rep.Replaced != 0 {
		t.Fatalf("aucune pour remplacée attendue : %d", rep.Replaced)
	}

	// Persistance : rechargement du projet.
	p, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID : %v", err)
	}
	if got := len(p.Board().Pours); got != 3 {
		t.Fatalf("3 pours persistées attendues : %d", got)
	}
	if !p.Board().Pours[0].IsGround {
		t.Fatalf("le pour GND doit être marqué plan de masse")
	}
}

func TestPourClearanceSubtractsForeignCopper(t *testing.T) {
	ctx := context.Background()
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	base, err := svc.Generate(ctx, id, PourGenerateRequest{Layers: []int{3}})
	if err != nil {
		t.Fatalf("Generate base : %v", err)
	}

	// Ajout d'une grosse piste étrangère sur B.Cu (couche 3).
	p, _ := repo.FindByID(ctx, id)
	p.Board().AddTrack(domainlayout.Track{Net: "N1", Layer: 3, Width: 2.0,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 25}, {X: 55, Y: 25}}})
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update : %v", err)
	}

	busy, err := svc.Generate(ctx, id, PourGenerateRequest{Layers: []int{3}})
	if err != nil {
		t.Fatalf("Generate busy : %v", err)
	}

	free := base.Pours[0].FillPct
	loaded := busy.Pours[0].FillPct
	if loaded >= free {
		t.Fatalf("le remplissage doit chuter avec une piste étrangère : %.1f%% vs %.1f%%", loaded, free)
	}
	// Piste de 50 mm × 2 mm dans un plan de ~57×47 mm ≈ 15 % de perte max.
	if free-loaded > 20 {
		t.Fatalf("perte de remplissage irréaliste : %.1f points", free-loaded)
	}
}

func TestPourStitchAddsViasBetweenPlaneLayers(t *testing.T) {
	ctx := context.Background()
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	rep, err := svc.Generate(ctx, id, PourGenerateRequest{
		Layers: []int{1, 2}, Stitch: true, StitchGridMM: 4,
	})
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	if rep.StitchingVias == 0 {
		t.Fatalf("des vias de couture sont attendus entre les couches 1 et 2")
	}

	p, _ := repo.FindByID(ctx, id)
	for _, v := range p.Board().Vias {
		if v.Net != "GND" {
			t.Fatalf("via %v inattendu (net %q)", v, v.Net)
		}
		if (v.FromLayer != 1 || v.ToLayer != 2) && (v.FromLayer != 2 || v.ToLayer != 1) {
			t.Fatalf("via de couture hors paire 1-2 : %+v", v)
		}
		// Marge de bord respectée.
		if v.X < 0.5 || v.Y < 0.5 || v.X > 59.5 || v.Y > 49.5 {
			t.Fatalf("via trop près du bord : %+v", v)
		}
	}
}

func TestPourGenerateReplacesExistingPours(t *testing.T) {
	ctx := context.Background()
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	if _, err := svc.Generate(ctx, id, PourGenerateRequest{Layers: []int{2}}); err != nil {
		t.Fatalf("Generate 1 : %v", err)
	}
	rep, err := svc.Generate(ctx, id, PourGenerateRequest{Layers: []int{2}})
	if err != nil {
		t.Fatalf("Generate 2 : %v", err)
	}
	if rep.Replaced != 1 {
		t.Fatalf("1 pour remplacée attendue : %d", rep.Replaced)
	}
	pours, err := svc.List(ctx, id)
	if err != nil {
		t.Fatalf("List : %v", err)
	}
	if len(pours) != 1 {
		t.Fatalf("1 pour en stock attendue : %d", len(pours))
	}
}

func TestPourDelete(t *testing.T) {
	ctx := context.Background()
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	if _, err := svc.Generate(ctx, id, PourGenerateRequest{}); err != nil {
		t.Fatalf("Generate : %v", err)
	}
	pours, _ := svc.List(ctx, id)
	if err := svc.Delete(ctx, id, pours[0].ID); err != nil {
		t.Fatalf("Delete : %v", err)
	}
	remaining, _ := svc.List(ctx, id)
	if len(remaining) != len(pours)-1 {
		t.Fatalf("suppression non appliquée : %d → %d", len(pours), len(remaining))
	}

	removed, err := svc.DeleteForNet(ctx, id, "")
	if err != nil {
		t.Fatalf("DeleteForNet : %v", err)
	}
	if removed != len(remaining) {
		t.Fatalf("suppression totale attendue : %d ≠ %d", removed, len(remaining))
	}
}

func TestPourRejectsInvalidLayers(t *testing.T) {
	repo, id := newPourFixture(t)
	svc := NewPourService(repo, nil)

	if _, err := svc.Generate(context.Background(), id, PourGenerateRequest{Layers: []int{9}}); err == nil {
		t.Fatalf("couche hors stack doit être rejetée")
	}
}

func TestFillFractionEmptyBoardNearlyFull(t *testing.T) {
	b, err := domainlayout.NewBoard(40, 30, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	outline := domainlayout.RectOutline(1, 1, 39, 29)
	fill := fillFraction(outline, b, 1, "GND", 0.3)
	if math.Abs(fill-100) > 2 {
		t.Fatalf("carte vide : remplissage ~100%% attendu, obtenu %.1f%%", fill)
	}
}

func TestNetsForProjectHonoursSchematicClasses(t *testing.T) {
	p, err := domainproject.New("Classes", "net classes", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	sch := domainschematic.New("sch")
	_ = sch.AddNet(domainschematic.Net{Name: "VCC", Class: domainschematic.ClassPower})
	_ = sch.AddNet(domainschematic.Net{Name: "USB_D+", Class: domainschematic.ClassHighSpeed})
	p.SetSchematic(sch)

	nets := netsForProject(p, []string{"USB_D+"})
	if len(nets) != 1 || nets[0].Class != domainschematic.ClassHighSpeed {
		t.Fatalf("filtre par nom avec classe conservée attendu : %+v", nets)
	}
}
