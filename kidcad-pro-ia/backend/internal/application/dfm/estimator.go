// Package dfm implements the "Cost & Yield Oracle": a deterministic
// manufacturability estimator. From the physical board (area, layers, via
// density, minimum geometry) it derives a unit price curve at three
// production volumes, a first-pass-yield prediction and risk flags. The
// cost model is intentionally transparent — every surcharge is listed so
// the engineer sees WHY the price moves, something quote portals keep
// hidden.
package dfm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// Cost model reference points (EUR, documented in docs/guides).
const (
	setupFeeEUR        = 90.0 // outillage / phototraces / programme de test
	basePricePerDM2    = 16.0 // 2 couches, process standard, FR-4 1.6 mm
	pricePerExtraLayer = 6.5  // €/dm² par couche cuivre supplémentaire
	protoQty           = 10
	pilotQty           = 100
	seriesQty          = 1000
	protoDiscount      = 1.0 // pas de remise en proto
	pilotDiscount      = 0.82
	seriesDiscount     = 0.66
)

// Risk thresholds (process "standard" des fondeurs européens).
const (
	minTrackStandard = 0.15 // mm
	minDrillStandard = 0.25 // mm
	viaDensityRisky  = 50.0 // vias / cm²
	utilizationRisky = 85.0 // % d'occupation
)

// Estimate is the full response of POST .../dfm.
type Estimate struct {
	Currency   string      `json:"currency"` // "EUR"
	AreaDM2    float64     `json:"area_dm2"`
	Layers     int         `json:"layers"`
	ViaCount   int         `json:"via_count"`
	ViaDensity float64     `json:"via_density_per_cm2"` // vias/cm²
	MinTrackMM float64     `json:"min_track_mm"`
	MinDrillMM float64     `json:"min_drill_mm"`
	BOMLines   int         `json:"bom_lines"`
	Components int         `json:"components"`
	UnitPrices []UnitPrice `json:"unit_prices"`
	YieldPct   float64     `json:"first_pass_yield_pct"`
	DefectRisk string      `json:"defect_risk"` // "faible" | "moyen" | "élevé"
	RiskFlags  []RiskFlag  `json:"risk_flags"`
	Surcharges []Surcharge `json:"surcharges"`
	Advice     []string    `json:"advice"`
	DurationMS int64       `json:"duration_ms"`
}

// UnitPrice is the quote for one volume.
type UnitPrice struct {
	Qty      int     `json:"qty"`
	Label    string  `json:"label"` // "Prototype", "Pilote", "Série"
	UnitEUR  float64 `json:"unit_eur"`
	TotalEUR float64 `json:"total_eur"`
}

// RiskFlag is one manufacturability warning.
type RiskFlag struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Impact  float64 `json:"impact_pct"` // impact relatif sur le prix unitaire
}

// Surcharge explains one line of the price build-up.
type Surcharge struct {
	Label string  `json:"label"`
	Pct   float64 `json:"pct"` // multiplication du prix de base (1.0 = neutre)
}

// Service is the DFM use case.
type Service struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewService builds the estimator.
func NewService(projects domainproject.Repository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{projects: projects, log: log}
}

