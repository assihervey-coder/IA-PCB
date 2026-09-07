// Package schematicapp contains the schematic use cases: design file import
// and schematic validation. The package name differs from the folder name to
// avoid collisions with the domain package (see contracts.md §1).
package schematicapp

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

// ImportResult is the outcome of a successful file import: the parsed design
// plus the default constraints applied when the source format carries none.
type ImportResult struct {
	Format      string // "kicad", "eagle", "interchange", "kicad-netlist", "protel-netlist"
	Schematic   *domainschematic.Schematic
	Board       *domainlayout.Board
	Constraints *domainconstraints.ConstraintSet
	Warnings    []string
}

// ReaderRegistry is the port to the format readers (implemented by
// infrastructure/fileio/reader.Registry). It dispatches on file extension
// and content sniffing.
type ReaderRegistry interface {
	Read(path string) (ImportResult, error)
}

// ImportService loads a design file and attaches the parsed schematic /
// board / constraints to an existing project.
type ImportService struct {
	projects domainproject.Repository
	registry ReaderRegistry
	log      *slog.Logger
}

// NewImportService builds the import use case.
func NewImportService(projects domainproject.Repository, registry ReaderRegistry, log *slog.Logger) *ImportService {
	return &ImportService{projects: projects, registry: registry, log: log}
}

// ImportFromFile reads the design at path and persists it on the project.
// The project must already exist; project.ErrNotFound is wrapped so callers
// can map it onto a 404.
func (s *ImportService) ImportFromFile(ctx context.Context, projectID, path string) (*ImportResult, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("import : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("import : chargement du projet : %w", err)
	}

	res, err := s.registry.Read(path)
	if err != nil {
		return nil, fmt.Errorf("import : %w", err)
	}

	if res.Schematic != nil {
		p.SetSchematic(res.Schematic)
	}
	if res.Board != nil {
		p.SetBoard(res.Board)
	}
	// Le contrat (§4) impose un jeu de contraintes non nil après import :
	// quand le format source n'en transporte pas, les valeurs par défaut
	// sont appliquées.
	if res.Constraints == nil {
		res.Constraints = domainconstraints.NewDefault()
	}
	p.SetConstraints(res.Constraints)

	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("import : persistance du projet : %w", err)
	}

	var components, nets int
	if res.Schematic != nil {
		components, nets = len(res.Schematic.Components), len(res.Schematic.Nets)
	}
	s.log.Info("import terminé",
		"project_id", projectID,
		"format", res.Format,
		"components", components,
		"nets", nets,
		"warnings", len(res.Warnings))
	return &res, nil
}
