// Package integration_test wires the whole backend pipeline together:
// in-memory repository + KiCad reader + ERC/DRC checkers + Gerber export +
// route jobs driven by a mock AIService. Only the standard library "testing"
// package is used; assertions follow contracts.md §4/§5 signatures.
package integration_test

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	exportapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/export"
	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
	verificationapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/verification"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	reader "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/fileio/reader"
	writer "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/fileio/writer"
	memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/logger"
)

// fixturePath resolves a fixture relative to the package directory, whatever
// the layout used to invoke go test. This test lives under backend/tests/
// because Go's internal-package visibility rule forbids importing
// backend/internal/** from the top-level tests/ directory.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "tests", "fixtures", name),
		filepath.Join("..", "..", "tests", "fixtures", name),
		filepath.Join("..", "fixtures", name),
		filepath.Join("tests", "fixtures", name),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Fatalf("fixture %s introuvable (candidats : %v)", name, candidates)
	return ""
}

// mustNewProject creates and persists a project or fails the test.
func mustNewProject(t *testing.T, ctx context.Context, repo domainproject.Repository,
	name string) *domainproject.Project {
	t.Helper()
	p, err := domainproject.New(name, "", 2)
	if err != nil {
		t.Fatalf("création du projet %q : %v", name, err)
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("persistance du projet %q : %v", name, err)
	}
	return p
}

// resistorFootprint builds a two-pad 0603 footprint with the given nets.
func resistorFootprint(netPad1, netPad2 string) domainlayout.Footprint {
	return domainlayout.Footprint{
		Name:         "Resistor_SMD:R_0603_1608Metric",
		Value:        "10k",
		BodyWidthMM:  1.6,
		BodyHeightMM: 0.8,
		HeightMM:     0.5,
		Pads: []domainlayout.Pad{
			{Name: "1", Shape: domainlayout.ShapeRect, X: -0.75, Y: 0,
				Width: 0.6, Height: 0.6, Layer: 0, Net: netPad1},
			{Name: "2", Shape: domainlayout.ShapeRect, X: 0.75, Y: 0,
				Width: 0.6, Height: 0.6, Layer: 0, Net: netPad2},
		},
	}
}

// TestImportFixtureAndERC covers contracts.md §9 steps 1-3: project creation
// (memory), import of tests/fixtures/sample.kicad_pcb through the reader
// registry, attachment to the aggregate and a clean ERC run.
func TestImportFixtureAndERC(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()

	p := mustNewProject(t, ctx, repo, "Demo")

	reg := reader.NewRegistry(logger.New("debug"))
	res, err := reg.Read(fixturePath(t, "sample.kicad_pcb"))
	if err != nil {
		t.Fatalf("lecture de sample.kicad_pcb : %v", err)
	}
	if res.Format != "kicad" {
		t.Errorf("format détecté = %q, attendu %q", res.Format, "kicad")
	}
	if res.Board == nil {
		t.Fatal("board attendu non nil après import d'un .kicad_pcb")
	}
	if got := len(res.Board.Components); got < 4 {
		t.Errorf("composants sur le board = %d, attendu >= 4", got)
	}
	if res.Schematic == nil {
		t.Fatal("schéma attendu non nil après import d'un .kicad_pcb")
	}
	if got := len(res.Schematic.Nets); got < 2 {
		t.Errorf("nets du schéma = %d, attendu >= 2", got)
	}

	p.SetSchematic(res.Schematic)
	p.SetBoard(res.Board)
	if res.Constraints != nil {
		p.SetConstraints(res.Constraints)
	} else {
		p.SetConstraints(domainconstraints.NewDefault())
	}
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("mise à jour du projet après import : %v", err)
	}

	checker := verificationapp.NewERCChecker(repo)
	r, err := checker.Run(ctx, p.ID())
	if err != nil {
		t.Fatalf("exécution ERC : %v", err)
	}
	if r == nil {
		t.Fatal("rapport ERC nil")
	}
	for _, v := range r.Violations {
		if v.Severity == "error" {
			t.Errorf("violation ERC de gravité error inattendue : %s (%s)", v.Code, v.Message)
		}
	}
	if !r.Passed && len(r.Violations) == 0 {
		t.Errorf("ERC Passed=false alors qu'aucune violation n'est remontée")
	}
}

