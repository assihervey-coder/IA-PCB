// impedance.go implements the Differential Impedance Oracle: a closed-form
// edge-coupled microstrip analyser that reports, per net class, every
// differential pair detected in the design (±, _p/_n, P/N naming), its odd /
// even / differential / common-mode impedances, the intra-pair skew, and the
// width or gap corrections needed to hit the class target (90 Ω USB, 100 Ω
// Ethernet/PCIe/LVDS…). Analytic like the Eye Oracle: deterministic,
// instant, stdlib only.
package verificationapp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/geometry"
)

// Default gap assumed for the differential analysis when the pair is not
// routed (or no measurable edge-to-edge spacing exists) — a common tightly
// coupled manufacturing value.
const impDefaultGapMM = 0.15

// DiffPairReport is the verdict for one differential pair.
type DiffPairReport struct {
	NetPlus  string `json:"net_plus"`
	NetMinus string `json:"net_minus"`
	Class    string `json:"net_class"`

	Routed  bool    `json:"routed"` // les deux branches ont du cuivre
	WidthMM float64 `json:"width_mm"`
	GapMM   float64 `json:"gap_mm"` // 0 = non mesurable (hypothèse par défaut)

	LengthPlusMM  float64 `json:"length_plus_mm"`
	LengthMinusMM float64 `json:"length_minus_mm"`
	SkewMM        float64 `json:"skew_mm"`
	SkewPS        float64 `json:"skew_ps"`

	Z0Ohms    float64 `json:"z0_ohms"` // piste simple équivalente
	ZoddOhms  float64 `json:"zodd_ohms"`
	ZevenOhms float64 `json:"zeven_ohms"`
	ZdiffOhms float64 `json:"zdiff_ohms"`
	ZcomOhms  float64 `json:"zcom_ohms"`

	TargetOhms  float64       `json:"target_ohms"`
	ErrorPct    float64       `json:"error_pct"`
	InTolerance bool          `json:"in_tolerance"`
	RecWidthMM  float64       `json:"recommended_width_mm"` // à écart constant
	RecGapMM    float64       `json:"recommended_gap_mm"`   // à largeur constante
	Criticality SICriticality `json:"criticality"`
	Advices     []string      `json:"advices"`
}

// DiffClassReport groups the pairs of one net class with its target.
type DiffClassReport struct {
	Class      string           `json:"class"`
	TargetOhms float64          `json:"target_ohms"`
	Pairs      []DiffPairReport `json:"pairs"`
}

// ImpedanceRequest tunes the analysis: per-class targets in ohms, an
// optional forced gap and the tolerance band (default ±10 %).
type ImpedanceRequest struct {
	Targets       map[string]float64 `json:"targets,omitempty"`
	GapOverrideMM float64            `json:"gap_mm,omitempty"`
	TolerancePct  float64            `json:"tolerance_pct,omitempty"`
}

// ImpedanceStackup documents the FR4 assumption used by the solver.
type ImpedanceStackup struct {
	ErRelative float64 `json:"er_relative"`
	HeightMM   float64 `json:"dielectric_height_mm"`
	TraceMM    float64 `json:"copper_thickness_mm"`
	VPropMMPS  float64 `json:"propagation_mm_per_ps"`
}

// DiffImpedanceResult is the response payload of POST .../impedance.
type DiffImpedanceResult struct {
	Stackup       ImpedanceStackup  `json:"stackup"`
	TolerancePct  float64           `json:"tolerance_pct"`
	AnalyzedPairs int               `json:"analyzed_pairs"`
	InTolerance   int               `json:"in_tolerance_count"`
	WorstPair     string            `json:"worst_pair"`
	Summary       string            `json:"summary"`
	Classes       []DiffClassReport `json:"classes"`
	DurationMS    int64             `json:"duration_ms"`
}

// ImpedanceService runs the differential impedance oracle for a project.
type ImpedanceService struct {
	projects domainproject.Repository
}

