// Package verification now also hosts the Thermal Ghost: a steady-state
// thermal simulator for PCB boards. It solves the 2D heat equation
// (Laplace with heat sources) on a regular grid using an iterative
// Gauss-Seidel scheme, with ambient temperature fixed on the board edges
// (Dirichlet boundary). The result is a thermal map, ranked hotspots and
// the worst spatial gradient — real physics, deterministic, stdlib only,
// fully unit-testable.
package verificationapp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// Thermal tuning constants (documented physics, not magic numbers).
const (
	// thermalGridMax caps the solver resolution per axis (performance).
	thermalGridMax = 96
	// thermalIterations is the Gauss-Seidel iteration budget; the solver
	// also early-exits on convergence.
	thermalIterations = 3000
	// thermalConvergence stops the sweep when the max delta drops below
	// this many degrees.
	thermalConvergence = 0.05
	// copperSpreading models in-plane copper spreading (higher = flatter
	// gradients). 1.0 is bare FR4-ish, multilayer boards spread more.
	copperSpreading = 1.35
)

// ThermalSource is one heat source: a dissipating component.
type ThermalSource struct {
	Ref      string  `json:"ref"`
	PowerW   float64 `json:"power_w"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	WidthMM  float64 `json:"width_mm"`
	HeightMM float64 `json:"height_mm"`
}

// Hotspot is a ranked temperature peak.
type Hotspot struct {
	Rank          int     `json:"rank"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	TempC         float64 `json:"temp_c"`
	AboveAmbientC float64 `json:"above_ambient_c"`
	// LikelyRef is the nearest dissipating component, best explanation.
	LikelyRef string `json:"likely_ref"`
}

// ThermalResult is the response payload of POST .../thermal.
type ThermalResult struct {
	GridW       int       `json:"grid_w"`
	GridH       int       `json:"grid_h"`
	CellMM      float64   `json:"cell_mm"`
	AmbientC    float64   `json:"ambient_c"`
	MaxTempC    float64   `json:"max_temp_c"`
	MeanTempC   float64   `json:"mean_temp_c"`
	MinTempC    float64   `json:"min_temp_c"`
	MaxGradient float64   `json:"max_gradient_c_per_mm"` // pire gradient spatial
	Hotspots    []Hotspot `json:"hotspots"`
	// Grid is row-major, grid[row*W+col], values in °C; grid[0] is the
	// (0,0) corner of the board outline.
	Grid     []float64 `json:"grid"`
	Warnings []string  `json:"warnings"`
}

// ThermalChecker runs the steady-state simulation for a project.
type ThermalChecker struct {
	projects domainproject.Repository
	ambientC float64
}

// NewThermalChecker builds the simulator with the given ambient temperature
// (25 °C when zero).
func NewThermalChecker(projects domainproject.Repository, ambientC float64) *ThermalChecker {
	if ambientC <= 0 {
		ambientC = 25.0
	}
	return &ThermalChecker{projects: projects, ambientC: ambientC}
}

// defaultPower guesses a dissipation from the reference/value when the
// schematic carries no explicit power. Deliberately conservative.
func defaultPower(ref, value string) float64 {
	v := strings.ToLower(value)
	r := strings.ToUpper(ref)
	switch {
	case strings.Contains(v, "ldo"), strings.Contains(v, "7805"), strings.Contains(v, "317"), strings.Contains(v, "regulat"):
		return 0.8
	case strings.Contains(v, "mosfet"), strings.Contains(v, "buck"), strings.Contains(v, "driver"):
		return 0.6
	case strings.HasPrefix(r, "U"):
		return 0.25
	case strings.HasPrefix(r, "R"):
		return 0.05
	}
	return 0.02
}

// ThermalOverride carries the optional what-if payload of the REST call:
// a custom ambient and/or explicit heat sources replacing the component
// guesses. Zero values mean "use the project as-is".
type ThermalOverride struct {
	AmbientC float64
	Sources  []ThermalSource
}

