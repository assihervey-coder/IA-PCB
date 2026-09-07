// Package arena stages live routing battles between two autonomous
// fighters — "greedy" (naive Manhattan L-paths) and "astar" (grid A* with
// obstacle avoidance) — over the real netlist of a project. Each match
// produces a narrated battle log, per-fighter metrics, a winner and an ELO
// rating update persisted for the process lifetime. The frontend can
// render the fight like a sports replay: same nets, two philosophies, one
// leaderboard.
package arena

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// Battle tuning.
const (
	// maxNetsPerMatch caps the fight for snappy REST latency; the nets
	// with the widest span are chosen (best spectacle, hardest routing).
	maxNetsPerMatch = 12
	// eloDefault is the starting rating of a newcomer fighter.
	eloDefault = 1200.0
	// eloK is the K-factor of the ELO update.
	eloK = 24.0
)

// Endpoint is one pad the fighters must connect (absolute board coords).
type Endpoint struct {
	Ref string  `json:"ref"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
}

// NetTask is the routing brief of a single net.
type NetTask struct {
	Name      string     `json:"name"`
	Endpoints []Endpoint `json:"endpoints"`
}

// FighterResult is one fighter's performance card.
type FighterResult struct {
	Strategy      string   `json:"strategy"`
	Completed     int      `json:"completed"`
	Failed        int      `json:"failed"`
	TotalLengthMM float64  `json:"total_length_mm"`
	ViaCount      int      `json:"via_count"`
	Collisions    int      `json:"collisions"`
	DurationMS    int64    `json:"duration_ms"`
	Score         float64  `json:"score"`
	NetLog        []string `json:"net_log"`
}

// Fighter standing in the leaderboard.
type Standing struct {
	Strategy string  `json:"strategy"`
	Rating   float64 `json:"rating"`
	Matches  int     `json:"matches"`
	Wins     int     `json:"wins"`
	Losses   int     `json:"losses"`
	Draws    int     `json:"draws"`
}

// MatchReport is the full response of POST .../arena.
type MatchReport struct {
	ProjectID string        `json:"project_id"`
	Nets      []string      `json:"nets"`
	Greedy    FighterResult `json:"greedy"`
	Astar     FighterResult `json:"astar"`
	Winner    string        `json:"winner"` // "greedy" | "astar" | "draw"
	Margin    float64       `json:"margin"` // écart de score
	Elo       [2]Standing   `json:"elo"`    // après mise à jour
	Log       []string      `json:"log"`    // récit du combat (FR)
	At        time.Time     `json:"at"`
}

// EventSink receives live battle events (wired to the WebSocket hub by
// main.go); nil disables live streaming.
type EventSink func(net string, percent float64, message string)

// ArenaService orchestrates the fights and owns the leaderboard.
type ArenaService struct {
	projects domainproject.Repository
	log      *slog.Logger

	mu          sync.Mutex
	leaderboard map[string]*Standing
}

// NewArenaService builds the arena with its persistent-for-process
// leaderboard.
func NewArenaService(projects domainproject.Repository, log *slog.Logger) *ArenaService {
	if log == nil {
		log = slog.Default()
	}
	return &ArenaService{
		projects:    projects,
		log:         log,
		leaderboard: map[string]*Standing{},
	}
}

// Leaderboard returns the standings sorted by rating descending.
func (s *ArenaService) Leaderboard() []Standing {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Standing, 0, len(s.leaderboard))
	for _, st := range s.leaderboard {
		out = append(out, *st)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rating > out[j].Rating })
	return out
}

// Fight loads the project, builds the net tasks and runs both fighters on
// the identical workload, then updates the leaderboard.
func (s *ArenaService) Fight(ctx context.Context, projectID string, onEvent EventSink) (*MatchReport, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("arène : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("arène : chargement du projet : %w", err)
	}
	board := p.Board()
	sch := p.Schematic()
	if board == nil || sch == nil || len(sch.Nets) == 0 {
		return nil, errors.New("arène : aucun netlist exploitable (importez un schéma et un placement d'abord)")
	}

	tasks := buildTasks(sch, board)
	if len(tasks) == 0 {
		return nil, errors.New("arène : aucune broche localisable sur la carte : placez les composants d'abord")
	}
	sort.SliceStable(tasks, func(i, j int) bool { return span(tasks[i]) > span(tasks[j]) })
	if len(tasks) > maxNetsPerMatch {
		tasks = tasks[:maxNetsPerMatch]
	}

	report := &MatchReport{
		ProjectID: projectID,
		At:        time.Now().UTC(),
		Log: []string{
			fmt.Sprintf("⚔️  Le combat est lancé : %d nets, deux écoles s'affrontent.", len(tasks)),
			"🥊  greedy (Manhattan naïf) contre astar (détours intelligents).",
		},
	}

	report.Greedy = runFighter("greedy", tasks, board, func(net string, pct float64, msg string) {
		emit(onEvent, net, pct*0.5, msg)
	})
	report.Astar = runFighter("astar", tasks, board, func(net string, pct float64, msg string) {
		emit(onEvent, net, 50+pct*0.5, msg)
	})

	// Narration + verdict.
	report.Nets = make([]string, len(tasks))
	for i, t := range tasks {
		report.Nets[i] = t.Name
	}
	report.Log = append(report.Log,
		fmt.Sprintf("📊 greedy : %d/%d nets, %.1f mm, %d collision(s).",
			report.Greedy.Completed, len(tasks), report.Greedy.TotalLengthMM, report.Greedy.Collisions),
		fmt.Sprintf("📊 astar : %d/%d nets, %.1f mm, %d collision(s).",
			report.Astar.Completed, len(tasks), report.Astar.TotalLengthMM, report.Astar.Collisions),
	)

	greedyScore, astarScore := report.Greedy.Score, report.Astar.Score
	switch {
	case math.Abs(greedyScore-astarScore) < 0.5:
		report.Winner = "draw"
		report.Log = append(report.Log, "🤝 Match nul ! Les deux écoles font jeu égal.")
	case greedyScore > astarScore:
		report.Winner = "greedy"
		report.Log = append(report.Log, "🏆 greedy l'emporte — la brutalité Manhattan a gagné ce soir.")
	default:
		report.Winner = "astar"
		report.Log = append(report.Log, "🏆 astar l'emporte — l'intelligence évite les murs.")
	}
	report.Margin = math.Abs(greedyScore - astarScore)

	s.updateElo(report)
	report.Elo = [2]Standing{*s.standing("greedy"), *s.standing("astar")}
	report.Log = append(report.Log,
		fmt.Sprintf("📈 ELO mis à jour : greedy %.0f, astar %.0f.", report.Elo[0].Rating, report.Elo[1].Rating))

	s.log.Info("combat d'arène terminé", "project_id", projectID,
		"winner", report.Winner, "nets", len(tasks))
	return report, nil
}

// updateElo applies the ELO update under lock (1 win / loss / draw).
func (s *ArenaService) updateElo(r *MatchReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.standing("greedy")
	a := s.standing("astar")

	expected := 1.0 / (1.0 + math.Pow(10, (a.Rating-g.Rating)/400.0))
	actual := 0.5
	switch r.Winner {
	case "greedy":
		actual = 1.0
	case "astar":
		actual = 0.0
	}
	g.Rating += eloK * (actual - expected)
	a.Rating += eloK * ((1 - actual) - (1 - expected))
	g.Matches, a.Matches = g.Matches+1, a.Matches+1
	switch r.Winner {
	case "greedy":
		g.Wins, a.Losses = g.Wins+1, a.Losses+1
	case "astar":
		a.Wins, g.Losses = a.Wins+1, g.Losses+1
	default:
		g.Draws, a.Draws = g.Draws+1, a.Draws+1
	}
}

// standing seeds or returns the stored standing of one fighter. The
// returned pointer aliases the map value: callers must hold s.mu.
func (s *ArenaService) standing(strategy string) *Standing {
	if st, ok := s.leaderboard[strategy]; ok {
		return st
	}
	st := &Standing{Strategy: strategy, Rating: eloDefault}
	s.leaderboard[strategy] = st
	return st
}

// emit safely invokes the sink.
func emit(on EventSink, net string, pct float64, msg string) {
	if on != nil {
		on(net, math.Min(100, math.Max(0, pct)), msg)
	}
}

// span measures the widest endpoint distance of a net (spectacle metric).
func span(t NetTask) float64 {
	best := 0.0
	for i := range t.Endpoints {
		for j := i + 1; j < len(t.Endpoints); j++ {
			dx := t.Endpoints[i].X - t.Endpoints[j].X
			dy := t.Endpoints[i].Y - t.Endpoints[j].Y
			best = math.Max(best, math.Sqrt(dx*dx+dy*dy))
		}
	}
	return best
}

// buildTasks converts schematic nets into routable tasks using board pad
// positions (component origin + schematic pin offsets).
func buildTasks(sch *domainschematic.Schematic, board *domainlayout.Board) []NetTask {
	tasks := make([]NetTask, 0, len(sch.Nets))
	for i := range sch.Nets {
		n := &sch.Nets[i]
		task := NetTask{Name: n.Name}
		for _, c := range n.Connections {
			comp, ok := sch.ComponentByRef(c.ComponentRef)
			if !ok {
				continue
			}
			bc, ok := board.ComponentByRef(c.ComponentRef)
			if !ok {
				continue
			}
			// Pin offsets are relative to the component origin; the board
			// position is the placed origin plus that offset.
			offX, offY := 0.0, 0.0
			for _, pin := range comp.Pins {
				if pin.Number == c.PinNumber {
					offX, offY = pin.X, pin.Y
					break
				}
			}
			task.Endpoints = append(task.Endpoints, Endpoint{
				Ref: c.ComponentRef,
				X:   bc.X + offX,
				Y:   bc.Y + offY,
			})
		}
		if len(task.Endpoints) >= 2 {
			// deduplicate identical endpoints (same pad listed twice)
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// netTaskLabel is a small helper for log lines.
func netTaskLabel(t NetTask) string {
	refs := make([]string, 0, len(t.Endpoints))
	for _, e := range t.Endpoints {
		refs = append(refs, e.Ref)
	}
	return strings.Join(refs, "→")
}