// NewImpedanceService builds the oracle.
func NewImpedanceService(projects domainproject.Repository) *ImpedanceService {
	return &ImpedanceService{projects: projects}
}

// coupledImpedances returns the single-ended Z0 and the odd/even mode
// impedances of an edge-coupled microstrip pair (Bogatin-style closed form,
// good to a few percent for s/h in 0.1..4 and w/h in 0.1..3):
//
//	Zodd  = Z0 · (1 − 0.48·e^(−0.96·s/h))
//	Zeven = Z0 · (1 + 0.36·e^(−1.5·s/h))
//	Zdiff = 2·Zodd        Zcom = Zeven / 2
func coupledImpedances(widthMM, gapMM float64) (z0, zodd, zeven, zdiff, zcom float64) {
	z0 = microstripZ0(widthMM, siH)
	sh := math.Max(0.02, gapMM) / siH
	zodd = z0 * (1 - 0.48*math.Exp(-0.96*sh))
	zeven = z0 * (1 + 0.36*math.Exp(-1.5*sh))
	return z0, zodd, zeven, 2 * zodd, zeven / 2
}

// zdiffAt is the scalar the bisection solvers target.
func zdiffAt(widthMM, gapMM float64) float64 {
	_, _, _, zdiff, _ := coupledImpedances(widthMM, gapMM)
	return zdiff
}

