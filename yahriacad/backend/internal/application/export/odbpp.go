// Export ODB++ : le service construit l'arborescence ODB++ (via le port
// writer) puis l'empaquette en .tgz — le format d'échange standard des
// télématics/CAA. Le contenu .tgz est un job ODB++ directement ouvrable
// par les outils compatibles (structure simplifiée v8 documentée dans
// contracts.md §13).
package exportapp

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// ODBWriter is the port to the ODB++ generator (implemented by
// infrastructure/fileio/writer.ODBPPWriter). Write must create outDir when
// missing and return the produced file paths (absolute, under outDir).
type ODBWriter interface {
	Write(b *domainlayout.Board, projectName, outDir string) ([]string, error)
}

// ODBService exports the board of a project as a simplified ODB++ job,
// either as a directory tree or packaged into a .tgz archive.
type ODBService struct {
	projects domainproject.Repository
	writer   ODBWriter
	dataDir  string
	log      *slog.Logger
}

// NewODBService builds the ODB++ export use case.
func NewODBService(projects domainproject.Repository, w ODBWriter, dataDir string, log *slog.Logger) *ODBService {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &ODBService{projects: projects, writer: w, dataDir: dataDir, log: log}
}

// ExportToDir writes the ODB++ job tree of the project into outDir and
// returns the produced file paths.
func (s *ODBService) ExportToDir(ctx context.Context, projectID, outDir string) ([]string, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("export odb++ : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("export odb++ : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return nil, fmt.Errorf("export odb++ : le projet %q n'a pas de carte", projectID)
	}
	files, err := s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return nil, fmt.Errorf("export odb++ : %w", err)
	}
	s.log.Info("export odb++", "project_id", projectID, "dir", outDir, "files", len(files))
	return files, nil
}

// ExportToTgz writes the ODB++ job tree under
// <dataDir>/exports/<projectID>/odbpp, then packages it (tar+gzip, chemins
// relatifs préservés) into <dataDir>/exports/<projectID>/<name>-odbpp.tgz.
// It returns the archive path and the file names inside the job.
func (s *ODBService) ExportToTgz(ctx context.Context, projectID string) (tgzPath string, files []string, err error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", nil, fmt.Errorf("export odb++ : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", nil, fmt.Errorf("export odb++ : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return "", nil, fmt.Errorf("export odb++ : le projet %q n'a pas de carte", projectID)
	}

	outDir := filepath.Join(s.dataDir, "exports", projectID, "odbpp")
	produced, err := s.writer.Write(board, p.Name(), outDir)
	if err != nil {
		return "", nil, fmt.Errorf("export odb++ : %w", err)
	}

	tgzPath = filepath.Join(filepath.Dir(outDir), slugify(p.Name())+"-odbpp.tgz")
	if err := writeTarGz(tgzPath, outDir, produced); err != nil {
		return "", nil, fmt.Errorf("export odb++ : archive : %w", err)
	}

	names := make([]string, 0, len(produced))
	for _, f := range produced {
		rel, relErr := filepath.Rel(outDir, f)
		if relErr != nil {
			rel = filepath.Base(f)
		}
		names = append(names, filepath.ToSlash(rel))
	}
	s.log.Info("export odb++ tgz", "project_id", projectID, "tgz", tgzPath, "files", len(names))
	return tgzPath, names, nil
}

// writeTarGz packages the produced files into a gzipped tar, preserving
// their path relative to root (POSIX slashes).
func writeTarGz(tgzPath, root string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(tgzPath), 0o755); err != nil {
		return fmt.Errorf("répertoire d'archive : %w", err)
	}
	out, err := os.Create(tgzPath)
	if err != nil {
		return fmt.Errorf("création de l'archive : %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return fmt.Errorf("chemin relatif de %s : %w", f, err)
		}
		rel = filepath.ToSlash(rel)

		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("lecture de %s : %w", f, err)
		}
		hdr := &tar.Header{
			Name: rel,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("entrée tar %s : %w", rel, err)
		}
		if _, err := io.Copy(tw, strings.NewReader(string(content))); err != nil {
			return fmt.Errorf("écriture de %s : %w", rel, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("finalisation tar : %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("finalisation gzip : %w", err)
	}
	return out.Sync()
}
