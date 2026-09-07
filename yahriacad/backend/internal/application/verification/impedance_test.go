package verificationapp

import (
	"context"
	"math"
	"strings"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
)

func newImpedanceBoard(t *testing.T) *domainlayout.Board {
	t.Helper()
	b, err := domainlayout.NewBoard(60, 40, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	// Paire USB routée côte à côte (écart 0.2 mm) sur la couche 0.
	b.AddTrack(domainlayout.Track{Net: "USB_DP", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 10}, {X: 30, Y: 10}}})
	b.AddTrack(domainlayout.Track{Net: "USB_DN", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 10.2}, {X: 30, Y: 10.2}}})
	// Paire Ethernet plus longue sur une branche (skew volontaire).
	b.AddTrack(domainlayout.Track{Net: "ETH_TXP", Layer: 0, Width: 0.2,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 20}, {X: 50, Y: 20}}})
	b.AddTrack(domainlayout.Track{Net: "ETH_TXN", Layer: 0, Width: 0.2,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 20.25}, {X: 44, Y: 20.25}}})
	return b
}

func newImpedanceFixture(t *testing.T, withSchematic bool) (domainproject.Repository, string) {
	t.Helper()
	repo := memory.NewProjectRepository()
	p, err := domainproject.New("ImpedanceMe", "test impédance différentielle", 2)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	p.SetBoard(newImpedanceBoard(t))
	if withSchematic {
		sch := domainschematic.New("imp-test")
		_ = sch.AddNet(domainschematic.Net{Name: "USB_DP", Class: domainschematic.ClassHighSpeed})
		_ = sch.AddNet(domainschematic.Net{Name: "USB_DN", Class: domainschematic.ClassHighSpeed})
		_ = sch.AddNet(domainschematic.Net{Name: "ETH_TXP", Class: domainschematic.ClassSignal})
		_ = sch.AddNet(domainschematic.Net{Name: "ETH_TXN", Class: domainschematic.ClassSignal})
		p.SetSchematic(sch)
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("Create : %v", err)
	}
	return repo, p.ID()
}

func TestPairBaseAndPolarityConventions(t *testing.T) {
	cases := []struct {
		name, base string
		pol        int
	}{
		{"USB_D+", "USB_D", 1},
		{"USB_D-", "USB_D", -1},
		{"USB_DP", "USB_D", 1},
		{"USB_DN", "USB_D", -1},
		{"A_p", "A", 1},
		{"A_n", "A", -1},
		{"TXP", "TX", 1},
		{"TXN", "TX", -1},
		{"GND", "", 0},
		{"CLK", "", 0},
	}
	for _, c := range cases {
		base, pol, ok := pairBaseAndPolarity(c.name)
		if !ok && c.pol != 0 {
			t.Fatalf("%s : devrait être reconnu comme pôle", c.name)
		}
		if c.pol == 0 {
			if ok {
				t.Fatalf("%s : ne doit pas former de paire", c.name)
			}
			continue
		}
		if base != c.base || pol != c.pol {
			t.Fatalf("%s : attendu (%s,%d), obtenu (%s,%d)", c.name, c.base, c.pol, base, pol)
		}
	}
}

func TestCoupledImpedancesPhysicalBounds(t *testing.T) {
	z0, zodd, zeven, zdiff, zcom := coupledImpedances(0.25, 0.15)
	if z0 <= 0 || zodd <= 0 || zeven <= 0 || zdiff <= 0 || zcom <= 0 {
		t.Fatalf("impédances positives attendues : %+v", map[string]float64{"z0": z0, "zodd": zodd, "zeven": zeven, "zdiff": zdiff, "zcom": zcom})
	}
	// Couplage serré : Zodd < Z0 < Zeven.
	if !(zodd < z0 && z0 < zeven) {
		t.Fatalf("attendu Zodd < Z0 < Zeven, obtenu %.1f / %.1f / %.1f", zodd, z0, zeven)
	}
	// Grand écart : tout converge vers la piste simple.
	_, zoddFar, zevenFar, zdiffFar, _ := coupledImpedances(0.25, 8.0)
	if math.Abs(zoddFar-z0) > 0.5 || math.Abs(zevenFar-z0) > 0.5 {
		t.Fatalf("à écart infini Zodd/Zeven doivent converger vers Z0 : %.1f vs %.1f/%.1f", z0, zoddFar, zevenFar)
	}
	if math.Abs(zdiffFar-2*z0) > 1.0 {
		t.Fatalf("Zdiff doit tendre vers 2·Z0 : %.1f vs %.1f", zdiffFar, 2*z0)
	}
	_ = zcom
}