// RunWithSources executes the simulation honouring the override.
func (t *ThermalChecker) RunWithSources(ctx context.Context, projectID string, ov ThermalOverride) (*ThermalResult, error) {
	ambient := t.ambientC
	if ov.AmbientC > 0 {
		ambient = ov.AmbientC
	}
	if len(ov.Sources) > 0 {
		var wMM, hMM float64
		layers := 2
		p, err := t.projects.FindByID(ctx, projectID)
		if err == nil && p.Board() != nil {
			wMM, hMM, layers = p.Board().WidthMM, p.Board().HeightMM, p.LayerCount()
		}
		if wMM <= 0 {
			wMM, hMM = 100, 80
		}
		return (&ThermalChecker{projects: t.projects, ambientC: ambient}).
			Simulate(wMM, hMM, layers, ov.Sources), nil
	}
	// Delegate to the plain Run by temporarily adopting the override
	// ambient (cleanest without duplicating the load logic).
	backup := t.ambientC
	t.ambientC = ambient
	defer func() { t.ambientC = backup }()
	return t.Run(ctx, projectID)
}

// Run executes the thermal simulation for the project.
func (t *ThermalChecker) Run(ctx context.Context, projectID string) (*ThermalResult, error) {
	p, err := t.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("thermique : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("thermique : chargement du projet : %w", err)
	}

	board := p.Board()
	if board == nil {
		return nil, fmt.Errorf("thermique : aucune carte physique sur le projet %q", projectID)
	}

	srcs := make([]ThermalSource, 0, len(board.Components))
	for i := range board.Components {
		c := &board.Components[i]
		srcs = append(srcs, ThermalSource{
			Ref: c.Ref, PowerW: defaultPower(c.Ref, c.Footprint.Value),
			X: c.X, Y: c.Y,
			WidthMM:  math.Max(2, c.Footprint.BodyWidthMM),
			HeightMM: math.Max(2, c.Footprint.BodyHeightMM),
		})
	}
	return t.Simulate(board.WidthMM, board.HeightMM, p.LayerCount(), srcs), nil
}

