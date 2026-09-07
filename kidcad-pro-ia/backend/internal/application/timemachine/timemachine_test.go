package timemachine

import (
        "context"
        "testing"

        domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
        domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
        domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
        memory "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/persistence/memory"
)

func newTMFixture(t *testing.T) (domainproject.Repository, string, *domainlayout.Board) {
        t.Helper()
        repo := memory.NewProjectRepository()
        p, err := domainproject.New("Voyage", "test time machine", 2)
        if err != nil {
                t.Fatalf("New : %v", err)
        }
        b, err := domainlayout.NewBoard(50, 40, 2)
        if err != nil {
                t.Fatalf("NewBoard : %v", err)
        }
        fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8}
        _ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R1", Footprint: fp, X: 10, Y: 10})
        b.AddTrack(domainlayout.Track{Net: "N1", Layer: 0, Width: 0.25,
                Points: []domainlayout.TrackPoint{{X: 10, Y: 10}, {X: 30, Y: 10}}})
        p.SetBoard(b)
        p.SetConstraints(domainconstraints.NewDefault())
        if err := repo.Create(context.Background(), p); err != nil {
                t.Fatalf("Create : %v", err)
        }
        return repo, p.ID(), b
}

func TestCaptureListDiffRestore(t *testing.T) {
        ctx := context.Background()
        repo, id, _ := newTMFixture(t)
        svc := NewService(repo, nil)

        // Capture de l'état initial.
        meta, err := svc.Capture(ctx, id, "état initial")
        if err != nil {
                t.Fatalf("Capture : %v", err)
        }
        if meta.Board.Components != 1 || meta.Board.Tracks != 1 {
                t.Fatalf("méta inattendue : %+v", meta.Board)
        }
        if len(svc.List(id)) != 1 {
                t.Fatalf("1 capture attendue : %d", len(svc.List(id)))
        }

        // Évolution de la carte : R1 déplacé, R2 ajouté, piste allongée, via +
        // règle modifiée.
        p, err := repo.FindByID(ctx, id)
        if err != nil {
                t.Fatalf("FindByID : %v", err)
        }
        b := p.Board()
        r1, _ := b.ComponentByRef("R1")
        r1.X = 25
        fp := domainlayout.Footprint{Name: "0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8}
        _ = b.AddComponent(domainlayout.PlacedComponent{Ref: "R2", Footprint: fp, X: 40, Y: 30})
        b.Tracks[0].Points[1].X = 45
        _ = b.AddVia(domainlayout.Via{Net: "N1", X: 35, Y: 10, FromLayer: 0, ToLayer: 1,
                Diameter: 0.6, Drill: 0.3})
        p.Constraints().SetMinTrackWidth(0.3, "all")
        p.SetBoard(b)
        if err := repo.Update(ctx, p); err != nil {
                t.Fatalf("Update : %v", err)
        }

        // Diff riche.
        diff, err := svc.Diff(ctx, id, meta.ID)
        if err != nil {
                t.Fatalf("Diff : %v", err)
        }
        if !diff.Changed {
                t.Fatalf("des changements sont attendus : %+v", diff)
        }
        kinds := map[string]int{}
        for _, e := range diff.Entries {
                kinds[e.Kind]++
        }
        if kinds["component"] < 2 || kinds["track"] == 0 || kinds["via"] == 0 || kinds["rule"] == 0 {
                t.Fatalf("diff incomplet : %+v", kinds)
        }

        // Restauration : retour à l'état initial.
        restored, err := svc.Restore(ctx, id, meta.ID)
        if err != nil {
                t.Fatalf("Restore : %v", err)
        }
        if restored.ID != meta.ID {
                t.Fatalf("mauvaise capture restaurée : %+v", restored)
        }
        p2, _ := repo.FindByID(ctx, id)
        if got := len(p2.Board().Components); got != 1 {
                t.Fatalf("R2 devrait avoir disparu après restauration : %d composants", got)
        }
        if p2.Board().ViaCount() != 0 {
                t.Fatalf("vias attendus à zéro après restauration : %d", p2.Board().ViaCount())
        }
        if p2.Board().Tracks[0].Points[1].X != 30 {
                t.Fatalf("longueur de piste non restaurée : %v", p2.Board().Tracks[0].Points[1].X)
        }
        if v, _ := p2.Constraints().Value(domainconstraints.RuleMinTrackWidth, "*", -1); v != 0.2 {
                t.Fatalf("règle non restaurée : %v", v)
        }

        // La restauration a capturé l'état intermédiaire (réversibilité) :
        // initiale + capture de sécurité avant restauration.
        if len(svc.List(id)) != 2 {
                t.Fatalf("2 captures attendues (initiale + sécurité) : %d", len(svc.List(id)))
        }
}

func TestRingBufferCapsHistory(t *testing.T) {
        ctx := context.Background()
        repo, id, _ := newTMFixture(t)
        svc := NewService(repo, nil)
        for i := 0; i < maxSnapshotsPerProject+5; i++ {
                if _, err := svc.Capture(ctx, id, "auto"); err != nil {
                        t.Fatalf("Capture #%d : %v", i, err)
                }
        }
        if got := len(svc.List(id)); got != maxSnapshotsPerProject {
                t.Fatalf("historique plafonné à %d, obtenu %d", maxSnapshotsPerProject, got)
        }
}

func TestUnknownSnapshotFails(t *testing.T) {
        ctx := context.Background()
        repo, id, _ := newTMFixture(t)
        svc := NewService(repo, nil)
        if _, err := svc.Diff(ctx, id, "inconnu"); err == nil {
                t.Fatal("erreur attendue pour une capture inconnue (diff)")
        }
        if _, err := svc.Restore(ctx, id, "inconnu"); err == nil {
                t.Fatal("erreur attendue pour une capture inconnue (restore)")
        }
}