// TestDRCDetectsClearanceViolation covers contracts.md §9 step 4: two tracks
// on different nets 0.35 mm apart (centers) with default rules require
// 0.2 + (0.25+0.25)/2 = 0.45 mm of clearance, so DRC_CLEARANCE must fire.
func TestDRCDetectsClearanceViolation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()

	board, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("création du board : %v", err)
	}
	if err := board.AddComponent(domainlayout.PlacedComponent{
		Ref:       "RA",
		Footprint: resistorFootprint("A", ""),
		X:         5,
		Y:         30,
	}); err != nil {
		t.Fatalf("ajout composant RA : %v", err)
	}
	if err := board.AddComponent(domainlayout.PlacedComponent{
		Ref:       "RB",
		Footprint: resistorFootprint("B", ""),
		X:         45,
		Y:         30,
	}); err != nil {
		t.Fatalf("ajout composant RB : %v", err)
	}

	board.AddTrack(domainlayout.Track{
		Net: "A", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 10, Y: 10}, {X: 30, Y: 10}},
	})
	board.AddTrack(domainlayout.Track{
		Net: "B", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 10, Y: 10.35}, {X: 30, Y: 10.35}},
	})

	p := mustNewProject(t, ctx, repo, "DRC demo")
	p.SetBoard(board)
	p.SetConstraints(domainconstraints.NewDefault()) // clearance 0.2 mm
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("mise à jour du projet : %v", err)
	}

	checker := verificationapp.NewDRCChecker(repo)
	r, err := checker.Run(ctx, p.ID())
	if err != nil {
		t.Fatalf("exécution DRC : %v", err)
	}
	if r == nil {
		t.Fatal("rapport DRC nil")
	}
	if len(r.Violations) < 1 {
		t.Fatalf("au moins une violation DRC attendue (pistes A/B distantes de 0,35 mm)")
	}
	found := false
	for _, v := range r.Violations {
		if v.Code == "DRC_CLEARANCE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("violation DRC_CLEARANCE attendue, codes reçus : %v", r.Violations)
	}
}

// TestGerberExportZip covers contracts.md §9 step 5: ExportToZip produces a
// readable archive containing the top-copper Gerber (-F_Cu.gbr).
func TestGerberExportZip(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()

	board, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("création du board : %v", err)
	}
	if err := board.AddComponent(domainlayout.PlacedComponent{
		Ref:       "R1",
		Footprint: resistorFootprint("A", ""),
		X:         10,
		Y:         10,
	}); err != nil {
		t.Fatalf("ajout composant R1 : %v", err)
	}
	board.AddTrack(domainlayout.Track{
		Net: "A", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 5, Y: 10}, {X: 15, Y: 10}},
	})

	p := mustNewProject(t, ctx, repo, "Export demo")
	p.SetBoard(board)
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("mise à jour du projet : %v", err)
	}

	svc := exportapp.NewGerberService(repo, writer.NewGerberWriter(), t.TempDir(), logger.New("info"))
	zipPath, files, err := svc.ExportToZip(ctx, p.ID())
	if err != nil {
		t.Fatalf("ExportToZip : %v", err)
	}
	if filepath.Ext(zipPath) != ".zip" {
		t.Errorf("chemin zip = %q, extension .zip attendue", zipPath)
	}

	inFileList := false
	for _, f := range files {
		if strings.HasSuffix(f, "-F_Cu.gbr") {
			inFileList = true
			break
		}
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("ouverture de l'archive %s : %v", zipPath, err)
	}
	defer zr.Close()

	var entry *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "-F_Cu.gbr") {
			entry = f
			break
		}
	}
	if entry == nil && !inFileList {
		t.Fatalf("aucun fichier -F_Cu.gbr (archive : %v, liste : %v)", zr.File, files)
	}
	if entry == nil {
		t.Fatal("entrée -F_Cu.gbr absente de l'archive alors qu'elle figure dans la liste")
	}

	rc, err := entry.Open()
	if err != nil {
		t.Fatalf("ouverture de l'entrée %s : %v", entry.Name, err)
	}
	content, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil {
		t.Fatalf("lecture de l'entrée %s : %v", entry.Name, err)
	}
	if closeErr != nil {
		t.Fatalf("fermeture de l'entrée %s : %v", entry.Name, closeErr)
	}
	if len(content) == 0 {
		t.Fatal("contenu Gerber vide")
	}
	if !strings.HasPrefix(strings.TrimSpace(string(content)), "G04") {
		t.Errorf("le contenu Gerber doit commencer par G04 (début : %q)", content[:min(24, len(content))])
	}
}

