// pour.go implements the copper-pour (ground plane) use case: generate,
// list and remove filled copper regions tied to a net. The generator is
// deterministic: a rectangular outline inset from the board edge, a
// sample-based fill computation that respects the clearance around every
// foreign track / pad / via of the layer, and optional via stitching
// between the plane layers. Polygon booleans are approximated by sampling
// (documented behaviour, <0.5% error at the default 0.25–0.5 mm step).
package layoutapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/geometry"
)

// Default pour parameters.
const (
	DefaultPourNet         = "GND"
	DefaultPourClearanceMM = 0.3
	DefaultStitchGridMM    = 2.5
	DefaultEdgeMarginMM    = 0.5

	maxFillSamples    = 120_000 // sampling budget per pour
	maxStitchingVias  = 400     // hard cap of stitching vias per generation
	stitchViaDiameter = 0.6
	stitchViaDrill    = 0.3
)

// PourGenerateRequest drives one plane-generation pass.
type PourGenerateRequest struct {
	// Net carrying the pour (empty = "GND").
	Net string `json:"net"`
	// Layers receiving a pour (indices); empty = every layer except F.Cu
	// (single-layer boards pour on F.Cu).
	Layers []int `json:"layers"`
	// Copper-to-foreign clearance; <=0 uses the default.
	ClearanceMM float64 `json:"clearance_mm"`
	// Hatch period; 0 = solid pour.
	HatchMM float64 `json:"hatch_mm"`
	// IsGround marks the pour as part of the return-current path; inferred
	// from the net name when omitted.
	IsGround bool `json:"is_ground"`
	// Edge margin between the outline and the board edge; <=0 uses the
	// project's edge-clearance rule or the default.
	EdgeMarginMM float64 `json:"edge_margin_mm"`
	// Stitch adds stitching vias between the poured layers.
	Stitch bool `json:"stitch"`
	// Stitch lattice period; <=0 uses the default.
	StitchGridMM float64 `json:"stitch_grid_mm"`
}

// PourLayerReport summarises one generated pour.
type PourLayerReport struct {
	PourID    string  `json:"pour_id"`
	Layer     int     `json:"layer"`
	LayerName string  `json:"layer_name"`
	AreaMM2   float64 `json:"area_mm2"`
	FillPct   float64 `json:"fill_pct"`
	Stitched  int     `json:"stitched"`
}

// PourReport is the response of the generation pass.
type PourReport struct {
	Net            string            `json:"net"`
	Replaced       int               `json:"replaced"`
	Pours          []PourLayerReport `json:"pours"`
	StitchingVias  int               `json:"stitching_vias"`
	TotalCopperMM2 float64           `json:"total_copper_mm2"`
	AvgFillPct     float64           `json:"avg_fill_pct"`
	DurationMS     int64             `json:"duration_ms"`
}

// PourService generates and maintains the copper pours of a board.
type PourService struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewPourService builds the copper-pour use case.
func NewPourService(projects domainproject.Repository, log *slog.Logger) *PourService {
	if log == nil {
		log = slog.Default()
	}
	return &PourService{projects: projects, log: log}
}