// Estimate runs the model on the current board.
func (s *Service) Estimate(ctx context.Context, projectID string) (*Estimate, error) {
	start := time.Now()

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("dfm : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("dfm : chargement du projet : %w", err)
	}
	b := p.Board()
	if b == nil {
		return nil, fmt.Errorf("dfm : le projet %q n'a pas de carte (importez ou placez d'abord)", projectID)
	}

	est := &Estimate{
		Currency:   "EUR",
		Layers:     b.LayerCount,
		ViaCount:   len(b.Vias),
		RiskFlags:  []RiskFlag{},
		Surcharges: []Surcharge{},
		Advice:     []string{},
	}

	areaDM2 := b.WidthMM * b.HeightMM / 100.0 // mm² -> dm²
	est.AreaDM2 = math.Round(areaDM2*1000) / 1000
	areaCM2 := b.WidthMM * b.HeightMM / 100.0
	if areaCM2 > 0 {
		est.ViaDensity = math.Round(float64(len(b.Vias))/areaCM2*10) / 10
	}
	est.Components = len(b.Components)
	est.BOMLines = bomLineCount(p)

	minTrack, minDrill := minGeometry(b)
	est.MinTrackMM = minTrack
	est.MinDrillMM = minDrill

	// --- Construction du prix --------------------------------------------
	base := basePricePerDM2
	layerSurcharge := pricePerExtraLayer * float64(maxInt(0, b.LayerCount-2))
	techFactor := 1.0

	if minTrack > 0 && minTrack < minTrackStandard {
		f := 1.25
		if minTrack < 0.12 {
			f = 1.5
		}
		techFactor *= f
		est.Surcharges = append(est.Surcharges, Surcharge{
			Label: fmt.Sprintf("Piste fine (%.2f mm < %.2f mm)", minTrack, minTrackStandard), Pct: f})
		est.RiskFlags = append(est.RiskFlags, RiskFlag{
			Code:    "fine_track",
			Message: fmt.Sprintf("Piste de %.2f mm : gravure fine (±20 µm) requise.", minTrack),
			Impact:  (f - 1) * 100})
	}
	if minDrill > 0 && minDrill < minDrillStandard {
		f := 1.4
		techFactor *= f
		est.Surcharges = append(est.Surcharges, Surcharge{
			Label: fmt.Sprintf("Perçage laser (%.2f mm)", minDrill), Pct: f})
		est.RiskFlags = append(est.RiskFlags, RiskFlag{
			Code:    "laser_drill",
			Message: fmt.Sprintf("Perçage de %.2f mm : forage laser micro-via obligatoire.", minDrill),
			Impact:  (f - 1) * 100})
	}
	if est.ViaDensity > viaDensityRisky {
		f := 1.15
		techFactor *= f
		est.Surcharges = append(est.Surcharges, Surcharge{
			Label: fmt.Sprintf("Densité de vias (%.0f/cm²)", est.ViaDensity), Pct: f})
		est.RiskFlags = append(est.RiskFlags, RiskFlag{
			Code:    "via_density",
			Message: fmt.Sprintf("Densité de %.0f vias/cm² : fiable mais hors process standard (>%.0f).", est.ViaDensity, viaDensityRisky),
			Impact:  (f - 1) * 100})
	}

	sqm := areaDM2
	if sqm <= 0 {
		sqm = 0.25 // plancher de facturation ~ 5x5 cm
	}
	corePerUnit := sqm * (base + layerSurcharge) * techFactor

	est.UnitPrices = []UnitPrice{
		volumePrice(protoQty, "Prototype", corePerUnit, protoDiscount, setupFeeEUR),
		volumePrice(pilotQty, "Pilote", corePerUnit, pilotDiscount, setupFeeEUR),
		volumePrice(seriesQty, "Série", corePerUnit, seriesDiscount, setupFeeEUR),
	}

	// --- Rendement premier passage ---------------------------------------
	// Modèle d'Erlang simplifié avec taux de défauts par cause : le forage
	// laser et la gravure fine ont chacun leur probabilité propre (plus
	// élevée que le process mécanique standard).
	laser := minDrill > 0 && minDrill < minDrillStandard
	fine := minTrack > 0 && minTrack < minTrackStandard
	viaRate := 0.00045
	if laser {
		viaRate = 0.0025 // micro-vias laser ≈ 5× plus sensibles
	}
	trackRate := 0.000018
	if fine {
		trackRate = 0.00004 // gravure fine ≈ 2× plus sensible
	}
	defects := float64(len(b.Vias))*viaRate + b.TotalTrackLength()*trackRate
	if b.LayerCount > 4 {
		defects *= 1.0 + 0.08*float64(b.LayerCount-4) // empilements complexes
	}
	yield := math.Exp(-defects) * 100
	est.YieldPct = math.Round(math.Min(99.9, math.Max(35, yield))*10) / 10
	switch {
	case est.YieldPct >= 95:
		est.DefectRisk = "faible"
	case est.YieldPct >= 85:
		est.DefectRisk = "moyen"
	default:
		est.DefectRisk = "élevé"
	}

	// --- Conseils ---------------------------------------------------------
	est.Advice = append(est.Advice, costAdvice(est, b)...)

	est.DurationMS = time.Since(start).Milliseconds()
	s.log.Info("dfm : estimation produite", "project_id", projectID,
		"proto_eur", est.UnitPrices[0].UnitEUR, "yield", est.YieldPct)
	return est, nil
}