// solveWidthForZdiff finds the trace width hitting target Zdiff at a fixed
// gap. Zdiff decreases monotonically with width, so bisection converges.
func solveWidthForZdiff(targetOhms, gapMM float64) float64 {
	lo, hi := 0.05, 5.0
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if zdiffAt(mid, gapMM) > targetOhms {
			lo = mid // trop fin : Zdiff trop haute → élargir
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// solveGapForZdiff finds the edge-to-edge gap hitting target Zdiff at a
// fixed width. Zdiff increases monotonically with the gap.
func solveGapForZdiff(targetOhms, widthMM float64) float64 {
	lo, hi := 0.05, 10.0
	if zdiffAt(widthMM, hi) < targetOhms {
		return hi // même très découplé, la cible n'est pas atteinte
	}
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if zdiffAt(widthMM, mid) < targetOhms {
			lo = mid // trop couplé → écarter
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// pairBaseAndPolarity extracts (base, ±1) from differential naming
// conventions: "USB_D+"/"USB_D-", "USB_DP"/"USB_DN", "TXP"/"TXN", "A_p"/"A_n".
func pairBaseAndPolarity(name string) (string, int, bool) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", 0, false
	}
	switch {
	case strings.HasSuffix(n, "+"):
		return strings.TrimSuffix(n, "+"), 1, true
	case strings.HasSuffix(n, "-"):
		return strings.TrimSuffix(n, "-"), -1, true
	case strings.HasSuffix(n, "_P"), strings.HasSuffix(n, "_p"):
		return n[:len(n)-2], 1, true
	case strings.HasSuffix(n, "_N"), strings.HasSuffix(n, "_n"):
		return n[:len(n)-2], -1, true
	}
	last := n[len(n)-1]
	if len(n) >= 3 && (last == 'P' || last == 'p') {
		return n[:len(n)-1], 1, true
	}
	if len(n) >= 3 && (last == 'N' || last == 'n') {
		return n[:len(n)-1], -1, true
	}
	return "", 0, false
}

// segSegDistanceMM is the minimum distance between two 2D segments
// (0 when they intersect). Uses the geometry helpers.
func segSegDistanceMM(a1, a2, b1, b2 geometry.Point) float64 {
	if geometry.SegmentsIntersect(a1, a2, b1, b2) {
		return 0
	}
	min := geometry.PointSegmentDistance(a1, b1, b2)
	if d := geometry.PointSegmentDistance(a2, b1, b2); d < min {
		min = d
	}
	if d := geometry.PointSegmentDistance(b1, a1, a2); d < min {
		min = d
	}
	if d := geometry.PointSegmentDistance(b2, a1, a2); d < min {
		min = d
	}
	return min
}

// minGapBetweenNets measures the smallest edge-to-edge distance between the
// copper of two nets, same layer only. Returns false when nothing compares.
func minGapBetweenNets(board *domainlayout.Board, netA, netB string) (float64, bool) {
	best := math.MaxFloat64
	found := false
	for i := range board.Tracks {
		ta := &board.Tracks[i]
		if ta.Net != netA || len(ta.Points) < 2 {
			continue
		}
		for j := range board.Tracks {
			tb := &board.Tracks[j]
			if tb.Net != netB || tb.Layer != ta.Layer || len(tb.Points) < 2 {
				continue
			}
			for k := 0; k+1 < len(ta.Points); k++ {
				a1 := geometry.Point{X: ta.Points[k].X, Y: ta.Points[k].Y}
				a2 := geometry.Point{X: ta.Points[k+1].X, Y: ta.Points[k+1].Y}
				for l := 0; l+1 < len(tb.Points); l++ {
					b1 := geometry.Point{X: tb.Points[l].X, Y: tb.Points[l].Y}
					b2 := geometry.Point{X: tb.Points[l+1].X, Y: tb.Points[l+1].Y}
					d := segSegDistanceMM(a1, a2, b1, b2)
					if d < best {
						best, found = d, true
					}
				}
			}
		}
	}
	if !found {
		return 0, false
	}
	return best, true
}

// netLengthMM sums the routed length of a net.
func netLengthMM(board *domainlayout.Board, net string) float64 {
	total := 0.0
	for _, t := range board.TracksForNet(net) {
		total += t.Length()
	}
	return total
}

// trackWidthForNet returns the first positive track width of the net (the
// reference width used for the impedance computation).
func trackWidthForNet(board *domainlayout.Board, cs *domainconstraints.ConstraintSet, net, class string) float64 {
	for _, t := range board.TracksForNet(net) {
		if t.Width > 0 {
			return t.Width
		}
	}
	if cs != nil {
		if v, ok := cs.Value(domainconstraints.RuleMinTrackWidth, class, domainconstraints.AnyLayer); ok && v > 0 {
			return v
		}
	}
	return 0.25
}

// defaultTargetFor guesses the class impedance target from its name
// (USB pairs target 90 Ω, the industry default elsewhere is 100 Ω).
func defaultTargetFor(class string) float64 {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "usb"):
		return 90.0
	case strings.Contains(c, "eth"), strings.Contains(c, "pci"), strings.Contains(c, "lvds"),
		strings.Contains(c, "hdmi"), strings.Contains(c, "sata"):
		return 100.0
	default:
		return 100.0
	}
}

// netInfo is the per-net metadata the pair detector needs.
type netInfo struct {
	Class string
}

// detectDiffPairs groups schematic (or routed) nets into differential
// pairs of the same class.
func detectDiffPairs(netClasses map[string]netInfo) []pairCandidate {
	keys := make([]string, 0, len(netClasses))
	for name := range netClasses {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	type half struct {
		base, name string
	}
	plus := map[string]half{}
	minus := map[string]half{}
	for _, name := range keys {
		base, pol, ok := pairBaseAndPolarity(name)
		if !ok {
			continue
		}
		key := netClasses[name].Class + "|" + strings.ToLower(base)
		h := half{base: base, name: name}
		if pol > 0 {
			if _, exists := plus[key]; !exists {
				plus[key] = h
			}
		} else if _, exists := minus[key]; !exists {
			minus[key] = h
		}
	}

	out := []pairCandidate{}
	for key, p := range plus {
		m, ok := minus[key]
		if !ok {
			continue
		}
		class := netClasses[p.name].Class
		if class == "" {
			class = string(domainschematic.ClassDefault)
		}
		out = append(out, pairCandidate{plus: p.name, minus: m.name, class: class})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].class != out[j].class {
			return out[i].class < out[j].class
		}
		return out[i].plus < out[j].plus
	})
	return out
}

