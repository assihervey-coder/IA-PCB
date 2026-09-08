// AutoFixService — the "DRC Auto-Healer". Where classic EDA tools stop at
// listing violations, the healer proposes a concrete repair for each of them,
// applies the safe ones (width bumps, via enlargement, edge nudging),
// persists the healed board and re-runs the DRC to report the real
// before/after delta. Every fix carries a confidence score so the UI can
// sort repairs by trust. dry-run mode returns the proposals untouched.
package verificationapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// Fix action labels returned to the client.
const (
	FixWidenTrack = "widen_track"
	FixEnlargeVia = "enlarge_via"
	FixNudgeEdge  = "nudge_edge"
	FixRipupNet   = "ripup_reroute" // jamais appliqué automatiquement : suggestion
)

// Internal tuning.
const geometryEps = 1e-9

// DRCFix is one repair proposal (and its application outcome).
type DRCFix struct {
	Code       string  `json:"code"`
	Action     string  `json:"action"`
	Message    string  `json:"message"`
	Confidence float64 `json:"confidence"`
	Applied    bool    `json:"applied"`
	Target     string  `json:"target"` // ref de composant, net, ou "board"
}

// AutoFixResult is the response of POST .../drc/autofix.
type AutoFixResult struct {
	DryRun           bool     `json:"dry_run"`
	PassedBefore     bool     `json:"passed_before"`
	PassedAfter      bool     `json:"passed_after"`
	ViolationsBefore int      `json:"violations_before"`
	ViolationsAfter  int      `json:"violations_after"`
	Fixed            int      `json:"fixed"`
	Remaining        []string `json:"remaining"` // messages des violations non réparables
	Fixes            []DRCFix `json:"fixes"`
	BoardChanged     bool     `json:"board_changed"`
	DurationMS       int64    `json:"duration_ms"`
}

// AutoFixService heals DRC violations.
type AutoFixService struct {
	projects domainproject.Repository
	drc      *DRCChecker
	log      *slog.Logger
}

// NewAutoFixService wires the healer on top of the DRC checker (reuse of the
// spatial-hash run) and the project repository.
func NewAutoFixService(projects domainproject.Repository, drc *DRCChecker, log *slog.Logger) *AutoFixService {
	if log == nil {
		log = slog.Default()
	}
	return &AutoFixService{projects: projects, drc: drc, log: log}
}

