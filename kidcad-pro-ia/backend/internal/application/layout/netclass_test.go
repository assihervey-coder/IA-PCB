package layoutapp

import (
	"context"
	"testing"

	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
)

func newNetClassFixture(t *testing.T) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("Classes", "test net classes", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	sch := domainschematic.New("sch")
	_ = sch.AddNet(domainschematic.Net{
		Name: "VCC", Class: domainschematic.ClassPower,
		Connections: []domainschematic.PinRef{{ComponentRef: "U1", PinNumber: "1"}},
	})
	_ = sch.AddNet(domainschematic.Net{
		Name: "USB_D+", Class: domainschematic.ClassHighSpeed,
		Connections: []domainschematic.PinRef{{ComponentRef: "U1", PinNumber: "2"}},
	})
	_ = sch.AddNet(domainschematic.Net{Name: "N1", Class: domainschematic.ClassDefault})
	p.SetSchematic(sch)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestNetClassOverview(t *testing.T) {
	ctx := context.Background()
	repo, id := newNetClassFixture(t)
	svc := NewNetClassService(repo)

	ov, err := svc.Overview(ctx, id)
	if err != nil {
		t.Fatalf("Overview : %v", err)
	}
	if len(ov.Nets) != 3 {
		t.Fatalf("3 nets attendus : %d", len(ov.Nets))
	}
	if len(ov.KnownClasses) != 4 {
		t.Fatalf("4 classes connues attendues : %v", ov.KnownClasses)
	}
	// Les valeurs effectives retombent sur les règles globales par défaut.
	for _, n := range ov.Nets {
		if n.Effective.MinTrackWidthMM <= 0 || n.Effective.MinClearanceMM <= 0 {
			t.Fatalf("valeurs effectives attendues pour %s : %+v", n.Name, n.Effective)
		}
	}
}

func TestNetClassAssignAndRules(t *testing.T) {
	ctx := context.Background()
	repo, id := newNetClassFixture(t)
	svc := NewNetClassService(repo)

	// Re-classement de N1 en "power".
	ov, err := svc.Assign(ctx, id, "N1", string(domainschematic.ClassPower))
	if err != nil {
		t.Fatalf("Assign : %v", err)
	}
	if ov.Class != "power" {
		t.Fatalf("classe power attendue : %q", ov.Class)
	}

	// Règles propres à la classe power.
	rules, err := svc.SetRules(ctx, id, "power", ClassRulesRequest{
		MinTrackWidthMM: 0.5, MinClearanceMM: 0.3, MinViaDiameterMM: 0.8,
	})
	if err != nil {
		t.Fatalf("SetRules : %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("3 règles attendues : %d", len(rules))
	}

	// Les valeurs effectives de VCC (power) reflètent les nouvelles règles.
	after, err := svc.Overview(ctx, id)
	if err != nil {
		t.Fatalf("Overview 2 : %v", err)
	}
	for _, n := range after.Nets {
		if n.Class == "power" {
			if n.Effective.MinTrackWidthMM != 0.5 || n.Effective.MinClearanceMM != 0.3 {
				t.Fatalf("règles power non appliquées : %+v", n.Effective)
			}
		}
	}
	// Les autres classes conservent les valeurs globales.
	for _, n := range after.Nets {
		if n.Class == "default" && n.Effective.MinTrackWidthMM >= 0.5 {
			t.Fatalf("la classe default ne doit pas hériter des règles power : %+v", n.Effective)
		}
	}

	// Persistance du classement.
	p, _ := repo.FindByID(ctx, id)
	for _, n := range p.Schematic().Nets {
		if n.Name == "N1" && n.Class != domainschematic.ClassPower {
			t.Fatalf("classement non persisté : %+v", n)
		}
	}
}

func TestNetClassRejectsUnknownClass(t *testing.T) {
	repo, id := newNetClassFixture(t)
	svc := NewNetClassService(repo)

	if _, err := svc.Assign(context.Background(), id, "N1", "ultra"); err == nil {
		t.Fatalf("classe inconnue doit être rejetée")
	}
	if _, err := svc.SetRules(context.Background(), id, "ultra", ClassRulesRequest{MinTrackWidthMM: 0.4}); err == nil {
		t.Fatalf("règles sur classe inconnue doivent être rejetées")
	}
}

func TestSetClassRuleUpsertIsIdempotent(t *testing.T) {
	cs := domainconstraints.NewDefault()
	if _, err := cs.SetClassRule(domainconstraints.RuleMinTrackWidth, "power", 0.5); err != nil {
		t.Fatalf("SetClassRule 1 : %v", err)
	}
	if _, err := cs.SetClassRule(domainconstraints.RuleMinTrackWidth, "power", 0.8); err != nil {
		t.Fatalf("SetClassRule 2 : %v", err)
	}
	if got := len(cs.ClassRules("power")); got != 1 {
		t.Fatalf("upsert attendu (1 règle) : %d", got)
	}
	if v, _ := cs.Value(domainconstraints.RuleMinTrackWidth, "power", domainconstraints.AnyLayer); v != 0.8 {
		t.Fatalf("valeur mise à jour attendue : %.2f", v)
	}
	if classes := cs.Classes(); len(classes) != 1 || classes[0] != "power" {
		t.Fatalf("classes attendues [power] : %v", classes)
	}
}