// ---- Mock AIService (contracts.md §4 port layoutapp.AIService) ------------

// mockAI routes every net with a single L-shaped track (pad A -> corner ->
// pad B) plus one via, emitting one progress event per net and a final done
// event. The use case is responsible for applying the outcome to the board.
type mockAI struct{}

func (m *mockAI) Health(ctx context.Context) error { return nil }

func (m *mockAI) PlanPlacement(ctx context.Context, b *domainlayout.Board,
	comps []domainschematic.Component, strategy string) ([]domainlayout.PlacedComponent, error) {
	placed := make([]domainlayout.PlacedComponent, 0, len(comps))
	for _, c := range comps {
		placed = append(placed, domainlayout.PlacedComponent{
			Ref:       c.Ref,
			Footprint: domainlayout.Footprint{Name: c.Footprint},
			X:         c.X,
			Y:         c.Y,
			Rotation:  c.Rotation,
			Fixed:     c.Fixed,
		})
	}
	return placed, nil
}

func (m *mockAI) RouteBoard(ctx context.Context, b *domainlayout.Board,
	nets []domainschematic.Net, cs *domainconstraints.ConstraintSet, strategy string,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	outcome := &layoutapp.RouteOutcome{Strategy: "mock", Nets: []layoutapp.NetRoute{}}
	for i, net := range nets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		outcome.Nets = append(outcome.Nets, routeNetLShaped(b, net.Name))
		if onProgress != nil {
			percent := 100.0
			if len(nets) > 0 {
				percent = float64(i+1) * 100.0 / float64(len(nets))
			}
			onProgress(layoutapp.RouteProgress{
				Stage:      "route",
				CurrentNet: net.Name,
				Message:    fmt.Sprintf("net %s routé (mock)", net.Name),
				Percent:    percent,
			})
		}
	}
	if onProgress != nil {
		onProgress(layoutapp.RouteProgress{
			Stage:   "route",
			Message: "routage terminé (mock)",
			Percent: 100,
			Done:    true,
		})
	}
	return outcome, nil
}

func (m *mockAI) OptimizeRoutes(ctx context.Context, b *domainlayout.Board,
	outcome *layoutapp.RouteOutcome, cs *domainconstraints.ConstraintSet,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	return outcome, nil
}

// routeNetLShaped builds the mock route of one net: an L polyline between the
// first two pads found on the board for that net, with a via at the corner.
func routeNetLShaped(b *domainlayout.Board, netName string) layoutapp.NetRoute {
	type padPos struct{ x, y float64 }
	var pads []padPos
	for i := range b.Components {
		c := &b.Components[i]
		for _, pad := range c.Footprint.Pads {
			if pad.Net != netName {
				continue
			}
			pos := c.PadAbsolutePosition(pad)
			pads = append(pads, padPos{x: pos.X, y: pos.Y})
		}
	}

	route := layoutapp.NetRoute{Net: netName}
	if len(pads) < 2 {
		return route
	}
	a, z := pads[0], pads[1]
	corner := padPos{x: a.x, y: z.y}
	track := domainlayout.Track{
		Net:   netName,
		Layer: 0,
		Width: 0.25,
		Points: []domainlayout.TrackPoint{
			{X: a.x, Y: a.y},
			{X: corner.x, Y: corner.y},
			{X: z.x, Y: z.y},
		},
	}
	route.Tracks = []domainlayout.Track{track}
	route.Vias = []domainlayout.Via{{
		X: corner.x, Y: corner.y, FromLayer: 0, ToLayer: 1,
		Diameter: 0.6, Drill: 0.3, Net: netName,
	}}
	route.LengthMM = track.Length()
	route.Completed = true
	return route
}