// Run executes diagnose → propose → (apply) → re-check. It never returns a
// non-nil error for a healable situation: a board without violations simply
// yields an empty fix list.
func (s *AutoFixService) Run(ctx context.Context, projectID string, dryRun bool) (*AutoFixResult, error) {
	start := time.Now()

	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.Board() == nil {
		return nil, fmt.Errorf("autofix : le projet %q n'a pas de carte (importez ou placez d'abord)", projectID)
	}
	// IMPORTANT : le service ne travaille QUE sur un clone profond. L'adaptateur
	// mémoire partage les pointeurs board/constraints entre copies : muter
	// l'original polluerait le dépôt même en mode dry-run.
	board := cloneBoardForFix(p.Board())
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	before, err := s.drc.Run(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("autofix : DRC initial : %w", err)
	}

	res := &AutoFixResult{
		DryRun:           dryRun,
		PassedBefore:     before.Passed,
		ViolationsBefore: len(before.Violations),
		Fixes:            []DRCFix{},
		Remaining:        []string{},
	}

	if before.Passed {
		res.PassedAfter = true
		res.DurationMS = time.Since(start).Milliseconds()
		return res, nil
	}

	netClasses := netClassMap(p.Schematic())
	boardChanged := false

	// Les correctifs sont appliqués par famille, de la plus sûre à la plus
	// incertaine ; une seule passe suffit car chaque famille répare sa
	// propre violation de façon déterministe.
	for _, v := range before.Violations {
		switch v.Code {
		case CodeDRCTrackWidth:
			fix, ok := healTrackWidth(board, cs, netClasses, v)
			if ok {
				boardChanged = true
			}
			s.appendFix(res, fix)

		case CodeDRCViaDiameter, CodeDRCDrill, CodeDRCAnnular:
			fix, ok := healVia(board, cs, netClasses, v)
			if ok {
				boardChanged = true
			}
			s.appendFix(res, fix)

		case CodeDRCEdgeClearance:
			fix, ok := healEdgeViolation(board, cs, netClasses, v)
			if ok {
				boardChanged = true
			}
			s.appendFix(res, fix)

		case CodeDRCClearance:
			// Correction impossible sans re-routage : rip-up & re-route du
			// net fautif via le module layout (jamais appliqué ici).
			res.Fixes = append(res.Fixes, DRCFix{
				Code:       v.Code,
				Action:     FixRipupNet,
				Message:    fmt.Sprintf("Isolation %q : relancez le routage IA du net (rip-up & re-route).", v.Net),
				Confidence: 0,
				Applied:    false,
				Target:     v.Net,
			})
			res.Remaining = append(res.Remaining, v.Message)

		default:
			res.Remaining = append(res.Remaining, v.Message)
		}
	}

	if dryRun || !boardChanged {
		// Aucune persistance : le clone réparé est simplement abandonné.
		res.DurationMS = time.Since(start).Milliseconds()
		return res, nil
	}

	// Persistance de la carte réparée puis contre-vérification réelle.
	p.SetBoard(board)
	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("autofix : persistance de la carte réparée : %w", err)
	}
	res.BoardChanged = true

	after, err := s.drc.Run(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("autofix : DRC de contrôle : %w", err)
	}
	res.PassedAfter = after.Passed
	res.ViolationsAfter = len(after.Violations)
	res.Fixed = res.ViolationsBefore - res.ViolationsAfter
	for _, v := range after.Violations {
		res.Remaining = append(res.Remaining, v.Message)
	}

	s.log.Info("autofix terminé", "project_id", projectID,
		"before", res.ViolationsBefore, "after", res.ViolationsAfter,
		"fixed", res.Fixed, "dry_run", dryRun)
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

// appendFix registers a fix (nil-safe).
func (s *AutoFixService) appendFix(res *AutoFixResult, f *DRCFix) {
	if f != nil {
		res.Fixes = append(res.Fixes, *f)
	}
}

// load fetches the project, mapping not-found onto the sentinel.
func (s *AutoFixService) load(ctx context.Context, projectID string) (*domainproject.Project, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("autofix : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("autofix : chargement du projet : %w", err)
	}
	return p, nil
}

// --------------------------------------------------------------
// Réparateurs par famille de règle (fonctions pures sur l'agrégat)
// --------------------------------------------------------------

// healTrackWidth widens every offending track of the net to the rule value.
// Confidence 0.95 : élargir une piste ne rompt jamais sa connectivité.
func healTrackWidth(board *domainlayout.Board, cs *domainconstraints.ConstraintSet,
	netClasses map[string]domainschematic.NetClass, v DRCViolation) (*DRCFix, bool) {
	minW, ok := ruleValue(cs, domainconstraints.RuleMinTrackWidth, classOf(netClasses, v.Net), v.Layer)
	if !ok {
		return nil, false
	}
	changed := 0
	for i := range board.Tracks {
		t := &board.Tracks[i]
		if t.Net != v.Net || t.Layer != v.Layer {
			continue
		}
		if t.Width+geometryEps < minW {
			t.Width = minW
			changed++
		}
	}
	if changed == 0 {
		return nil, false
	}
	return &DRCFix{
		Code:       CodeDRCTrackWidth,
		Action:     FixWidenTrack,
		Message:    fmt.Sprintf("%d piste(s) du net %q élargie(s) à %.3f mm.", changed, v.Net, minW),
		Confidence: 0.95,
		Applied:    true,
		Target:     v.Net,
	}, true
}

