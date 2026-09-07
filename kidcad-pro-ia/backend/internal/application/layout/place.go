// Package layoutapp contains the placement, routing and optimisation use
// cases. They orchestrate the domain aggregate through the project
// repository and delegate the heavy computation to the AIService port
// (implemented by infrastructure/ai/grpc_client.go). The package name
// differs from the folder name to avoid collisions with the domain package.
package layoutapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
)

// ErrAIUnreachable is returned (wrapped) by the use cases when the AI engine
// cannot be reached at all (connection refused, DNS failure, gRPC
// Unavailable). The infrastructure gRPC client raises it and the REST layer
// maps it onto 503 {"error":{"code":"ai_unreachable"}}.
var ErrAIUnreachable = errors.New("moteur IA injoignable")

// AIService is the port toward the AI engine ( KidCAD ai-engine, gRPC).
type AIService interface {
	Health(ctx context.Context) error
	PlanPlacement(ctx context.Context, b *domainlayout.Board, comps []domainschematic.Component,
		strategy string) ([]domainlayout.PlacedComponent, error)
	RouteBoard(ctx context.Context, b *domainlayout.Board, nets []domainschematic.Net,
		cs *domainconstraints.ConstraintSet, strategy string,
		onProgress func(RouteProgress)) (*RouteOutcome, error)
	OptimizeRoutes(ctx context.Context, b *domainlayout.Board, outcome *RouteOutcome,
		cs *domainconstraints.ConstraintSet,
		onProgress func(RouteProgress)) (*RouteOutcome, error)
}

// RouteProgress is one progress event emitted while the AI engine routes or
// optimises a board.
type RouteProgress struct {
	JobID      string
	Stage      string // "route" | "optimize"
	CurrentNet string
	Message    string
	Percent    float64 // 0..100
	Done       bool
	Err        string
}

// NetRoute is the routing result of a single net.
type NetRoute struct {
	Net       string
	Tracks    []domainlayout.Track
	Vias      []domainlayout.Via
	LengthMM  float64
	Completed bool
}

// RouteOutcome aggregates the per-net results of a routing / optimisation
// run.
type RouteOutcome struct {
	Strategy   string
	Nets       []NetRoute
	DurationMS int64
}

// PlaceService runs the automatic component placement use case.
type PlaceService struct {
	projects domainproject.Repository
	ai       AIService
	log      *slog.Logger
}

// NewPlaceService builds the placement use case.
func NewPlaceService(projects domainproject.Repository, ai AIService, log *slog.Logger) *PlaceService {
	return &PlaceService{projects: projects, ai: ai, log: log}
}

// Place loads the project, asks the AI engine for a placement and persists
// the resulting board. Components already placed keep their footprints
// (matched by reference); the AI only moves them.
func (s *PlaceService) Place(ctx context.Context, projectID, strategy string) (*domainlayout.Board, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("placement : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("placement : chargement du projet : %w", err)
	}

	board := p.Board()
	if board == nil {
		// No board imported yet: use a sensible default outline so the AI
		// engine still receives a usable working area.
		var err error
		board, err = domainlayout.NewBoard(100, 80, p.LayerCount())
		if err != nil {
			return nil, fmt.Errorf("placement : carte par défaut : %w", err)
		}
	}

	components := componentsForPlacement(p, board)

	placed, err := s.ai.PlanPlacement(ctx, board, components, strategy)
	if err != nil {
		return nil, fmt.Errorf("placement : moteur IA : %w", err)
	}

	preserveFootprints(board, placed)
	board.Components = placed

	p.SetBoard(board)
	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("placement : persistance du projet : %w", err)
	}

	s.log.Info("placement terminé",
		"project_id", projectID,
		"strategy", strategy,
		"components", len(placed))
	return board, nil
}

// componentsForPlacement builds the schematic component list handed to the
// AI engine: from the schematic when available, otherwise from the already
// placed board components.
func componentsForPlacement(p *domainproject.Project, board *domainlayout.Board) []domainschematic.Component {
	if sch := p.Schematic(); sch != nil && len(sch.Components) > 0 {
		return append([]domainschematic.Component(nil), sch.Components...)
	}
	out := make([]domainschematic.Component, 0, len(board.Components))
	for i := range board.Components {
		bc := &board.Components[i]
		out = append(out, domainschematic.Component{
			Ref:       bc.Ref,
			Value:     bc.Footprint.Value,
			Footprint: bc.Footprint.Name,
			X:         bc.X,
			Y:         bc.Y,
			Rotation:  bc.Rotation,
			Fixed:     bc.Fixed,
		})
	}
	return out
}

// preserveFootprints restores the physical footprint of every placed
// component from the previous board state (matched by reference). The AI
// engine only returns positions; the copper pads must stay identical.
func preserveFootprints(board *domainlayout.Board, placed []domainlayout.PlacedComponent) {
	for i := range placed {
		pc := &placed[i]
		if pc.Footprint.Name != "" {
			continue
		}
		if previous, ok := board.ComponentByRef(pc.Ref); ok {
			pc.Footprint = previous.Footprint
			continue
		}
		pc.Footprint = domainlayout.Footprint{
			Name:         pc.Footprint.Name,
			BodyWidthMM:  2,
			BodyHeightMM: 2,
			HeightMM:     domainlayout.DefaultBoardHeightMM,
		}
	}
}