// pairCandidate is one detected differential pair before analysis.
type pairCandidate struct {
	plus, minus, class string
}

// Run analyses every differential pair of the project, grouped by class.
func (s *ImpedanceService) Run(ctx context.Context, projectID string, req ImpedanceRequest) (*DiffImpedanceResult, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("impédance différentielle : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("impédance différentielle : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return nil, errors.New("impédance différentielle : le projet n'a pas de carte")
	}

	// Classes par net : schéma d'abord, sinon déduit des pistes routées.
	netClasses := map[string]netInfo{}
	if sch := p.Schematic(); sch != nil {
		for _, n := range sch.Nets {
			netClasses[n.Name] = netInfo{Class: string(n.Class)}
		}
	}
	netSeen := map[string]bool{}
	for i := range board.Tracks {
		if tr := &board.Tracks[i]; tr.Net != "" && !netSeen[tr.Net] {
			netSeen[tr.Net] = true
			if _, ok := netClasses[tr.Net]; !ok {
				netClasses[tr.Net] = netInfo{Class: string(domainschematic.ClassDefault)}
			}
		}
	}
	if len(netClasses) == 0 {
		return nil, errors.New("impédance différentielle : aucun net à analyser (importez un schéma ou routez la carte)")
	}

	tolerance := req.TolerancePct
	if tolerance <= 0 || tolerance > 50 {
		tolerance = 10
	}

	pairs := detectDiffPairs(netClasses)
	if len(pairs) == 0 {
		return nil, errors.New("impédance différentielle : aucune paire détectée — nommez vos nets X+/X-, X_P/X_N ou XP/XN")
	}

	erEff := 0.475*siEr + 0.525
	vProp := siPropagationC / math.Sqrt(erEff)

	res := &DiffImpedanceResult{
		TolerancePct: tolerance,
		Stackup: ImpedanceStackup{
			ErRelative: siEr, HeightMM: siH, TraceMM: siTraceHeight,
			VPropMMPS: math.Round(vProp*1000) / 1000,
		},
		Classes: []DiffClassReport{},
	}

	classOrder := []string{}
	byClass := map[string][]pairCandidate{}
	for _, pc := range pairs {
		if _, ok := byClass[pc.class]; !ok {
			classOrder = append(classOrder, pc.class)
		}
		byClass[pc.class] = append(byClass[pc.class], pc)
	}
	sort.Strings(classOrder)

	inTol, analyzed, worstErr := 0, 0, 0.0
	worstName := ""
	for _, class := range classOrder {
		target := defaultTargetFor(class)
		if v, ok := req.Targets[class]; ok && v >= 20 && v <= 300 {
			target = v
		}
		rep := DiffClassReport{Class: class, TargetOhms: target, Pairs: []DiffPairReport{}}

		for _, pc := range byClass[class] {
			r := s.analysePair(board, p.Constraints(), pc, target, req, vProp)
			analyzed++
			if r.InTolerance {
				inTol++
			}
			if r.Routed && math.Abs(r.ErrorPct) > worstErr {
				worstErr = math.Abs(r.ErrorPct)
				worstName = pc.plus + " / " + pc.minus
			}
			rep.Pairs = append(rep.Pairs, r)
		}
		res.Classes = append(res.Classes, rep)
	}

	res.AnalyzedPairs = analyzed
	res.InTolerance = inTol
	res.WorstPair = worstName
	switch {
	case analyzed == inTol && analyzed > 0:
		res.Summary = fmt.Sprintf("%d paire(s) analysée(s) : toutes dans la bande de ±%.0f %%.", analyzed, tolerance)
	case inTol == 0:
		res.Summary = fmt.Sprintf("%d paire(s) hors bande ±%.0f %% : ajustez largeur/écart avant fabrication.", analyzed, tolerance)
	default:
		res.Summary = fmt.Sprintf("%d/%d paire(s) dans la bande ±%.0f %% ; la pire est %s.", inTol, analyzed, tolerance, worstName)
	}
	return res, nil
}

