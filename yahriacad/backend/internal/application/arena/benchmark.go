// Benchmark A* contre RL : le même carnet de nets, deux moteurs —
// l'A* déterministe local et le routeur RL (PyTorch) du moteur IA.
// Le rapport réutilise les cartes de performance de l'arène, met à jour
// l'ELO des deux camps et raconte le combat en français.
package arena

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// ErrModelNotLoaded is returned by Benchmark when the AI engine has no RL
// checkpoint loaded. The REST layer maps it onto 503 with a didactic
// message (entraîner un modèle, puis le recharger à chaud).
var ErrModelNotLoaded = errors.New("modèle RL non chargé")

// AIEngine is the minimal port toward the AI engine used by the RL
// fighter. layoutapp.AIService satisfies it structurally.
type AIEngine interface {
	EngineInfo(ctx context.Context) (layoutapp.EngineInfo, error)
	RouteBoard(ctx context.Context, b *domainlayout.Board, nets []domainschematic.Net,
		cs *domainconstraints.ConstraintSet, strategy string,
		onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error)
}

// BenchmarkReport is the response of POST .../arena/benchmark: the same
// netlist routed by the local A* and by the RL engine, head to head.
type BenchmarkReport struct {
	ProjectID string        `json:"project_id"`
	Nets      []string      `json:"nets"`
	Astar     FighterResult `json:"astar"`
	RL        FighterResult `json:"rl"`
	Winner    string        `json:"winner"` // "astar" | "rl" | "draw"
	Margin    float64       `json:"margin"` // écart de score
	Elo       [2]Standing   `json:"elo"`    // [astar, rl] après mise à jour
	Log       []string      `json:"log"`    // récit du benchmark (FR)
	At        time.Time     `json:"at"`
}

// AttachAI wires the AI engine port onto the arena (no-op when nil). The
// benchmark stays unavailable until this is called.
func (s *ArenaService) AttachAI(ai AIEngine) {
	if ai == nil {
		return
	}
	s.ai = ai
}

// Benchmark loads the project, routes the identical net list twice — once
// with the local A*, once with the RL engine (strategy="rl") — scores both
// cards with the arena formula, updates the ELO and narrates the fight.
func (s *ArenaService) Benchmark(ctx context.Context, projectID string, onEvent EventSink) (*BenchmarkReport, error) {
	if s.ai == nil {
		return nil, errors.New("benchmark : moteur IA non attaché")
	}
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("benchmark : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("benchmark : chargement du projet : %w", err)
	}
	board := p.Board()
	sch := p.Schematic()
	if board == nil || sch == nil || len(sch.Nets) == 0 {
		return nil, errors.New("benchmark : aucun netlist exploitable (importez un schéma et un placement d'abord)")
	}

	// Le routeur RL doit être chargé : sans checkpoint, un benchmark
	// A* contre A* n'a aucun sens — on refuse poliment.
	info, err := s.ai.EngineInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("benchmark : moteur IA injoignable : %w", err)
	}
	if !info.ModelLoaded {
		return nil, fmt.Errorf(
			"benchmark : %w (entraînez un checkpoint avec « make train-router » puis POST /api/v1/ai/model/reload)",
			ErrModelNotLoaded)
	}

	tasks := buildTasks(sch, board)
	if len(tasks) == 0 {
		return nil, errors.New("benchmark : aucune broche localisable sur la carte : placez les composants d'abord")
	}
	sort.SliceStable(tasks, func(i, j int) bool { return span(tasks[i]) > span(tasks[j]) })
	if len(tasks) > maxNetsPerMatch {
		tasks = tasks[:maxNetsPerMatch]
	}

	report := &BenchmarkReport{
		ProjectID: projectID,
		At:        time.Now().UTC(),
		Log: []string{
			fmt.Sprintf("🧪 Benchmark lancé : %d nets, même carnet pour les deux moteurs.", len(tasks)),
			fmt.Sprintf("🥊 astar (local, déterministe) contre rl (checkpoint %s, device %s).",
				baseName(info.Version), info.Device),
		},
	}

	// Camp 1 : A* local (réutilise le fighter de l'arène).
	report.Astar = runFighter("astar", tasks, board, func(net string, pct float64, msg string) {
		emit(onEvent, net, pct*0.5, msg)
	})

	// Camp 2 : RL via le moteur IA. Le carnet est converti en nets du
	// schéma (connexions incluses) ; les contraintes du projet (classes de
	// nets incluses) accompagnent la requête, comme pour un routage normal.
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}
	report.RL = s.runRLFighter(ctx, board, sch, tasks, cs, func(net string, pct float64, msg string) {
		emit(onEvent, net, 50+pct*0.5, msg)
	})

	// Narration + verdict.
	report.Nets = make([]string, len(tasks))
	for i, t := range tasks {
		report.Nets[i] = t.Name
	}
	report.Log = append(report.Log,
		fmt.Sprintf("📊 astar : %d/%d nets, %.1f mm, %d via(s), %d ms.",
			report.Astar.Completed, len(tasks), report.Astar.TotalLengthMM,
			report.Astar.ViaCount, report.Astar.DurationMS),
		fmt.Sprintf("📊 rl : %d/%d nets, %.1f mm, %d via(s), %d ms.",
			report.RL.Completed, len(tasks), report.RL.TotalLengthMM,
			report.RL.ViaCount, report.RL.DurationMS),
	)

	astarScore, rlScore := report.Astar.Score, report.RL.Score
	switch {
	case math.Abs(astarScore-rlScore) < 0.5:
		report.Winner = "draw"
		report.Log = append(report.Log, "🤝 Match nul ! L'algorithme classique et le réseau de neurones font jeu égal.")
	case astarScore > rlScore:
		report.Winner = "astar"
		report.Log = append(report.Log, "🏆 astar l'emporte — l'heuristique garde l'avantage sur cette carte.")
	default:
		report.Winner = "rl"
		report.Log = append(report.Log, "🏆 rl l'emporte — le modèle entraîné trace des routes plus courtes.")
	}
	report.Margin = math.Abs(astarScore - rlScore)

	s.updateEloPair("astar", "rl", report.Winner)
	report.Elo = [2]Standing{*s.standing("astar"), *s.standing("rl")}
	report.Log = append(report.Log,
		fmt.Sprintf("📈 ELO mis à jour : astar %.0f, rl %.0f.", report.Elo[0].Rating, report.Elo[1].Rating))

	s.log.Info("benchmark A* vs RL terminé", "project_id", projectID,
		"winner", report.Winner, "nets", len(tasks),
		"astar_score", astarScore, "rl_score", rlScore)
	return report, nil
}

