package exportapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// STEPWriter is the port to the STEP AP214 generator (implemented by
// infrastructure/fileio/writer.StepWriter). Write must create outDir when
// missing and return the produced file path.
type STEPWriter interface {
	Write(b *domainlayout.Board, projectName, outDir string) (string, error)
}

// STEPService exports the 3D model of a project (board slab + component
// cuboids) as a STEP file.
type STEPService struct {
	projects domainproject.Repository
	writer   STEPWriter
	dataDir  string
	log      *slog.Logger
}

// NewSTEPService builds the STEP export use case.
func NewSTEPService(projects domainproject.Repository, w STEPWriter, dataDir string, log *slog.Logger) *STEPService {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &STEPService{projects: projects, writer: w, dataDir: dataDir, log: log}
}

// Export writes the STEP model of the project under
// <dataDir>/step/<projectID>/ and returns the produced file path.
func (s *STEPService) Export(ctx context.Context, projectID string) (string, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", fmt.Errorf("export step : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", fmt.Errorf("export step : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return "", fmt.Errorf("export step : le projet %q n'a pas de carte", projectID)
	}

	outDir := filepath.Join(s.dataDir, "step", projectID)
	path, err := s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return "", fmt.Errorf("export step : %w", err)
	}

	s.log.Info("export step", "project_id", projectID, "file", path)
	return path, nil
}