// Simulate is the pure physics core: sources in, thermal map out.
// Spreading grows with the layer count (internal planes spread heat).
func (t *ThermalChecker) Simulate(widthMM, heightMM float64, layers int, srcs []ThermalSource) *ThermalResult {
	w, h := widthMM, heightMM
	if w <= 0 || h <= 0 {
		w, h = 100, 80
	}
	if layers < 1 {
		layers = 2
	}
	spread := copperSpreading + 0.15*float64(layers-1)

	// Grid resolution: ~1.2 mm cells, capped for performance.
	gw := int(math.Min(thermalGridMax, math.Max(16, math.Ceil(w/1.2))))
	gh := int(math.Min(thermalGridMax, math.Max(16, math.Ceil(h/1.2))))
	cellW, cellH := w/float64(gw-1), h/float64(gh-1)

	// Deposit source power as local temperature injection. A rough
	// lumped model: ΔT_local ≈ P * R_θ where R_θ shrinks with copper
	// spreading; sources are rasterised over their footprint.
	rTheta := 55.0 / spread // °C/W for a small pad, reduced by spreading
	deposits := make([]float64, gw*gh)
	for _, s := range srcs {
		if s.PowerW <= 0 {
			continue
		}
		x0 := clampIdx(int(s.X/cellW), gw)
		x1 := clampIdx(int((s.X+s.WidthMM)/cellW)+1, gw)
		y0 := clampIdx(int(s.Y/cellH), gh)
		y1 := clampIdx(int((s.Y+s.HeightMM)/cellH)+1, gh)
		cells := math.Max(1, float64((x1-x0)*(y1-y0)))
		dt := s.PowerW * rTheta / math.Sqrt(cells)
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				deposits[yy*gw+xx] += dt
			}
		}
	}

	// grid stores ΔT above ambient; start from the injected deposits.
	grid := make([]float64, gw*gh)
	copy(grid, deposits)

	// Gauss-Seidel toward steady state; edges pinned to ambient (ΔT=0).
	// leak < 1 models vertical conduction into planes / heatsinks, so
	// heat diffuses laterally while slowly draining to ambient.
	leak := 0.94
	delta := 1.0
	for it := 0; it < thermalIterations && delta > thermalConvergence; it++ {
		delta = 0
		for y := 1; y < gh-1; y++ {
			for x := 1; x < gw-1; x++ {
				i := y*gw + x
				nd := (grid[i-1]+grid[i+1]+grid[i-gw]+grid[i+gw])/4*leak + deposits[i]
				if d := math.Abs(nd - grid[i]); d > delta {
					delta = d
				}
				grid[i] = nd
			}
		}
	}

	// Stats + hotspots: local maxima above 1 °C over ambient, ranked.
	maxT, minT, sum := -math.MaxFloat64, math.MaxFloat64, 0.0
	for _, v := range grid {
		maxT = math.Max(maxT, v)
		minT = math.Min(minT, v)
		sum += v
	}

	hot := []Hotspot{}
	for y := 2; y < gh-2; y++ {
		for x := 2; x < gw-2; x++ {
			i := y*gw + x
			v := grid[i]
			if v < grid[i-1] || v < grid[i+1] || v < grid[i-gw] || v < grid[i+gw] {
				continue // not a local maximum
			}
			if v < 1.0 { // below 1 °C over ambient: noise floor
				continue
			}
			px, py := float64(x)*cellW, float64(y)*cellH
			hot = append(hot, Hotspot{
				X: px, Y: py, TempC: t.ambientC + v, AboveAmbientC: v,
				LikelyRef: nearestSource(px, py, srcs),
			})
		}
	}
	sort.SliceStable(hot, func(i, j int) bool { return hot[i].AboveAmbientC > hot[j].AboveAmbientC })
	if len(hot) > 8 {
		hot = hot[:8]
	}
	for i := range hot {
		hot[i].Rank = i + 1
	}

	// Worst spatial gradient (°C per mm) over horizontal/vertical steps.
	maxGrad := 0.0
	for y := 0; y < gh; y++ {
		for x := 1; x < gw; x++ {
			g := math.Abs(grid[y*gw+x]-grid[y*gw+x-1]) / cellW
			maxGrad = math.Max(maxGrad, g)
		}
	}
	for y := 1; y < gh; y++ {
		for x := 0; x < gw; x++ {
			g := math.Abs(grid[y*gw+x]-grid[(y-1)*gw+x]) / cellH
			maxGrad = math.Max(maxGrad, g)
		}
	}

	warnings := []string{}
	if t.ambientC+maxT >= 60 {
		who := "n/a"
		if len(hot) > 0 {
			who = hot[0].LikelyRef
		}
		warnings = append(warnings, fmt.Sprintf(
			"Point chaud à %.0f °C (>60 °C) : ajouter un plan cuivre ou une viande thermique près de %s.",
			t.ambientC+maxT, who))
	}
	if maxGrad > 8 {
		warnings = append(warnings,
			"Gradient thermique local élevé : vérifier la dilatation différentielle (CTE) et les viandes thermiques.")
	}
	if len(srcs) > 0 && maxT < 5 {
		warnings = append(warnings,
			"Dissipation totale très faible : carte thermiquement relaxée, rien à signaler.")
	}

	return &ThermalResult{
		GridW: gw, GridH: gh, CellMM: cellW, AmbientC: t.ambientC,
		MaxTempC: t.ambientC + maxT, MeanTempC: t.ambientC + sum/float64(len(grid)),
		MinTempC: t.ambientC + minT, MaxGradient: maxGrad,
		Hotspots: hot, Grid: grid, Warnings: warnings,
	}
}

// nearestSource names the closest source to (px, py) — "unknown" when the
// board carries no dissipating component.
func nearestSource(px, py float64, srcs []ThermalSource) string {
	best, bestD := "", math.MaxFloat64
	for _, s := range srcs {
		dx, dy := s.X+s.WidthMM/2-px, s.Y+s.HeightMM/2-py
		if d := math.Sqrt(dx*dx + dy*dy); d < bestD {
			best, bestD = s.Ref, d
		}
	}
	if best == "" {
		return "n/a"
	}
	return best
}

func clampIdx(v, max int) int {
	if v < 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}