func TestImpedanceSolversHitTarget(t *testing.T) {
	for _, target := range []float64{85, 90, 100, 110} {
		w := solveWidthForZdiff(target, 0.15)
		if w < 0.05 || w > 5.0 {
			t.Fatalf("cible %.0f : largeur irréaliste %.3f mm", target, w)
		}
		if got := zdiffAt(w, 0.15); math.Abs(got-target) > 0.1 {
			t.Fatalf("solveur largeur : cible %.0f, atteint %.2f", target, got)
		}
		g := solveGapForZdiff(target, 0.25)
		if got := zdiffAt(0.25, g); math.Abs(got-target) > 0.1 {
			t.Fatalf("solveur écart : cible %.0f, atteint %.2f", target, got)
		}
	}
}

func TestRunDetectsPairsAndClassTargets(t *testing.T) {
	repo, id := newImpedanceFixture(t, true)
	svc := NewImpedanceService(repo)

	res, err := svc.Run(context.Background(), id, ImpedanceRequest{})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if res.AnalyzedPairs != 2 {
		t.Fatalf("2 paires attendues, obtenu %d", res.AnalyzedPairs)
	}
	byClass := map[string]DiffClassReport{}
	for _, c := range res.Classes {
		byClass[c.Class] = c
	}
	hs, ok := byClass[string(domainschematic.ClassHighSpeed)]
	if !ok {
		t.Fatalf("classe high-speed attendue : %+v", res.Classes)
	}
	if hs.TargetOhms != 100 {
		t.Fatalf("cible par défaut high-speed = 100 Ω, obtenu %.0f", hs.TargetOhms)
	}

	// Surcharge de cible : la classe suit la requête.
	res2, err := svc.Run(context.Background(), id, ImpedanceRequest{Targets: map[string]float64{"high-speed": 90}})
	if err != nil {
		t.Fatalf("Run cible 90 : %v", err)
	}
	for _, c := range res2.Classes {
		if c.Class == "high-speed" && c.TargetOhms != 90 {
			t.Fatalf("cible surchargée à 90 Ω attendue, obtenu %.0f", c.TargetOhms)
		}
	}
}

func TestRunMeasuresGapSkewAndTolerance(t *testing.T) {
	repo, id := newImpedanceFixture(t, false) // classes déduites des pistes
	svc := NewImpedanceService(repo)

	res, err := svc.Run(context.Background(), id, ImpedanceRequest{})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	var usb *DiffPairReport
	for i := range res.Classes {
		for j := range res.Classes[i].Pairs {
			p := &res.Classes[i].Pairs[j]
			if p.NetPlus == "USB_DP" {
				usb = p
			}
		}
	}
	if usb == nil {
		t.Fatal("paire USB_DP/USB_DN introuvable")
	}
	if !usb.Routed {
		t.Fatal("la paire USB est routée")
	}
	// Écart mesuré entre les deux pistes parallèles : 0.2 mm.
	if math.Abs(usb.GapMM-0.2) > 1e-6 {
		t.Fatalf("écart mesuré attendu 0.2 mm, obtenu %.3f", usb.GapMM)
	}
	// Skew ETH : 6 mm de différence de longueur → signalé.
	var eth *DiffPairReport
	for i := range res.Classes {
		for j := range res.Classes[i].Pairs {
			p := &res.Classes[i].Pairs[j]
			if p.NetPlus == "ETH_TXP" {
				eth = p
			}
		}
	}
	if eth == nil {
		t.Fatal("paire ETH introuvable")
	}
	if eth.SkewMM < 5.0 {
		t.Fatalf("skew ETH attendu >= 5 mm, obtenu %.2f", eth.SkewMM)
	}
	found := false
	for _, a := range eth.Advices {
		if strings.Contains(a, "Décalage intra-paire") {
			found = true
		}
	}
	if !found {
		t.Fatalf("un conseil de serpentin est attendu : %+v", eth.Advices)
	}
	if usb.TargetOhms != 100 {
		// classe "default" → cible 100 Ω.
		t.Fatalf("cible défaut = 100 Ω, obtenu %.0f", usb.TargetOhms)
	}
}