// recordingPublisher is a ProgressPublisher double collecting every event.
type recordingPublisher struct {
	mu     sync.Mutex
	events []publishedEvent
}

type publishedEvent struct {
	jobID     string
	projectID string
	progress  layoutapp.RouteProgress
}

func (p *recordingPublisher) Publish(jobID, projectID string, prog layoutapp.RouteProgress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, publishedEvent{jobID: jobID, projectID: projectID, progress: prog})
}

func (p *recordingPublisher) countForProject(projectID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, e := range p.events {
		if e.projectID == projectID {
			n++
		}
	}
	return n
}

// TestRouteJobWithMockAI covers contracts.md §9 step 6: Start (202-style
// async job), progress publication, final "done" state and a board persisted
// with the routed tracks.
func TestRouteJobWithMockAI(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewProjectRepository()

	sch := domainschematic.New("routage-test")
	for _, name := range []string{"A", "B"} {
		if err := sch.AddNet(domainschematic.Net{Name: name, Class: domainschematic.ClassDefault}); err != nil {
			t.Fatalf("ajout du net %s : %v", name, err)
		}
	}

	board, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		t.Fatalf("création du board : %v", err)
	}
	// Deux composants, chacun avec un pad sur A et un pad sur B.
	for _, spec := range []struct {
		ref  string
		x, y float64
	}{
		{ref: "R1", x: 10, y: 10},
		{ref: "R2", x: 20, y: 20},
	} {
		if err := board.AddComponent(domainlayout.PlacedComponent{
			Ref:       spec.ref,
			Footprint: resistorFootprint("A", "B"),
			X:         spec.x,
			Y:         spec.y,
		}); err != nil {
			t.Fatalf("ajout composant %s : %v", spec.ref, err)
		}
	}

	p := mustNewProject(t, ctx, repo, "Routage mock")
	p.SetSchematic(sch)
	p.SetBoard(board)
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("mise à jour du projet : %v", err)
	}

	pub := &recordingPublisher{}
	svc := layoutapp.NewRouteService(repo, &mockAI{}, layoutapp.NewJobRegistry(), pub, logger.New("debug"))

	jobID, err := svc.Start(ctx, p.ID(), "astar", nil)
	if err != nil {
		t.Fatalf("démarrage du job de routage : %v", err)
	}
	if jobID == "" {
		t.Fatal("identifiant de job vide")
	}

	var info layoutapp.JobInfo
	var ok bool
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, ok = svc.JobStatus(jobID)
		if !ok {
			t.Fatalf("job %s introuvable dans le registre", jobID)
		}
		if info.State == "done" || info.State == "failed" || info.State == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job non terminé après 5 s (état courant : %s, job : %+v)", info.State, info)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info.State != "done" {
		t.Fatalf("état final du job = %s, attendu %q (job : %+v)", info.State, "done", info)
	}

	reloaded, err := repo.FindByID(ctx, p.ID())
	if err != nil {
		t.Fatalf("rechargement du projet : %v", err)
	}
	if reloaded.Board() == nil {
		t.Fatal("board absent du projet après routage")
	}
	routed := 0
	for _, tr := range reloaded.Board().Tracks {
		if tr.Net == "A" || tr.Net == "B" {
			routed++
		}
	}
	if routed < 2 {
		t.Errorf("pistes sur les nets A/B = %d, attendu >= 2", routed)
	}

	if n := pub.countForProject(p.ID()); n < 2 {
		t.Errorf("événements publiés pour le projet = %d, attendu >= 2", n)
	}
}
