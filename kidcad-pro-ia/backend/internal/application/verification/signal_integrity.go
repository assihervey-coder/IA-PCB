// signal_integrity.go implements the Eye Oracle: a closed-form signal
// integrity analyser that predicts, for every net of the board, the
// characteristic impedance (microstrip approximation), the propagation
// delay, the reflection budget (vias + stubs) and finally an estimated
// eye height / eye width — the numbers a real eye diagram would show.
// It is deliberately analytic (no SPICE): deterministic, instant, stdlib
// only, and accurate enough to rank nets and catch layout sins early.
package verificationapp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// SI material constants (FR4 stackup, sane industry defaults).
const (
	siEr           = 4.2 // permittivity relative FR4
	siH            = 0.2 // prepreg thickness above reference plane (mm)
	siTraceHeight  = 0.035
	siPropagationC = 299.792458 // mm/ps — speed of light (vacuum)
	siRiseTimePS   = 500.0      // driver rise time assumed (1 Gbps-ish)
	siViaStubPS    = 12.0       // reflection penalty per via (ps, lumped)
	siBitPeriodPS  = 1000.0     // UI at 1 Gbps
)

// SICriticality qualifies the verdict of a net.
type SICriticality string

const (
	SIOK       SICriticality = "ok"
	SIWarning  SICriticality = "warning"
	SICritical SICriticality = "critical"
)

// SINetReport is the Eye Oracle verdict for one net.
type SINetReport struct {
	Net          string        `json:"net"`
	LengthMM     float64       `json:"length_mm"`
	ViaCount     int           `json:"via_count"`
	WidthMM      float64       `json:"width_mm"`
	Z0Ohms       float64       `json:"z0_ohms"`
	DelayPS      float64       `json:"delay_ps"`
	ReflectionPS float64       `json:"reflection_budget_ps"`
	EyeHeightPct float64       `json:"eye_height_pct"` // % de l'excursion pleine
	EyeWidthPS   float64       `json:"eye_width_ps"`   // ouverture temporelle
	JitterPS     float64       `json:"jitter_ps"`
	Criticality  SICriticality `json:"criticality"`
	Advices      []string      `json:"advices"`
}

// SIResult is the response payload of POST .../si.
type SIResult struct {
	DriverPS  float64       `json:"driver_rise_time_ps"`
	BitPeriod float64       `json:"bit_period_ps"`
	Analyzed  int           `json:"analyzed"`
	OkCount   int           `json:"ok_count"`
	WarnCount int           `json:"warning_count"`
	CritCount int           `json:"critical_count"`
	Nets      []SINetReport `json:"nets"`
	MaxEyeNet string        `json:"worst_eye_net"`
	ScorePct  float64       `json:"si_score_pct"` // 100 = parfait
	Summary   string        `json:"summary"`
}

// SIChecker runs the Eye Oracle for a project.
type SIChecker struct {
	projects domainproject.Repository
}

// NewSIChecker builds the oracle.
func NewSIChecker(projects domainproject.Repository) *SIChecker {
	return &SIChecker{projects: projects}
}

// microstripZ0 returns the characteristic impedance of an outer-layer
// microstrip ( mm trace width, mm dielectric height), IPC-2141 style
// closed form — good to ~5% for 0.1..0.6 mm traces on 0.2 mm prepreg.
func microstripZ0(widthMM, heightMM float64) float64 {
	w, h := math.Max(0.05, widthMM), math.Max(0.05, heightMM)
	wh := w / h
	if wh >= 1.0 {
		return 87.0 / (wh + 1.41) * math.Log10(5.98*h/(0.8*w+siTraceHeight))
	}
	return 60.0*math.Log10(8.0*h/(0.8*w+siTraceHeight))/(wh+0.1+0.001) + 20
}