// runRLFighter routes every task through the AI engine with strategy
// "rl" and converts the per-net results into an arena performance card.
// The project constraint set rides along so net-class rules (width,
// clearance) apply to the RL fighter exactly like a normal routing job.
func (s *ArenaService) runRLFighter(ctx context.Context, board *domainlayout.Board,
	sch *domainschematic.Schematic, tasks []NetTask, cs *domainconstraints.ConstraintSet,
	onNet func(net string, pct float64, msg string)) FighterResult {

	card := FighterResult{Strategy: "rl"}

	byName := make(map[string]domainschematic.Net, len(sch.Nets))
	for i := range sch.Nets {
		byName[sch.Nets[i].Name] = sch.Nets[i]
	}
	nets := make([]domainschematic.Net, 0, len(tasks))
	for _, t := range tasks {
		if n, ok := byName[t.Name]; ok {
			nets = append(nets, n)
		}
	}

	start := time.Now()
	outcome, err := s.ai.RouteBoard(ctx, board, nets, cs, "rl", nil)
	card.DurationMS = time.Since(start).Milliseconds()
	if err != nil || outcome == nil {
		// Forfait : le moteur n'a pas pu router — carte honnête à zéro.
		card.NetLog = append(card.NetLog, fmt.Sprintf("  moteur IA : forfait (%v)", err))
		card.Score = 0
		return card
	}

	byResult := make(map[string]layoutapp.NetRoute, len(outcome.Nets))
	for _, nr := range outcome.Nets {
		byResult[nr.Net] = nr
	}
	for i, t := range tasks {
		nr, ok := byResult[t.Name]
		line := fmt.Sprintf("  %s [%s] : forfait (aucun résultat du moteur)", t.Name, netTaskLabel(t))
		if ok {
			card.TotalLengthMM += nr.LengthMM
			card.ViaCount += len(nr.Vias)
			if nr.Completed {
				card.Completed++
			} else {
				card.Failed++
			}
			line = fmt.Sprintf("  %s [%s] : %.1f mm, %d via(s), %s",
				t.Name, netTaskLabel(t), nr.LengthMM, len(nr.Vias), ternary(nr.Completed, "connecté", "incomplet"))
		} else {
			card.Failed++
		}
		card.NetLog = append(card.NetLog, line)
		if onNet != nil {
			onNet(t.Name, float64(i+1)/float64(len(tasks))*100, line)
		}
	}

	// Même formule que runFighter (collisions inconnues côté moteur = 0) :
	// compléter un net domine, puis longueur, puis durée en tie-breaker.
	card.Score = float64(card.Completed)*100 -
		card.TotalLengthMM*0.05 -
		float64(card.Collisions)*40 -
		float64(card.DurationMS)*0.01
	card.Score = math.Round(card.Score*100) / 100
	return card
}

// updateEloPair applies a pairwise ELO update between two fighters.
func (s *ArenaService) updateEloPair(a, b, winner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sa := s.standing(a)
	sb := s.standing(b)

	expected := 1.0 / (1.0 + math.Pow(10, (sb.Rating-sa.Rating)/400.0))
	actual := 0.5
	switch winner {
	case a:
		actual = 1.0
	case b:
		actual = 0.0
	}
	sa.Rating += eloK * (actual - expected)
	sb.Rating += eloK * ((1 - actual) - (1 - expected))
	sa.Matches, sb.Matches = sa.Matches+1, sb.Matches+1
	switch winner {
	case a:
		sa.Wins, sb.Losses = sa.Wins+1, sb.Losses+1
	case b:
		sb.Wins, sa.Losses = sb.Wins+1, sa.Losses+1
	default:
		sa.Draws, sb.Draws = sa.Draws+1, sb.Draws+1
	}
}

// baseName trims a version string to its short form (log readability).
func baseName(v string) string {
	if v == "" {
		return "checkpoint chargé"
	}
	return v
}

// ternary is a tiny readability helper for log lines.
func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