// Generate replaces every pour of the target net with freshly computed
// planes on the requested layers, persists the board and returns the
// per-layer statistics.
func (s *PourService) Generate(ctx context.Context, projectID string, req PourGenerateRequest) (*PourReport, error) {
	start := time.Now()

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("plans de masse : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("plans de masse : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return nil, fmt.Errorf("plans de masse : le projet %q n'a pas de carte", projectID)
	}

	net := req.Net
	if net == "" {
		net = DefaultPourNet
	}
	clearance := req.ClearanceMM
	if clearance <= 0 {
		clearance = DefaultPourClearanceMM
	}
	grid := req.StitchGridMM
	if grid <= 0 {
		grid = DefaultStitchGridMM
	}
	margin := req.EdgeMarginMM
	if margin <= 0 {
		margin = s.edgeMargin(p.Constraints())
	}
	if margin >= math.Min(board.WidthMM, board.HeightMM)/2 {
		return nil, fmt.Errorf("plans de masse : la marge de bord %.2f mm dévore la carte", margin)
	}

	layers, err := pourLayers(board, req.Layers)
	if err != nil {
		return nil, err
	}

	replaced := board.RemovePoursForNet(net)

	report := &PourReport{Net: net, Pours: []PourLayerReport{}}
	var pourIDs []int
	for _, layer := range layers {
		outline := domainlayout.RectOutline(margin, margin, board.WidthMM-margin, board.HeightMM-margin)
		fill := fillFraction(outline, board, layer, net, clearance)
		pour, err := board.AddPour(domainlayout.CopperPour{
			Net:         net,
			Name:        fmt.Sprintf("Plan %s — %s", net, layerNameOf(board, layer)),
			Layer:       layer,
			Outline:     outline,
			ClearanceMM: clearance,
			HatchMM:     req.HatchMM,
			IsGround:    req.IsGround || isGroundNet(net),
		})
		if err != nil {
			return nil, fmt.Errorf("plans de masse : couche %s : %w", layerNameOf(board, layer), err)
		}
		pour.FillPct = fill
		pour.AreaMM2 = pour.Area()
		pourIDs = append(pourIDs, layer)
		report.Pours = append(report.Pours, PourLayerReport{
			PourID: pour.ID, Layer: layer, LayerName: layerNameOf(board, layer),
			AreaMM2: pour.AreaMM2, FillPct: fill,
		})
	}

	if req.Stitch && len(pourIDs) >= 1 {
		report.StitchingVias = s.stitch(board, layers, net, clearance, grid)
		for i := range report.Pours {
			report.Pours[i].Stitched = boardStitchedCount(board, report.Pours[i].PourID)
		}
	}

	var totalArea, totalFill float64
	for _, pr := range report.Pours {
		totalArea += pr.AreaMM2 * pr.FillPct / 100
		totalFill += pr.FillPct
	}
	report.Replaced = replaced
	report.TotalCopperMM2 = round2(totalArea)
	if len(report.Pours) > 0 {
		report.AvgFillPct = round1(totalFill / float64(len(report.Pours)))
	}

	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("plans de masse : persistance : %w", err)
	}
	report.DurationMS = time.Since(start).Milliseconds()

	s.log.Info("plans de masse générés", "project_id", projectID, "net", net,
		"couches", len(report.Pours), "remplissage_moyen", report.AvgFillPct)
	return report, nil
}

// List returns every pour currently stored on the board.
func (s *PourService) List(ctx context.Context, projectID string) ([]domainlayout.CopperPour, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("plans de masse : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("plans de masse : chargement du projet : %w", err)
	}
	if p.Board() == nil {
		return []domainlayout.CopperPour{}, nil
	}
	out := make([]domainlayout.CopperPour, len(p.Board().Pours))
	copy(out, p.Board().Pours)
	return out, nil
}

// Delete removes one pour by identifier.
func (s *PourService) Delete(ctx context.Context, projectID, pourID string) error {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return fmt.Errorf("plans de masse : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return fmt.Errorf("plans de masse : chargement du projet : %w", err)
	}
	if p.Board() == nil || !p.Board().RemovePour(pourID) {
		return fmt.Errorf("plans de masse : plan de cuivre %q introuvable : %w", pourID, apperrors.ErrNotFound)
	}
	return s.projects.Update(ctx, p)
}

// DeleteForNet removes every pour of a net ("" = all pours).
func (s *PourService) DeleteForNet(ctx context.Context, projectID, net string) (int, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("plans de masse : chargement du projet : %w", err)
	}
	if p.Board() == nil {
		return 0, nil
	}
	removed := 0
	if net == "" {
		removed = len(p.Board().Pours)
		p.Board().Pours = p.Board().Pours[:0]
	} else {
		removed = p.Board().RemovePoursForNet(net)
	}
	if removed > 0 {
		if err := s.projects.Update(ctx, p); err != nil {
			return 0, fmt.Errorf("plans de masse : persistance : %w", err)
		}
	}
	return removed, nil
}

// --------------------------------------------------------------
// Génération : couches, remplissage, couture
// --------------------------------------------------------------

