// Package timemachine implements design time-travel: named snapshots of the
// board + constraint set, a structural diff between any snapshot and the
// current board, and one-click restoration. Real EDA tools bury history in
// opaque autosave files; here the whole history is queryable over REST and
// every diff is expressed in engineer-readable terms (moved parts, added
// copper, rule changes).
package timemachine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/utils"
)

// History limits.
const (
	maxSnapshotsPerProject = 20 // ring buffer
	moveThresholdMM        = 0.05
)

// SnapshotMeta is the listing view of a snapshot (without the heavy payload).
type SnapshotMeta struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	At        time.Time `json:"at"`
	Board     BoardMeta `json:"board"`
	RuleCount int       `json:"rule_count"`
}

// BoardMeta summarizes the captured board.
type BoardMeta struct {
	WidthMM    float64 `json:"width_mm"`
	HeightMM   float64 `json:"height_mm"`
	Components int     `json:"components"`
	Tracks     int     `json:"tracks"`
	Vias       int     `json:"vias"`
	LengthMM   float64 `json:"length_mm"`
	MinTrackMM float64 `json:"min_track_mm"`
}

// DiffEntry is one structural change between a snapshot and the current board.
type DiffEntry struct {
	Kind   string `json:"kind"`   // "component" | "track" | "via" | "rule"
	Change string `json:"change"` // "added" | "removed" | "moved" | "changed"
	Target string `json:"target"`
	Detail string `json:"detail"`
}

// Diff is the comparison result.
type Diff struct {
	SnapshotID  string      `json:"snapshot_id"`
	SnapshotLbl string      `json:"snapshot_label"`
	Entries     []DiffEntry `json:"entries"`
	Summary     string      `json:"summary"`
	Changed     bool        `json:"changed"`
}

// snapshot is the stored payload.
type snapshot struct {
	meta        SnapshotMeta
	board       *domainlayout.Board
	constraints *domainconstraints.ConstraintSet
}

// Service is the time-machine use case. Snapshots live for the process
// lifetime (same contract as the arena leaderboard).
type Service struct {
	projects domainproject.Repository
	log      *slog.Logger

	mu    sync.Mutex
	store map[string][]*snapshot // project_id -> snapshots (ordre chronologique)
}

// NewService builds the time machine.
func NewService(projects domainproject.Repository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		projects: projects,
		log:      log,
		store:    make(map[string][]*snapshot),
	}
}

// Capture freezes the current board + constraints into a new snapshot.
func (s *Service) Capture(ctx context.Context, projectID, label string) (*SnapshotMeta, error) {
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.Board() == nil {
		return nil, fmt.Errorf("timemachine : le projet %q n'a pas de carte à capturer", projectID)
	}

	if label == "" {
		label = "Capture " + time.Now().Format("15:04:05")
	}

	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	meta := SnapshotMeta{
		ID:        utils.NewID(),
		Label:     label,
		At:        time.Now().UTC(),
		Board:     summarize(p.Board()),
		RuleCount: len(cs.Rules),
	}

	s.mu.Lock()
	history := append(s.store[projectID], &snapshot{
		meta:        meta,
		board:       cloneBoard(p.Board()),
		constraints: cs.Clone(),
	})
	if len(history) > maxSnapshotsPerProject {
		history = history[len(history)-maxSnapshotsPerProject:]
	}
	s.store[projectID] = history
	s.mu.Unlock()

	s.log.Info("timemachine : capture", "project_id", projectID, "label", label)
	return &meta, nil
}

// List returns the snapshots of a project, newest first.
func (s *Service) List(projectID string) []SnapshotMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.store[projectID]
	out := make([]SnapshotMeta, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		out = append(out, history[i].meta)
	}
	return out
}

// Diff compares a snapshot with the current board of the project.
func (s *Service) Diff(ctx context.Context, projectID, snapshotID string) (*Diff, error) {
	snap := s.get(projectID, snapshotID)
	if snap == nil {
		return nil, fmt.Errorf("timemachine : capture %q introuvable", snapshotID)
	}
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	current := p.Board()
	if current == nil {
		return nil, fmt.Errorf("timemachine : le projet %q n'a plus de carte", projectID)
	}

	d := &Diff{
		SnapshotID:  snapshotID,
		SnapshotLbl: snap.meta.Label,
		Entries:     []DiffEntry{},
	}
	diffBoards(d, snap.board, current)
	diffRules(d, snap.constraints, p.Constraints())

	d.Changed = len(d.Entries) > 0
	if d.Changed {
		added, removed, moved, other := 0, 0, 0, 0
		for _, e := range d.Entries {
			switch {
			case e.Change == "added":
				added++
			case e.Change == "removed":
				removed++
			case e.Change == "moved":
				moved++
			default:
				other++
			}
		}
		d.Summary = fmt.Sprintf("%d ajout(s), %d suppression(s), %d déplacement(s), %d modification(s)",
			added, removed, moved, other)
	} else {
		d.Summary = "Aucune différence : la carte est identique à la capture."
	}
	return d, nil
}

