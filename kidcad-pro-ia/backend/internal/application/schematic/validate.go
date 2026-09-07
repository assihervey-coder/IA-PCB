package schematicapp

import (
	"context"
	"fmt"
	"log/slog"

	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
)

// ValidationService exposes the schematic-level checks (ERC primitives) of
// the domain aggregate. The verification use case (application/verification)
// builds on top of it for the HTTP-facing ERC report.
type ValidationService struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewValidationService builds the validation use case.
func NewValidationService(projects domainproject.Repository, log *slog.Logger) *ValidationService {
	return &ValidationService{projects: projects, log: log}
}

// ValidateSchematic loads the project and runs the intrinsic domain checks.
// A project without a schematic yields an empty issue list, not an error.
func (s *ValidationService) ValidateSchematic(ctx context.Context, projectID string) ([]domainschematic.Issue, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("validation : %w", err)
	}
	sch := p.Schematic()
	if sch == nil {
		s.log.Debug("validation : projet sans schéma", "project_id", projectID)
		return []domainschematic.Issue{}, nil
	}
	issues := sch.Validate()
	s.log.Debug("validation schéma terminée",
		"project_id", projectID,
		"issues", len(issues))
	return issues, nil
}