// healVia enlarges the via so diameter, drill and annular ring all satisfy
// their rules. Confidence 0.9 : le via grossit sur place, sans toucher au
// routage.
func healVia(board *domainlayout.Board, cs *domainconstraints.ConstraintSet,
	netClasses map[string]domainschematic.NetClass, v DRCViolation) (*DRCFix, bool) {
	cls := classOf(netClasses, v.Net)
	minD, hasD := ruleValue(cs, domainconstraints.RuleMinViaDiameter, cls, v.Layer)
	minDrill, hasDrill := ruleValue(cs, domainconstraints.RuleMinDrill, cls, v.Layer)
	minRing, hasRing := ruleValue(cs, domainconstraints.RuleMinAnnularRing, cls, v.Layer)
	if !hasD && !hasDrill && !hasRing {
		return nil, false
	}

	changed := 0
	for i := range board.Vias {
		via := &board.Vias[i]
		if via.Net != v.Net || via.X != v.X || via.Y != v.Y {
			continue
		}
		diameter, drill := via.Diameter, via.Drill
		if hasDrill && drill+geometryEps < minDrill {
			drill = minDrill
		}
		if hasRing && (diameter-drill)/2+geometryEps < minRing {
			diameter = drill + 2*minRing
		}
		if hasD && diameter+geometryEps < minD {
			diameter = minD
		}
		if hasD && hasRing && diameter < minD { // cohérence croisée
			diameter = math.Max(minD, diameter)
		}
		if diameter <= drill { // garde-fou absolu
			diameter = drill * 1.6
		}
		if diameter != via.Diameter || drill != via.Drill {
			via.Diameter = roundMM(diameter)
			via.Drill = roundMM(drill)
			if err := via.Validate(); err != nil {
				// via invalide : on annule la modification de ce via
				via.Diameter, via.Drill = diameter, drill
				continue
			}
			changed++
		}
	}
	if changed == 0 {
		return nil, false
	}
	return &DRCFix{
		Code:       v.Code,
		Action:     FixEnlargeVia,
		Message:    fmt.Sprintf("%d via(s) du net %q agrandi(s) aux dimensions réglementaires.", changed, v.Net),
		Confidence: 0.9,
		Applied:    true,
		Target:     v.Net,
	}, true
}

