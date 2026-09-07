// Package doctor implements the "Design Doctor": a global design review that
// fuses every verification axis of the platform (DRC, ERC, thermal, signal
// integrity, routability/DFM heuristics) into a single 0-100 health score,
// a letter grade, a 5-axis radar and prioritized prescriptions with their
// estimated point gains. Classic EDA tools report each axis separately and
// let the engineer do the triage; the doctor does the triage for them.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
	verificationapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/verification"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
)

// aiProbeTimeout bounds the engine probe and the sandbox rehearsal.
const aiProbeTimeout = 15 * time.Second

// Radar axes (labels kept in French for the UI) and their weights.
const (
	AxeDRC     = "DRC"
	AxeERC     = "ERC"
	AxeSI      = "Intégrité signal"
	AxeThermal = "Thermique"
	AxeRoutage = "Routage & DFM"

	weightDRC   = 0.25
	weightERC   = 0.10
	weightSI    = 0.25
	weightTherm = 0.15
	weightDFM   = 0.25
)

// Prescription is one prioritized recommendation.
type Prescription struct {
	Priority int     `json:"priority"` // 1 = à traiter en premier
	Axe      string  `json:"axe"`
	Title    string  `json:"title"`
	Detail   string  `json:"detail"`
	GainPts  float64 `json:"gain_pts"` // gain de score global estimé
}

// AxisScore is one radar axis.
type AxisScore struct {
	Axe   string  `json:"axe"`
	Score float64 `json:"score"` // 0..100
	Max   float64 `json:"max"`
}

// AISection exposes the state of the RL engine as seen by the doctor.
type AISection struct {
	Reachable   bool   `json:"reachable"`
	Device      string `json:"device,omitempty"`
	ModelLoaded bool   `json:"model_loaded"`
	Strategy    string `json:"strategy,omitempty"` // "rl" si un modèle est chargé, sinon "astar"
}

// Rehearsal is the sandbox routing of the unrouted nets: the RL engine is
// asked to route them WITHOUT persisting anything, giving the doctor a
// quantitative prediction ("3/4 nets routables, 42 mm de cuivre").
type Rehearsal struct {
	Attempted         int     `json:"attempted"`
	Completed         int     `json:"completed"`
	PredictedLengthMM float64 `json:"predicted_length_mm"`
	Strategy          string  `json:"strategy"`
	DurationMS        int64   `json:"duration_ms"`
	Note              string  `json:"note"`
}

