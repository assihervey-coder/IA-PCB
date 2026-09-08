// Package exportapp — see doc.go for the package rationale.
package exportapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// KicadWriter is the port to the .kicad_pcb generator (implemented by
// infrastructure/fileio/writer.KicadWriter). Write must create outDir when
// missing and return the produced file path.
type KicadWriter interface {
	Write(b *domainlayout.Board, projectName, outDir string) (string, error)
}

// KicadService exports the board of a project as a KiCad board file
// (.kicad_pcb), closing the KiCad round-trip loop: a board imported from
// KiCad, routed by YahriaCad and re-exported opens again in pcbnew.
type KicadService struct {
	projects domainproject.Repository
	writer   KicadWriter
	dataDir  string
	log      *slog.Logger
}

// NewKicadService builds the KiCad export use case.
func NewKicadService(projects domainproject.Repository, w KicadWriter, dataDir string, log *slog.Logger) *KicadService {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &KicadService{projects: projects, writer: w, dataDir: dataDir, log: log}
}

// ExportToFile writes the KiCad board of the project under
// <dataDir>/exports/<projectID>/<projectName>.kicad_pcb and returns the
// produced path.
func (s *KicadService) ExportToFile(ctx context.Context, projectID string) (string, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", fmt.Errorf("export kicad : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", fmt.Errorf("export kicad : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return "", fmt.Errorf("export kicad : le projet %q n'a pas de carte", projectID)
	}
	outDir := filepath.Join(s.dataDir, "exports", projectID)
	path, err := s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return "", fmt.Errorf("export kicad : %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("export kicad : fichier manquant après écriture : %w", err)
	}
	s.log.Info("export kicad pcb", "project_id", projectID, "file", path)
	return path, nil
}
