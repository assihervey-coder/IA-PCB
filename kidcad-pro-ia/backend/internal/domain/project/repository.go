package project

import "context"

// ErrNotFound must be returned by Repository implementations when the
// requested project does not exist. Use cases rely on errors.Is to map it
// onto an HTTP 404.
//
//	var ErrNotFound = errors.New("projet : introuvable")

// Repository is the persistence port of the Project aggregate. Implementations
// must persist the metadata AND the associated schematic / board / constraint
// set (as embedded JSON documents, for instance).
type Repository interface {
	// Create inserts a new project. It must fail if the ID already exists.
	Create(ctx context.Context, p *Project) error

	// Update replaces the stored state of an existing project.
	Update(ctx context.Context, p *Project) error

	// FindByID returns the project or ErrNotFound.
	FindByID(ctx context.Context, id string) (*Project, error)

	// List returns up to limit projects starting at offset, plus the total
	// number of projects. limit <= 0 means "no limit".
	List(ctx context.Context, limit, offset int) ([]*Project, int, error)

	// Delete removes the project. Deleting an unknown project is a no-op.
	Delete(ctx context.Context, id string) error
}