// pourLayers validates/derives the target layers: explicit list, or every
// layer except F.Cu (single-layer boards pour on F.Cu).
func pourLayers(board *domainlayout.Board, requested []int) ([]int, error) {
	if len(requested) == 0 {
		if board.LayerCount == 1 {
			return []int{0}, nil
		}
		out := make([]int, 0, board.LayerCount-1)
		for l := 1; l < board.LayerCount; l++ {
			out = append(out, l)
		}
		return out, nil
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(requested))
	for _, l := range requested {
		if l < 0 || l >= board.LayerCount {
			return nil, fmt.Errorf("plans de masse : couche %d hors stack (%d couches)", l, board.LayerCount)
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	sort.Ints(out)
	return out, nil
}

// edgeMargin reads the project's edge-clearance rule, falling back to the
// default margin.
func (s *PourService) edgeMargin(cs *domainconstraints.ConstraintSet) float64 {
	if cs == nil {
		return DefaultEdgeMarginMM
	}
	if v, ok := cs.Value(domainconstraints.RuleEdgeClearance, domainconstraints.AnyNetClass, domainconstraints.AnyLayer); ok && v > 0 {
		return v
	}
	return DefaultEdgeMarginMM
}

// layerObstacle is one foreign copper item of a layer, pre-boxed for the
// sample loop.
type layerObstacle struct {
	kind   string // "segment" | "via" | "pad"
	box    geometry.AABB
	ax, ay float64 // segment endpoints
	bx, by float64
	radius float64 // via/pad effective radius (clearance excluded)
	net    string
}

// collectObstacles gathers the foreign copper of a layer (everything whose
// net differs from the pour net).
func collectObstacles(board *domainlayout.Board, layer int, net string, clearance float64) []layerObstacle {
	out := []layerObstacle{}
	push := func(o layerObstacle) {
		o.box = o.box.Expand(clearance)
		out = append(out, o)
	}
	for i := range board.Tracks {
		t := &board.Tracks[i]
		if t.Net == net {
			continue
		}
		for j := 1; j < len(t.Points); j++ {
			a, b := t.Points[j-1], t.Points[j]
			if t.Layer != layer {
				continue
			}
			push(layerObstacle{
				kind: "segment", ax: a.X, ay: a.Y, bx: b.X, by: b.Y,
				radius: t.Width / 2, net: t.Net,
				box: geometry.AABBFromPoints([]geometry.Point{{X: a.X, Y: a.Y}, {X: b.X, Y: b.Y}}),
			})
		}
	}
	for i := range board.Vias {
		v := &board.Vias[i]
		if v.Net == net || layer < v.FromLayer || layer > v.ToLayer {
			continue
		}
		push(layerObstacle{
			kind: "via", ax: v.X, ay: v.Y,
			radius: v.Diameter / 2, net: v.Net,
			box: geometry.AABB{MinX: v.X, MinY: v.Y, MaxX: v.X, MaxY: v.Y},
		})
	}
	for i := range board.Components {
		c := &board.Components[i]
		for _, pad := range c.Footprint.Pads {
			if pad.Net == "" || pad.Net == net {
				continue
			}
			if pad.Layer != layer && pad.Layer >= 0 {
				continue
			}
			pos := c.PadAbsolutePosition(pad)
			r := math.Hypot(pad.Width, pad.Height) / 2 // cercle englobant (conservateur)
			push(layerObstacle{
				kind: "pad", ax: pos.X, ay: pos.Y,
				radius: r, net: pad.Net,
				box: geometry.AABB{MinX: pos.X, MinY: pos.Y, MaxX: pos.X, MaxY: pos.Y},
			})
		}
	}
	return out
}

// occupied reports whether the sample point violates the clearance of any
// obstacle (the box pre-filter bounds the exact-distance checks).
func occupied(obs []layerObstacle, x, y, clearance float64) bool {
	for i := range obs {
		o := &obs[i]
		if x < o.box.MinX || x > o.box.MaxX || y < o.box.MinY || y > o.box.MaxY {
			continue
		}
		switch o.kind {
		case "segment":
			if geometry.PointSegmentDistance(geometry.Point{X: x, Y: y},
				geometry.Point{X: o.ax, Y: o.ay}, geometry.Point{X: o.bx, Y: o.by}) < o.radius+clearance {
				return true
			}
		default: // via / pad
			if math.Hypot(x-o.ax, y-o.ay) < o.radius+clearance {
				return true
			}
		}
	}
	return false
}

// fillFraction samples the outline and returns the free-copper percentage.
func fillFraction(outline []geometry.Point, board *domainlayout.Board, layer int, net string, clearance float64) float64 {
	poly := geometry.Polygon(outline)
	box := geometry.AABBFromPoints(outline)
	step := math.Max(0.25, math.Min(box.Width(), box.Height())/200)
	for box.Width()/step*box.Height()/step > maxFillSamples {
		step *= 1.5
	}
	obs := collectObstacles(board, layer, net, clearance)

	samples, free := 0, 0
	for y := box.MinY + step/2; y <= box.MaxY; y += step {
		for x := box.MinX + step/2; x <= box.MaxX; x += step {
			if !poly.ContainsPoint(geometry.Point{X: x, Y: y}) {
				continue
			}
			samples++
			if !occupied(obs, x, y, clearance) {
				free++
			}
		}
	}
	if samples == 0 {
		return 0
	}
	return round1(float64(free) / float64(samples) * 100)
}

// stitch drops a deterministic lattice of stitching vias between the poured
// layers, keeping the clearance of foreign copper. Returns the count added.
func (s *PourService) stitch(board *domainlayout.Board, layers []int, net string, clearance, grid float64) int {
	if len(layers) == 0 {
		return 0
	}
	added := 0
	for pair := 0; pair+1 < len(layers) && added < maxStitchingVias; pair++ {
		from, to := layers[pair], layers[pair+1]
		// Obstacles des deux couches (les pistes du même net bloquent aussi :
		// une couture collée à une piste du même net reste légale, mais on
		// privilégie le cuivre libre pour ne pas créer de coude DRC).
		obsA := collectObstacles(board, from, net, clearance)
		obsB := collectObstacles(board, to, net, clearance)
		obs := append(obsA, obsB...)

		margin := DefaultEdgeMarginMM
		viaR := stitchViaDiameter / 2
		keepout := clearance + viaR
		row := 0
		for y := margin + grid/2; y <= board.HeightMM-margin && added < maxStitchingVias; y += grid {
			col := 0
			for x := margin + grid/2; x <= board.WidthMM-margin && added < maxStitchingVias; x += grid {
				// Quinconce entre paires de couches pour maximiser la couverture.
				px := x + float64(row%2)*grid/2 + float64(pair%2)*grid/4
				py := y + float64(col%2)*grid/4
				col++
				if px >= board.WidthMM-margin || py >= board.HeightMM-margin {
					continue
				}
				if occupied(obs, px, py, keepout) {
					continue
				}
				// Pas de doublon avec un via existant du même net.
				if viaTooClose(board, px, py, viaR*2+2*clearance) {
					continue
				}
				if err := board.AddVia(domainlayout.Via{
					Net: net, X: round2(px), Y: round2(py),
					FromLayer: from, ToLayer: to,
					Diameter: stitchViaDiameter, Drill: stitchViaDrill,
				}); err != nil {
					continue
				}
				added++
			}
			row++
		}
	}
	return added
}

// viaTooClose reports whether an existing via sits within minMM of (x, y).
func viaTooClose(board *domainlayout.Board, x, y, minMM float64) bool {
	for i := range board.Vias {
		if math.Hypot(board.Vias[i].X-x, board.Vias[i].Y-y) < minMM {
			return true
		}
	}
	return false
}

// boardStitchedCount counts the pour-net vias (stitching report per pour).
func boardStitchedCount(board *domainlayout.Board, pourID string) int {
	pour, ok := board.PourByID(pourID)
	if !ok {
		return 0
	}
	count := 0
	for i := range board.Vias {
		v := &board.Vias[i]
		if v.Net == pour.Net && pour.Contains(v.X, v.Y) {
			count++
		}
	}
	return count
}

func isGroundNet(net string) bool {
	switch net {
	case "GND", "gnd", "AGND", "DGND", "PGND", "VSS", "vss":
		return true
	}
	return false
}

func layerNameOf(board *domainlayout.Board, layer int) string {
	if layer >= 0 && layer < len(board.LayerNames) {
		return board.LayerNames[layer]
	}
	return fmt.Sprintf("L%d", layer)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
