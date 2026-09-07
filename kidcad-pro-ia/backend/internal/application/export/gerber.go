// Package exportapp contains the export use cases: Gerber archive, BOM CSV
// and STEP 3D model. The heavy lifting is delegated to writer ports
// implemented by infrastructure/fileio/writer. The package name differs from
// the folder name to avoid collisions with encoding-specific packages.
package exportapp

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// GerberWriter is the port to the RS-274X generator (implemented by
// infrastructure/fileio/writer.GerberWriter). Write must create outDir when
// missing and return the list of produced file paths.
type GerberWriter interface {
	Write(b *domainlayout.Board, projectName, outDir string) ([]string, error)
}

// GerberService exports the board of a project as Gerber files, either to a
// directory or packaged into a zip archive.
type GerberService struct {
	projects domainproject.Repository
	writer   GerberWriter
	dataDir  string
	log      *slog.Logger
}

// NewGerberService builds the Gerber export use case.
func NewGerberService(projects domainproject.Repository, w GerberWriter, dataDir string, log *slog.Logger) *GerberService {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &GerberService{projects: projects, writer: w, dataDir: dataDir, log: log}
}

// ExportToDir writes the Gerber files of the project into outDir and returns
// their absolute-ish paths.
func (s *GerberService) ExportToDir(ctx context.Context, projectID, outDir string) ([]string, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("export gerber : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("export gerber : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return nil, fmt.Errorf("export gerber : le projet %q n'a pas de carte", projectID)
	}
	files, err := s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return nil, fmt.Errorf("export gerber : %w", err)
	}
	s.log.Info("export gerber", "project_id", projectID, "dir", outDir, "files", len(files))
	return files, nil
}

// ExportToZip writes the Gerber files under
// <dataDir>/exports/<projectID>/gerber, then packages them into
// <dataDir>/exports/<projectID>/<projectName>-gerber.zip. It returns the
// archive path and the file names included.
func (s *GerberService) ExportToZip(ctx context.Context, projectID string) (zipPath string, files []string, err error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", nil, fmt.Errorf("export gerber : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", nil, fmt.Errorf("export gerber : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return "", nil, fmt.Errorf("export gerber : le projet %q n'a pas de carte", projectID)
	}

	outDir := filepath.Join(s.dataDir, "exports", projectID, "gerber")
	files, err = s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return "", nil, fmt.Errorf("export gerber : %w", err)
	}

	zipPath = filepath.Join(filepath.Dir(outDir), slugify(p.Name())+"-gerber.zip")
	if err := writeZip(zipPath, files); err != nil {
		return "", nil, fmt.Errorf("export gerber : archive : %w", err)
	}

	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	s.log.Info("export gerber zip", "project_id", projectID, "zip", zipPath, "files", len(names))
	return zipPath, names, nil
}

// writeZip packages files (by path) into an archive; entries are stored with
// their base name only.
func writeZip(zipPath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return fmt.Errorf("répertoire d'archive : %w", err)
	}
	out, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("création de l'archive : %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for _, f := range files {
		if err := appendToZip(zw, f); err != nil {
			zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalisation de l'archive : %w", err)
	}
	return out.Sync()
}

func appendToZip(zw *zip.Writer, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("lecture de %s : %w", path, err)
	}
	defer src.Close()

	entry, err := zw.Create(filepath.Base(path))
	if err != nil {
		return fmt.Errorf("entrée zip : %w", err)
	}
	if _, err := io.Copy(entry, src); err != nil {
		return fmt.Errorf("écriture de %s : %w", path, err)
	}
	return nil
}

// slugify converts a project name into a filesystem-friendly slug
// (lowercase, non-alphanumerics collapsed into '-').
func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := true // évite un tiret initial
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "projet"
	}
	return out
}