// analysePair computes the full differential chain for one pair.
func (s *ImpedanceService) analysePair(board *domainlayout.Board, cs *domainconstraints.ConstraintSet,
	pc pairCandidate, target float64, req ImpedanceRequest, vPropMMPerPS float64) DiffPairReport {

	lenP := netLengthMM(board, pc.plus)
	lenM := netLengthMM(board, pc.minus)
	routed := lenP > 0 && lenM > 0

	width := trackWidthForNet(board, cs, pc.plus, pc.class)
	if width <= 0 {
		width = trackWidthForNet(board, cs, pc.minus, pc.class)
	}
	// Le modèle microstrip sort de son domaine au-delà de ~1,45 mm
	// (une paire différentielle n'utilise jamais une piste aussi large).
	width = math.Min(1.2, math.Max(0.05, width))

	gap, measurable := minGapBetweenNets(board, pc.plus, pc.minus)
	if req.GapOverrideMM > 0 {
		gap, measurable = req.GapOverrideMM, true
	}
	if !measurable || gap <= 0 {
		gap = impDefaultGapMM
	}

	z0, zodd, zeven, zdiff, zcom := coupledImpedances(width, gap)
	errPct := (zdiff - target) / target * 100
	recWidth := solveWidthForZdiff(target, gap)
	recGap := solveGapForZdiff(target, width)

	skewMM := math.Abs(lenP - lenM)
	skewPS := skewMM / vPropMMPerPS

	crit := SIOK
	var advices []string
	if !routed {
		crit = SIWarning
		advices = append(advices, fmt.Sprintf(
			"Paire non routée : hypothèse d'écart %.2f mm. Routez les deux branches pour une mesure réelle.", impDefaultGapMM))
	}
	if math.Abs(errPct) > 20 {
		crit = SICritical
		advices = append(advices, fmt.Sprintf(
			"Zdiff %.0f Ω à %.0f %% de la cible %.0f Ω : passer la largeur à %.2f mm (écart %.2f mm) corrige le tir.",
			zdiff, math.Abs(errPct), target, recWidth, gap))
	} else if math.Abs(errPct) > 10 {
		if crit == SIOK {
			crit = SIWarning
		}
		advices = append(advices, fmt.Sprintf(
			"Hors bande ±10 %% : largeur recommandée %.2f mm à écart constant, ou écart %.2f mm à largeur constante.",
			recWidth, recGap))
	}
	if skewMM > 5.0 {
		advices = append(advices, fmt.Sprintf(
			"Décalage intra-paire %.2f mm (%.0f ps) : serrez le serpentin sur la branche la plus longue.",
			skewMM, skewPS))
		if crit == SIOK {
			crit = SIWarning
		}
	}
	if gap < 2*width && routed {
		advices = append(advices, "Couplage serré : la crosse de mesure TDR est plus fiable sur un coupon de test.")
	}

	return DiffPairReport{
		NetPlus: pc.plus, NetMinus: pc.minus, Class: pc.class,
		Routed: routed, WidthMM: width, GapMM: gap,
		LengthPlusMM: math.Round(lenP*100) / 100, LengthMinusMM: math.Round(lenM*100) / 100,
		SkewMM: math.Round(skewMM*100) / 100, SkewPS: math.Round(skewPS),
		Z0Ohms: math.Round(z0), ZoddOhms: math.Round(zodd), ZevenOhms: math.Round(zeven),
		ZdiffOhms: math.Round(zdiff), ZcomOhms: math.Round(zcom),
		TargetOhms: target, ErrorPct: math.Round(errPct*10) / 10,
		InTolerance: math.Abs(errPct) <= 10, RecWidthMM: math.Round(recWidth*1000) / 1000,
		RecGapMM:    math.Round(recGap*1000) / 1000,
		Criticality: crit, Advices: advices,
	}
}