// Restore replaces the current board and constraints with the snapshot
// content and persists the aggregate.
func (s *Service) Restore(ctx context.Context, projectID, snapshotID string) (*SnapshotMeta, error) {
	snap := s.get(projectID, snapshotID)
	if snap == nil {
		return nil, fmt.Errorf("timemachine : capture %q introuvable", snapshotID)
	}
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Sécurité : capture automatique de l'état actuel avant restauration,
	// pour que le restore soit lui-même réversible.
	if p.Board() != nil {
		if _, err := s.Capture(ctx, projectID, "Avant restauration de « "+snap.meta.Label+" »"); err != nil {
			s.log.Warn("timemachine : capture de sécurité impossible", "err", err)
		}
	}

	p.SetBoard(cloneBoard(snap.board))
	p.SetConstraints(snap.constraints.Clone())
	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("timemachine : persistance de la restauration : %w", err)
	}

	meta := snap.meta
	s.log.Info("timemachine : restauration", "project_id", projectID, "snapshot", meta.Label)
	return &meta, nil
}

// --------------------------------------------------------------
// Diff structurel
// --------------------------------------------------------------

func diffBoards(d *Diff, before, after *domainlayout.Board) {
	oldComps := map[string]domainlayout.PlacedComponent{}
	for _, c := range before.Components {
		oldComps[c.Ref] = c
	}
	newComps := map[string]domainlayout.PlacedComponent{}
	for _, c := range after.Components {
		newComps[c.Ref] = c
	}

	refs := make([]string, 0, len(newComps))
	for ref := range newComps {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		c := newComps[ref]
		old, existed := oldComps[ref]
		switch {
		case !existed:
			d.Entries = append(d.Entries, DiffEntry{Kind: "component", Change: "added",
				Target: ref, Detail: fmt.Sprintf("%s (%s) ajouté en (%.2f, %.2f)", ref, c.Footprint.Name, c.X, c.Y)})
		case mathAbs(c.X-old.X) > moveThresholdMM || mathAbs(c.Y-old.Y) > moveThresholdMM || c.Rotation != old.Rotation:
			d.Entries = append(d.Entries, DiffEntry{Kind: "component", Change: "moved",
				Target: ref, Detail: fmt.Sprintf("%s déplacé (%.2f, %.2f) → (%.2f, %.2f)", ref, old.X, old.Y, c.X, c.Y)})
		}
	}

	oldRefs := make([]string, 0, len(oldComps))
	for ref := range oldComps {
		oldRefs = append(oldRefs, ref)
	}
	sort.Strings(oldRefs)
	for _, ref := range oldRefs {
		if _, exists := newComps[ref]; !exists {
			d.Entries = append(d.Entries, DiffEntry{Kind: "component", Change: "removed",
				Target: ref, Detail: fmt.Sprintf("%s retiré de la carte", ref)})
		}
	}

	// Cuivre : comparaison quantitative par net (le diff piste à piste
	// serait illisible pour un routage régénéré).
	oldLen, newLen := copperByNet(before), copperByNet(after)
	nets := make([]string, 0, len(newLen))
	for net := range newLen {
		nets = append(nets, net)
	}
	sort.Strings(nets)
	for _, net := range nets {
		o, existed := oldLen[net]
		n := newLen[net]
		if !existed {
			d.Entries = append(d.Entries, DiffEntry{Kind: "track", Change: "added",
				Target: net, Detail: fmt.Sprintf("Net %q routé (%.1f mm de cuivre)", net, n.length)})
			continue
		}
		if mathAbs(n.length-o.length) > 0.5 || n.tracks != o.tracks {
			d.Entries = append(d.Entries, DiffEntry{Kind: "track", Change: "changed",
				Target: net, Detail: fmt.Sprintf("Net %q : %d → %d piste(s), %.1f → %.1f mm",
					net, o.tracks, n.tracks, o.length, n.length)})
		}
	}
	for _, net := range sortedNets(oldLen) {
		if _, exists := newLen[net]; !exists {
			d.Entries = append(d.Entries, DiffEntry{Kind: "track", Change: "removed",
				Target: net, Detail: fmt.Sprintf("Routage du net %q supprimé", net)})
		}
	}

	if before.ViaCount() != after.ViaCount() {
		delta := after.ViaCount() - before.ViaCount()
		change := "added"
		if delta < 0 {
			change = "removed"
		}
		d.Entries = append(d.Entries, DiffEntry{Kind: "via", Change: change,
			Target: "board", Detail: fmt.Sprintf("%d via(s) %s (%d → %d)",
				int(mathAbs(float64(delta))), map[string]string{"added": "ajouté(s)", "removed": "supprimé(s)"}[change],
				before.ViaCount(), after.ViaCount())})
	}
}