// healEdgeViolation pulls the offending geometry inside the safe margin.
// NB : le point (X,Y) de la violation DRC est le point du BORD le plus
// proche, pas l'objet fautif — on re-scanne donc vias et pistes et on
// répare tous ceux qui violent réellement la marge (idempotent).
//   - via  : repositionné à la marge (confiance 0.85)
//   - piste: points translatés vers l'intérieur (confiance 0.65, la
//     connectivité des pads peut en pâtir → contre-vérification DRC).
func healEdgeViolation(board *domainlayout.Board, cs *domainconstraints.ConstraintSet,
	netClasses map[string]domainschematic.NetClass, v DRCViolation) (*DRCFix, bool) {
	if cs == nil {
		return nil, false
	}
	baseEdge, hasRule := ruleValue(cs, domainconstraints.RuleEdgeClearance, domainconstraints.AnyNetClass, domainconstraints.AnyLayer)
	if !hasRule {
		return nil, false
	}
	viaChanged, ptsChanged := 0, 0

	// Vias : centre à edge + diamètre/2 du bord minimum.
	for i := range board.Vias {
		via := &board.Vias[i]
		edge := baseEdge
		if val, ok := ruleValue(cs, domainconstraints.RuleEdgeClearance, classOf(netClasses, via.Net), via.FromLayer); ok {
			edge = val
		}
		required := edge + via.Diameter/2
		if pointBoundaryDistance(via.X, via.Y, board)+geometryEps >= required {
			continue
		}
		margin := required + 0.05
		if margin*2 >= board.WidthMM || margin*2 >= board.HeightMM {
			continue // carte trop petite : réparation impossible
		}
		nx, ny, moved := pullInside(via.X, via.Y, board.WidthMM, board.HeightMM, margin)
		if !moved {
			continue
		}
		via.X, via.Y = nx, ny
		viaChanged++
	}

	// Pistes : tout point trop proche du bord est ramené à l'intérieur.
	for i := range board.Tracks {
		t := &board.Tracks[i]
		edge := baseEdge
		if val, ok := ruleValue(cs, domainconstraints.RuleEdgeClearance, classOf(netClasses, t.Net), t.Layer); ok {
			edge = val
		}
		margin := edge + t.Width/2 + 0.05
		if margin*2 >= board.WidthMM || margin*2 >= board.HeightMM {
			continue
		}
		for j := range t.Points {
			if pointBoundaryDistance(t.Points[j].X, t.Points[j].Y, board)+geometryEps >= margin {
				continue
			}
			nx, ny, moved := pullInside(t.Points[j].X, t.Points[j].Y, board.WidthMM, board.HeightMM, margin)
			if moved {
				t.Points[j].X, t.Points[j].Y = nx, ny
				ptsChanged++
			}
		}
	}

	switch {
	case viaChanged > 0:
		return &DRCFix{
			Code:       v.Code,
			Action:     FixNudgeEdge,
			Message:    fmt.Sprintf("%d via(s) repositionné(s) à l'intérieur de la marge de bord (%.2f mm).", viaChanged, baseEdge),
			Confidence: 0.85,
			Applied:    true,
			Target:     v.Net,
		}, true
	case ptsChanged > 0:
		return &DRCFix{
			Code:       v.Code,
			Action:     FixNudgeEdge,
			Message:    fmt.Sprintf("%d point(s) de piste ramené(s) à l'intérieur de la marge de bord.", ptsChanged),
			Confidence: 0.65,
			Applied:    true,
			Target:     v.Net,
		}, true
	default:
		return nil, false
	}
}

// --------------------------------------------------------------
// Petits utilitaires géométriques / règles
// --------------------------------------------------------------

// cloneBoardForFix deep-copies a board (components, footprints, pads,
// tracks, vias) so repairs never touch the stored aggregate.
func cloneBoardForFix(b *domainlayout.Board) *domainlayout.Board {
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

func classOf(netClasses map[string]domainschematic.NetClass, net string) string {
	if c, ok := netClasses[net]; ok && c != "" {
		return string(c)
	}
	return string(domainschematic.ClassDefault)
}

func ruleValue(cs *domainconstraints.ConstraintSet, t domainconstraints.RuleType, cls string, layer int) (float64, bool) {
	if cs == nil {
		return 0, false
	}
	return cs.Value(t, cls, layer)
}

func roundMM(v float64) float64 { return math.Round(v*1000) / 1000 }

// pointBoundaryDistance returns the distance from a point to the nearest
// board edge (les 4 côtés du rectangle outline).
func pointBoundaryDistance(x, y float64, board *domainlayout.Board) float64 {
	d := x
	if y < d {
		d = y
	}
	if w := board.WidthMM - x; w < d {
		d = w
	}
	if h := board.HeightMM - y; h < d {
		d = h
	}
	return d
}

// pullInside moves a point just inside the given margin from every edge,
// along the shortest excursion. moved is false when already safe.
func pullInside(x, y, w, h, margin float64) (float64, float64, bool) {
	nx, ny := x, y
	if x < margin {
		nx = margin
	} else if x > w-margin {
		nx = w - margin
	}
	if y < margin {
		ny = margin
	} else if y > h-margin {
		ny = h - margin
	}
	moved := math.Abs(nx-x) > geometryEps || math.Abs(ny-y) > geometryEps
	return roundMM(nx), roundMM(ny), moved
}