// volumePrice builds one quote line: amortized setup + volume-discounted
// core price.
func volumePrice(qty int, label string, corePerUnit, discount, setup float64) UnitPrice {
	amortized := setup / float64(qty)
	unit := (corePerUnit*discount + amortized) * 1.08 // marge transport/manutention
	return UnitPrice{
		Qty:      qty,
		Label:    label,
		UnitEUR:  math.Round(unit*100) / 100,
		TotalEUR: math.Round(unit*float64(qty)*100) / 100,
	}
}

// bomLineCount counts the distinct reference prefixes (BOM lines) of the
// schematic, falling back to the placed components.
func bomLineCount(p *domainproject.Project) int {
	if sch := p.Schematic(); sch != nil && len(sch.Components) > 0 {
		seen := map[string]struct{}{}
		for _, c := range sch.Components {
			seen[c.Value+"|"+c.Footprint] = struct{}{}
		}
		return len(seen)
	}
	seen := map[string]struct{}{}
	if b := p.Board(); b != nil {
		for i := range b.Components {
			seen[b.Components[i].Footprint.Value+"|"+b.Components[i].Footprint.Name] = struct{}{}
		}
	}
	return len(seen)
}

// minGeometry returns the smallest track width and drill of the board.
func minGeometry(b *domainlayout.Board) (float64, float64) {
	minTrack, minDrill := 0.0, 0.0
	for i := range b.Tracks {
		w := b.Tracks[i].Width
		if w > 0 && (minTrack == 0 || w < minTrack) {
			minTrack = w
		}
	}
	for i := range b.Vias {
		d := b.Vias[i].Drill
		if d > 0 && (minDrill == 0 || d < minDrill) {
			minDrill = d
		}
	}
	return minTrack, minDrill
}

// costAdvice turns the estimate into concrete cost-saving actions.
func costAdvice(est *Estimate, b *domainlayout.Board) []string {
	var out []string
	if est.MinDrillMM > 0 && est.MinDrillMM < minDrillStandard {
		out = append(out, "Relargez les micro-vias à 0.25 mm pour rester en forage mécanique (≈ -28 % sur le prix unitaire).")
	}
	if est.MinTrackMM > 0 && est.MinTrackMM < minTrackStandard {
		out = append(out, "Élargissez les pistes à 0.15 mm minimum pour éviter la gravure fine (≈ -20 %).")
	}
	if est.ViaDensity > viaDensityRisky {
		out = append(out, "Réduisez la densité de vias (planes de masse, deux couches supplémentaires) pour revenir au process standard.")
	}
	if b.LayerCount > 4 && est.ViaDensity < 20 {
		out = append(out, "Avec cette faible densité, un stack-up 4 couches au lieu de "+
			fmt.Sprintf("%d", b.LayerCount)+" diviserait le coût cuivre par ~2.")
	}
	if len(out) == 0 {
		out = append(out, "Géométrie conforme au process standard : le prix est déjà optimisé, jouez sur les volumes pour baisser le coût unitaire.")
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