func diffRules(d *Diff, before, after *domainconstraints.ConstraintSet) {
	if before == nil || after == nil {
		return
	}
	oldRules := map[string]domainconstraints.Rule{}
	for _, r := range before.Rules {
		oldRules[r.ID] = r
	}
	for _, r := range after.Rules {
		old, existed := oldRules[r.ID]
		if !existed {
			d.Entries = append(d.Entries, DiffEntry{Kind: "rule", Change: "added",
				Target: r.ID, Detail: fmt.Sprintf("Règle ajoutée : %s (%.3f mm)", r.Name, r.ValueMM)})
			continue
		}
		if mathAbs(r.ValueMM-old.ValueMM) > 1e-9 || r.Enabled != old.Enabled {
			d.Entries = append(d.Entries, DiffEntry{Kind: "rule", Change: "changed",
				Target: r.ID, Detail: fmt.Sprintf("Règle %s : %.3f → %.3f mm", r.Name, old.ValueMM, r.ValueMM)})
		}
		delete(oldRules, r.ID)
	}
	for id, r := range oldRules {
		d.Entries = append(d.Entries, DiffEntry{Kind: "rule", Change: "removed",
			Target: id, Detail: fmt.Sprintf("Règle retirée : %s", r.Name)})
	}
}

// copperStat aggregates one net's copper.
type copperStat struct {
	tracks int
	length float64
}

func copperByNet(b *domainlayout.Board) map[string]copperStat {
	out := map[string]copperStat{}
	for i := range b.Tracks {
		st := out[b.Tracks[i].Net]
		st.tracks++
		st.length += b.Tracks[i].Length()
		out[b.Tracks[i].Net] = st
	}
	return out
}

func sortedNets(m map[string]copperStat) []string {
	nets := make([]string, 0, len(m))
	for net := range m {
		nets = append(nets, net)
	}
	sort.Strings(nets)
	return nets
}

// summarize builds the listing meta of a board.
func summarize(b *domainlayout.Board) BoardMeta {
	minTrack := 0.0
	for i := range b.Tracks {
		w := b.Tracks[i].Width
		if w > 0 && (minTrack == 0 || w < minTrack) {
			minTrack = w
		}
	}
	return BoardMeta{
		WidthMM: b.WidthMM, HeightMM: b.HeightMM,
		Components: len(b.Components), Tracks: len(b.Tracks), Vias: len(b.Vias),
		LengthMM:   round1(b.TotalTrackLength()),
		MinTrackMM: minTrack,
	}
}

// cloneBoard deep-copies a board (snapshots must be immutable).
func cloneBoard(b *domainlayout.Board) *domainlayout.Board {
	out := &domainlayout.Board{
		WidthMM: b.WidthMM, HeightMM: b.HeightMM,
		LayerCount: b.LayerCount,
		LayerNames: append([]string(nil), b.LayerNames...),
		Components: make([]domainlayout.PlacedComponent, len(b.Components)),
		Tracks:     make([]domainlayout.Track, len(b.Tracks)),
		Vias:       make([]domainlayout.Via, len(b.Vias)),
	}
	copy(out.Vias, b.Vias)
	for i, c := range b.Components {
		fp := c.Footprint
		fp.Pads = append([]domainlayout.Pad(nil), c.Footprint.Pads...)
		c.Footprint = fp
		out.Components[i] = c
	}
	for i, t := range b.Tracks {
		t.Points = append([]domainlayout.TrackPoint(nil), t.Points...)
		out.Tracks[i] = t
	}
	return out
}

// get returns a pointer to the stored snapshot (read-only usage).
func (s *Service) get(projectID, snapshotID string) *snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snap := range s.store[projectID] {
		if snap.meta.ID == snapshotID {
			return snap
		}
	}
	return nil
}

func (s *Service) load(ctx context.Context, projectID string) (*domainproject.Project, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("timemachine : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("timemachine : chargement du projet : %w", err)
	}
	return p, nil
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
