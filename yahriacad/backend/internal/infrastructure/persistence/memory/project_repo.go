// Package memory is the in-memory implementation of the project repository
// port. It stores one clone of each project (metadata rebuilt through
// FromSnapshot plus the schematic / board / constraints pointers) so the
// caller can keep mutating its own instance afterwards.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// Repository keeps the projects in a guarded map.
type Repository struct {
	mu       sync.RWMutex
	projects map[string]*domainproject.Project
}

// Compile-time check: the adapter satisfies the domain port.
var _ domainproject.Repository = (*Repository)(nil)

// NewProjectRepository returns an empty in-memory repository.
func NewProjectRepository() domainproject.Repository {
	return &Repository{projects: make(map[string]*domainproject.Project)}
}

// Create inserts a new project, rejecting duplicated identifiers.
func (r *Repository) Create(_ context.Context, p *domainproject.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.projects[p.ID()]; exists {
		return fmt.Errorf("%w : %s", apperrors.ErrDuplicate, p.ID())
	}
	r.projects[p.ID()] = cloneProject(p)
	return nil
}

// Update replaces the stored state of an existing project.
func (r *Repository) Update(_ context.Context, p *domainproject.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.projects[p.ID()]; !exists {
		return fmt.Errorf("memoire : mise à jour impossible, projet %s introuvable : %w", p.ID(), apperrors.ErrNotFound)
	}
	r.projects[p.ID()] = cloneProject(p)
	return nil
}

// FindByID returns a copy of the stored project or ErrNotFound.
func (r *Repository) FindByID(_ context.Context, id string) (*domainproject.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.projects[id]
	if !ok {
		return nil, fmt.Errorf("memoire : %w (%s)", apperrors.ErrNotFound, id)
	}
	return cloneProject(p), nil
}

// List returns up to limit projects starting at offset, plus the total
// count. limit <= 0 means "no limit". Results are ordered by creation time.
func (r *Repository) List(_ context.Context, limit, offset int) ([]*domainproject.Project, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*domainproject.Project, 0, len(r.projects))
	for _, p := range r.projects {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt().Before(all[j].CreatedAt())
	})

	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*domainproject.Project{}, total, nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	out := make([]*domainproject.Project, 0, len(all))
	for _, p := range all {
		out = append(out, cloneProject(p))
	}
	return out, total, nil
}

// Delete removes the project; deleting an unknown identifier is a no-op.
func (r *Repository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.projects, id)
	return nil
}

// cloneProject rebuilds a project from its snapshot and re-attaches the
// schematic / board / constraint pointers (shared references are an accepted
// simplification for the in-memory adapter; the SQL adapter persists the
// full documents instead).
func cloneProject(p *domainproject.Project) *domainproject.Project {
	out := domainproject.FromSnapshot(p.Snapshot())
	if s := p.Schematic(); s != nil {
		out.SetSchematic(s)
	}
	if b := p.Board(); b != nil {
		out.SetBoard(b)
	}
	if cs := p.Constraints(); cs != nil {
		out.SetConstraints(cs)
	}
	return out
}

// ErrDuplicate is kept for convenience so callers of this adapter can use a
// package-local alias (the canonical sentinel lives in application).
var ErrDuplicate = apperrors.ErrDuplicate