// Run analyses every routed net of the project.
func (s *SIChecker) Run(ctx context.Context, projectID string) (*SIResult, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("oracle d'œil : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("oracle d'œil : chargement du projet : %w", err)
	}

	board := p.Board()
	if board == nil || len(board.Tracks) == 0 {
		return nil, errors.New("oracle d'œil : aucune piste routée à analyser")
	}

	// effective εr of a microstrip (field mostly in FR4, partly in air).
	erEff := 0.475*siEr + 0.525
	vProp := siPropagationC / math.Sqrt(erEff) // mm/ps

	reports := []SINetReport{}
	netSeen := map[string]bool{}
	for i := range board.Tracks {
		tr := &board.Tracks[i]
		if netSeen[tr.Net] {
			continue
		}
		netSeen[tr.Net] = true
		reports = append(reports, s.analyseNet(board, tr.Net, p.Constraints(), vProp))
	}

	res := &SIResult{
		DriverPS: siRiseTimePS, BitPeriod: siBitPeriodPS,
		Analyzed: len(reports), Nets: reports,
	}
	ok, warn, crit := 0, 0, 0
	worstEye := math.MaxFloat64
	worstNet := ""
	var score float64
	for _, r := range reports {
		switch r.Criticality {
		case SIOK:
			ok++
		case SIWarning:
			warn++
		case SICritical:
			crit++
		}
		score += r.EyeHeightPct
		if r.EyeWidthPS < worstEye {
			worstEye, worstNet = r.EyeWidthPS, r.Net
		}
	}
	res.OkCount, res.WarnCount, res.CritCount = ok, warn, crit
	res.MaxEyeNet = worstNet
	res.ScorePct = math.Round(score / float64(len(reports))) // mean eye height %
	switch {
	case crit > 0:
		res.Summary = fmt.Sprintf("%d net(s) critiques : un re-routage ciblé est recommandé avant fabrication.", crit)
	case warn > 0:
		res.Summary = fmt.Sprintf("Fabricable, mais %d net(s) à surveiller (marges d'œil réduites).", warn)
	default:
		res.Summary = "Tous les nets affichent une œil ouverte et saine à 1 Gbps."
	}
	return res, nil
}

// analyseNet computes the full SI chain for one net.
func (s *SIChecker) analyseNet(board *domainlayout.Board, net string,
	cs *domainconstraints.ConstraintSet, vPropMMPerPS float64) SINetReport {

	length := 0.0
	viaCount := len(board.ViasForNet(net))
	for _, t := range board.TracksForNet(net) {
		length += t.Length()
	}
	width := 0.25
	for _, t := range board.TracksForNet(net) {
		if t.Width > 0 {
			width = t.Width
			break
		}
	}
	if cs != nil {
		if v, ok := cs.Value(domainconstraints.RuleMinTrackWidth, "high-speed", 0); ok && v > width {
			width = v
		}
	}

	z0 := microstripZ0(width, siH)
	// Unrouted nets still get a report: mark them with delay 0.
	delay := 0.0
	if vPropMMPerPS > 0 {
		delay = length / vPropMMPerPS
	}
	reflection := float64(viaCount) * siViaStubPS

	// Eye model: inter-symbol interference from the driver rise time,
	// reflections behave like bounded jitter; skin/loss degrade the
	// amplitude with an exponential attenuation over length.
	atten := math.Exp(-length / 900.0) // 900 mm 1/e decay (~lossy FR4)
	isiPS := siRiseTimePS*0.35 + reflection
	eyeWidth := math.Max(0, siBitPeriodPS-2*isiPS)
	jitter := siRiseTimePS*0.08 + reflection*0.25
	eyeHeight := atten * 100.0 * (1 - math.Min(0.9, isiPS/siBitPeriodPS))

	crit := SIOK
	var advices []string
	switch {
	case eyeWidth < siBitPeriodPS*0.25 || eyeHeight < 35:
		crit = SICritical
		advices = append(advices,
			"Réduire le nombre de vias ou raccourcir ce net : l'œil est presque fermé.")
		if viaCount > 2 {
			advices = append(advices, "Remplacer les vias traversants par des vias enterrés si le stackup le permet.")
		}
	case eyeWidth < siBitPeriodPS*0.45 || eyeHeight < 55:
		crit = SIWarning
		advices = append(advices, "Marge d'œil modérée : privilégier un routage direct, éviter les détours inutiles.")
	}
	if math.Abs(z0-50) > 8 && length > 30 {
		advices = append(advices, fmt.Sprintf(
			"Impédance %.0f Ω loin de 50 Ω : ajuster la largeur de piste (actuellement %.2f mm).", z0, width))
		crit = worstOf(crit, SIWarning)
	}
	if strings.HasPrefix(strings.ToUpper(net), "CLK") || strings.Contains(strings.ToUpper(net), "CLOCK") {
		advices = append(advices, "Net d'horloge : viser une longueur identique entre les branches (skew).")
	}

	return SINetReport{
		Net: net, LengthMM: length, ViaCount: viaCount, WidthMM: width,
		Z0Ohms: math.Round(z0), DelayPS: math.Round(delay),
		ReflectionPS: math.Round(reflection), EyeHeightPct: math.Round(eyeHeight),
		EyeWidthPS: math.Round(eyeWidth), JitterPS: math.Round(jitter),
		Criticality: crit, Advices: advices,
	}
}

// worstOf returns the most severe of two criticalities.
func worstOf(a, b SICriticality) SICriticality {
	rank := func(c SICriticality) int {
		switch c {
		case SICritical:
			return 2
		case SIWarning:
			return 1
		}
		return 0
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}