// Report is the full response of GET .../doctor.
type Report struct {
	Score         float64        `json:"score"`   // 0..100
	Grade         string         `json:"grade"`   // A+ … D
	Verdict       string         `json:"verdict"` // phrase de synthèse (FR)
	Axes          []AxisScore    `json:"axes"`    // 5 axes du radar
	Prescriptions []Prescription `json:"prescriptions"`
	Metrics       Metrics        `json:"metrics"`
	AI            AISection      `json:"ai"`
	Rehearsal     *Rehearsal     `json:"rehearsal,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

// Metrics exposes the raw measurements behind the scores.
type Metrics struct {
	Components     int     `json:"components"`
	Nets           int     `json:"nets"`
	UnroutedNets   int     `json:"unrouted_nets"`
	Tracks         int     `json:"tracks"`
	TotalLengthMM  float64 `json:"total_length_mm"`
	Vias           int     `json:"vias"`
	UtilizationPct float64 `json:"utilization_pct"`
	MaxTempC       float64 `json:"max_temp_c"`
	SIScorePct     float64 `json:"si_score_pct"`
	DRCViolations  int     `json:"drc_violations"`
	ERCViolations  int     `json:"erc_violations"`
	MinTrackMM     float64 `json:"min_track_mm"`
	MinDrillMM     float64 `json:"min_drill_mm"`
}

// DoctorService aggregates the verification use cases into one review.
// The optional ai port connects the doctor to the Python RL engine: when
// reachable, the report carries the runtime state (device, modèle chargé)
// and a sandbox rehearsal of the unrouted nets.
type DoctorService struct {
	projects domainproject.Repository
	drc      *verificationapp.DRCChecker
	erc      *verificationapp.ERCChecker
	thermal  *verificationapp.ThermalChecker
	si       *verificationapp.SIChecker
	ai       layoutapp.AIService
	log      *slog.Logger
}

// NewDoctorService wires the doctor on top of the existing checkers. ai can
// be nil (moteur désactivé) : l'examen reste complet, sans l'axe IA.
func NewDoctorService(
	projects domainproject.Repository,
	drc *verificationapp.DRCChecker,
	erc *verificationapp.ERCChecker,
	thermal *verificationapp.ThermalChecker,
	si *verificationapp.SIChecker,
	ai layoutapp.AIService,
	log *slog.Logger,
) *DoctorService {
	if log == nil {
		log = slog.Default()
	}
	return &DoctorService{projects: projects, drc: drc, erc: erc, thermal: thermal, si: si, ai: ai, log: log}
}

// Examine runs every axis and assembles the report. A failing axis degrades
// its score without failing the whole examination: an engineer always gets
// a usable verdict.
func (s *DoctorService) Examine(ctx context.Context, projectID string) (*Report, error) {
	start := time.Now()

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("doctor : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("doctor : chargement du projet : %w", err)
	}
	if p.Board() == nil {
		return nil, fmt.Errorf("doctor : le projet %q n'a pas de carte (importez ou placez d'abord)", projectID)
	}

	rep := &Report{Axes: []AxisScore{}, Prescriptions: []Prescription{}}
	s.fillMetrics(p, rep)

	// --- Axes vérifiés par les use cases existants -----------------------
	drcScore, drcDetail := s.axisDRC(ctx, projectID, rep)
	ercScore, ercDetail := s.axisERC(ctx, projectID, rep)
	siScore, siDetail := s.axisSI(ctx, projectID, rep)
	thermScore, thermDetail := s.axisThermal(ctx, projectID, rep)
	dfmScore, dfmDetail := s.axisDFM(p.Board(), rep)

	scoreAxis := func(axe string, v float64, detail string) {
		rep.Axes = append(rep.Axes, AxisScore{Axe: axe, Score: round1(v), Max: 100})
		s.prescribe(rep, axe, v, detail)
	}
	scoreAxis(AxeDRC, drcScore, drcDetail)
	scoreAxis(AxeERC, ercScore, ercDetail)
	scoreAxis(AxeSI, siScore, siDetail)
	scoreAxis(AxeThermal, thermScore, thermDetail)
	scoreAxis(AxeRoutage, dfmScore, dfmDetail)

	// Axe transversal : moteur RL Python (etat + repetition sandbox).
	s.examineAI(ctx, p, rep)

	rep.Score = round1(weightDRC*drcScore + weightERC*ercScore +
		weightSI*siScore + weightTherm*thermScore + weightDFM*dfmScore)
	rep.Grade = gradeOf(rep.Score)
	rep.Verdict = verdictOf(rep)
	sort.SliceStable(rep.Prescriptions, func(i, j int) bool {
		return rep.Prescriptions[i].Priority < rep.Prescriptions[j].Priority
	})
	rep.DurationMS = time.Since(start).Milliseconds()

	s.log.Info("doctor : examen terminé", "project_id", projectID,
		"score", rep.Score, "grade", rep.Grade)
	return rep, nil
}

// --------------------------------------------------------------
// Métriques de base (board + schematic)
// --------------------------------------------------------------

func (s *DoctorService) fillMetrics(p *domainproject.Project, rep *Report) {
	b := p.Board()
	rep.Metrics.Components = len(b.Components)
	rep.Metrics.Tracks = len(b.Tracks)
	rep.Metrics.Vias = len(b.Vias)
	rep.Metrics.TotalLengthMM = round1(b.TotalTrackLength())

	if sch := p.Schematic(); sch != nil {
		rep.Metrics.Nets = len(sch.Nets)
	} else {
		seen := map[string]struct{}{}
		for i := range b.Components {
			for _, pad := range b.Components[i].Footprint.Pads {
				if pad.Net != "" {
					seen[pad.Net] = struct{}{}
				}
			}
		}
		rep.Metrics.Nets = len(seen)
	}

	routed := map[string]struct{}{}
	for _, t := range b.Tracks {
		routed[t.Net] = struct{}{}
	}
	rep.Metrics.UnroutedNets = rep.Metrics.Nets - len(routed)
	if rep.Metrics.UnroutedNets < 0 {
		rep.Metrics.UnroutedNets = 0
	}

	minTrack, minDrill := math.MaxFloat64, math.MaxFloat64
	for i := range b.Tracks {
		if w := b.Tracks[i].Width; w > 0 && w < minTrack {
			minTrack = w
		}
	}
	for i := range b.Vias {
		if d := b.Vias[i].Drill; d > 0 && d < minDrill {
			minDrill = d
		}
	}
	if minTrack == math.MaxFloat64 {
		minTrack = 0
	}
	if minDrill == math.MaxFloat64 {
		minDrill = 0
	}
	rep.Metrics.MinTrackMM = round2(minTrack)
	rep.Metrics.MinDrillMM = round2(minDrill)
	rep.Metrics.UtilizationPct = utilizationOf(b)
}

// utilizationOf measures the share of the outline covered by placed
// component bodies.
func utilizationOf(b *domainlayout.Board) float64 {
	area := b.WidthMM * b.HeightMM
	if area <= 0 {
		return 0
	}
	var used float64
	for i := range b.Components {
		fp := b.Components[i].Footprint
		used += fp.BodyWidthMM * fp.BodyHeightMM
	}
	return round1(math.Min(100, used/area*100))
}

// --------------------------------------------------------------
// Axes de notation
// --------------------------------------------------------------

func (s *DoctorService) axisDRC(ctx context.Context, projectID string, rep *Report) (float64, string) {
	score := 100.0
	detail := "aucune violation"
	if s.drc != nil {
		res, err := s.drc.Run(ctx, projectID)
		if err == nil {
			rep.Metrics.DRCViolations = len(res.Violations)
			switch {
			case len(res.Violations) == 0:
			case len(res.Violations) <= 3:
				score = 90 - float64(len(res.Violations))*4
			default:
				score = math.Max(20, 78-float64(len(res.Violations)-3)*3)
			}
			if !res.Passed {
				detail = fmt.Sprintf("%d violation(s) de règles de conception", len(res.Violations))
			}
		} else {
			score, detail = 60, "DRC indisponible ("+err.Error()+")"
		}
	}
	return score, detail
}

func (s *DoctorService) axisERC(ctx context.Context, projectID string, rep *Report) (float64, string) {
	score := 100.0
	detail := "aucun défaut électrique"
	if s.erc != nil {
		res, err := s.erc.Run(ctx, projectID)
		if err == nil {
			rep.Metrics.ERCViolations = len(res.Violations)
			if len(res.Violations) > 0 {
				score = math.Max(30, 100-float64(len(res.Violations))*12)
				detail = fmt.Sprintf("%d avertissement(s) électrique(s)", len(res.Violations))
			}
		} else {
			score, detail = 60, "ERC indisponible ("+err.Error()+")"
		}
	}
	return score, detail
}

func (s *DoctorService) axisSI(ctx context.Context, projectID string, rep *Report) (float64, string) {
	score := 85.0 // neutre quand aucune piste routée
	detail := "aucune piste analysée"
	if s.si != nil {
		res, err := s.si.Run(ctx, projectID)
		if err == nil {
			rep.Metrics.SIScorePct = round1(res.ScorePct)
			if res.Analyzed > 0 {
				score = res.ScorePct
				detail = fmt.Sprintf("%d net(s) analysé(s), %d critique(s)", res.Analyzed, res.CritCount)
			}
		} else {
			score, detail = 70, "oracle d'œil indisponible ("+err.Error()+")"
		}
	}
	return score, detail
}

func (s *DoctorService) axisThermal(ctx context.Context, projectID string, rep *Report) (float64, string) {
	score := 90.0
	detail := "pas de point chaud significatif"
	if s.thermal != nil {
		res, err := s.thermal.Run(ctx, projectID)
		if err == nil {
			rep.Metrics.MaxTempC = round1(res.MaxTempC)
			// Marge avant la limite typique des composants grand public
			// (85 °C) : 45 °C de marge ou plus = parfait.
			margin := 85 - res.MaxTempC
			score = clampScore(margin / 45 * 100)
			if res.MaxTempC >= 85 {
				detail = fmt.Sprintf("point chaud à %.1f °C : limite composants dépassée", res.MaxTempC)
			} else if res.MaxTempC >= 70 {
				detail = fmt.Sprintf("point chaud à %.1f °C : marge thermique réduite", res.MaxTempC)
			}
		} else {
			score, detail = 70, "simulation thermique indisponible ("+err.Error()+")"
		}
	}
	return score, detail
}

// axisDFM scores routability + manufacturability heuristics.
func (s *DoctorService) axisDFM(b *domainlayout.Board, rep *Report) (float64, string) {
	score := 100.0
	var problems []string

	if rep.Metrics.Nets > 0 {
		unroutedPct := float64(rep.Metrics.UnroutedNets) / float64(rep.Metrics.Nets) * 100
		if unroutedPct > 0 {
			score -= math.Min(50, unroutedPct*0.7)
			problems = append(problems, fmt.Sprintf("%d net(s) non routé(s) (%.0f%%)",
				rep.Metrics.UnroutedNets, unroutedPct))
		}
	}
	if rep.Metrics.MinTrackMM > 0 && rep.Metrics.MinTrackMM < 0.15 {
		score -= 10
		problems = append(problems, fmt.Sprintf("piste de %.2f mm : process avancé requis", rep.Metrics.MinTrackMM))
	}
	if rep.Metrics.MinDrillMM > 0 && rep.Metrics.MinDrillMM < 0.25 {
		score -= 10
		problems = append(problems, fmt.Sprintf("perçage de %.2f mm : forage laser nécessaire", rep.Metrics.MinDrillMM))
	}
	if u := rep.Metrics.UtilizationPct; u > 85 {
		score -= (u - 85) * 1.5
		problems = append(problems, fmt.Sprintf("carte saturée (%.0f%% d'occupation)", u))
	}

	detail := "routabilité et processus standard OK"
	if len(problems) > 0 {
		detail = problems[0]
		for _, p := range problems[1:] {
			detail += " ; " + p
		}
	}
	return clampScore(score), detail
}

// --------------------------------------------------------------
// Ordonnances, note, verdict
// --------------------------------------------------------------

// prescribe turns a weak axis into one actionable prescription.
func (s *DoctorService) prescribe(rep *Report, axe string, score float64, detail string) {
	if score >= 90 {
		return
	}
	gain := func(weight float64) float64 { return round1((100 - score) * weight * 0.8) }
	switch axe {
	case AxeDRC:
		rep.Prescriptions = append(rep.Prescriptions, Prescription{
			Priority: 1, Axe: axe,
			Title:   "Lancer l'Auto-Healer DRC",
			Detail:  detail + " — les largeurs, vias et marges de bord sont réparables en un clic.",
			GainPts: gain(weightDRC),
		})
	case AxeERC:
		rep.Prescriptions = append(rep.Prescriptions, Prescription{
			Priority: 2, Axe: axe,
			Title:   "Corriger les avertissements électriques",
			Detail:  detail + " — revoyez les nets flottants et alimentations non connectées.",
			GainPts: gain(weightERC),
		})
	case AxeSI:
		if rep.Metrics.UnroutedNets > 0 {
			rep.Prescriptions = append(rep.Prescriptions, Prescription{
				Priority: 2, Axe: axe,
				Title:   "Compléter le routage IA",
				Detail:  fmt.Sprintf("%d net(s) restent à router avant l'analyse d'œil.", rep.Metrics.UnroutedNets),
				GainPts: gain(weightSI),
			})
		} else {
			rep.Prescriptions = append(rep.Prescriptions, Prescription{
				Priority: 2, Axe: axe,
				Title:   "Élargir ou raccourcir les nets critiques",
				Detail:  detail + " — ciblez d'abord le pire œil signalé par l'oracle.",
				GainPts: gain(weightSI),
			})
		}
	case AxeThermal:
		rep.Prescriptions = append(rep.Prescriptions, Prescription{
			Priority: 3, Axe: axe,
			Title:   "Étaler les sources de chaleur",
			Detail:  detail + " — éloignez les composants dissipatifs ou ajoutez des planes cuivre.",
			GainPts: gain(weightTherm),
		})
	case AxeRoutage:
		rep.Prescriptions = append(rep.Prescriptions, Prescription{
			Priority: 2, Axe: axe,
			Title:   "Améliorer la routabilité / le process",
			Detail:  detail + " — terminez le routage et évitez les géométries hors process standard.",
			GainPts: gain(weightDFM),
		})
	}
}

// examineAI sonde le moteur RL Python et, quand des nets restent a router,
// lance une repetition en sandbox : le moteur propose un routage qui N'EST
// PAS applique, mais le rapport chiffre ce qu'un clic sur /route produirait.
func (s *DoctorService) examineAI(ctx context.Context, p *domainproject.Project, rep *Report) {
	if s.ai == nil {
		return
	}
	infoCtx, cancel := context.WithTimeout(ctx, aiProbeTimeout)
	info, err := s.ai.EngineInfo(infoCtx)
	cancel()
	if err != nil {
		rep.AI = AISection{Reachable: false}
		return
	}
	strategy := "astar"
	if info.ModelLoaded {
		strategy = "rl"
	}
	rep.AI = AISection{
		Reachable:   true,
		Device:      info.Device,
		ModelLoaded: info.ModelLoaded,
		Strategy:    strategy,
	}

	if rep.Metrics.UnroutedNets <= 0 {
		return
	}
	unrouted := s.unroutedNets(p)
	if len(unrouted) == 0 {
		return
	}

	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}
	started := time.Now()
	outcome, err := s.ai.RouteBoard(ctx, p.Board(), unrouted, cs, strategy, nil)
	if err != nil {
		rep.Rehearsal = &Rehearsal{
			Attempted: len(unrouted), Strategy: strategy,
			DurationMS: time.Since(started).Milliseconds(),
			Note:       "repetition indisponible : " + err.Error(),
		}
		return
	}
	completed := 0
	var length float64
	for _, nr := range outcome.Nets {
		if nr.Completed {
			completed++
		}
		length += nr.LengthMM
	}
	rep.Rehearsal = &Rehearsal{
		Attempted:         len(unrouted),
		Completed:         completed,
		PredictedLengthMM: round1(length),
		Strategy:          strategy,
		DurationMS:        time.Since(started).Milliseconds(),
		Note:              fmt.Sprintf("prediction hors persist : %d/%d net(s) routable(s) via %s", completed, len(unrouted), strategy),
	}

	// Ordonnance chiffrée quand le moteur peut compléter le routage.
	if completed > 0 {
		unroutedPct := float64(rep.Metrics.UnroutedNets) / float64(maxInt(rep.Metrics.Nets, 1)) * 100
		penalty := math.Min(50, unroutedPct*0.7) // même modèle que l'axe DFM
		rep.Prescriptions = append(rep.Prescriptions, Prescription{
			Priority: 1, Axe: AxeRoutage,
			Title: "Compléter le routage avec le moteur IA (" + strategy + ")",
			Detail: fmt.Sprintf("Le moteur %s peut router %d/%d net(s) restant(s) en ~%.0f mm de cuivre (répétition sandbox, rien n'a été appliqué).",
				strategy, completed, len(unrouted), length),
			GainPts: round1(penalty * weightDFM * 0.8),
		})
	}
}

// unroutedNets lists the nets of the design that have no track on the board.
func (s *DoctorService) unroutedNets(p *domainproject.Project) []domainschematic.Net {
	var nets []domainschematic.Net
	if sch := p.Schematic(); sch != nil && len(sch.Nets) > 0 {
		nets = append([]domainschematic.Net(nil), sch.Nets...)
	} else {
		// Sans schéma, la dérivation pad->net est ambiguë : la répétition
		// attend un import de netlist (comportement documenté).
		return nil
	}
	routed := map[string]struct{}{}
	for _, t := range p.Board().Tracks {
		routed[t.Net] = struct{}{}
	}
	out := nets[:0]
	for _, n := range nets {
		if _, ok := routed[n.Name]; !ok {
			out = append(out, n)
		}
	}
	return out
}

func gradeOf(score float64) string {
	switch {
	case score >= 93:
		return "A+"
	case score >= 85:
		return "A"
	case score >= 75:
		return "B"
	case score >= 62:
		return "C"
	default:
		return "D"
	}
}

func verdictOf(rep *Report) string {
	switch {
	case rep.Score >= 93:
		return "Conception exemplaire : prête pour la fabrication en série."
	case rep.Score >= 85:
		return "Très bonne conception : quelques finitions avant le prototypage."
	case rep.Score >= 75:
		return "Conception solide mais perfectible : suivez les ordonnances prioritaires."
	case rep.Score >= 62:
		return "Conception fragile : plusieurs axes demandent un travail avant fabrication."
	default:
		return "Conception à risque : ne fabriquez pas avant correction des points bloquants."
	}
}

// --------------------------------------------------------------
// Utilitaires
// --------------------------------------------------------------

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampScore(v float64) float64 { return math.Min(100, math.Max(0, v)) }
func round1(v float64) float64     { return math.Round(v*10) / 10 }
func round2(v float64) float64     { return math.Round(v*100) / 100 }
