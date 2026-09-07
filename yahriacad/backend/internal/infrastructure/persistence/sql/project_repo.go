// Package sql is the PostgreSQL implementation of the project repository
// port, backed by database/sql + the pgx v5 stdlib driver. The rich
// documents (schematic, layout, constraints) are stored as JSONB columns
// serialised with the YahriaCad interchange format (backend/pkg/pcb-format).
package sql

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	// Enregistre le driver "pgx" pour database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	"github.com/assihervey-coder/IA-PCB/backend/pkg/pcb-format"
)

//go:embed migrations/001_init.sql migrations/002_indexes.sql
var migrationFS embed.FS

// migrations are applied in order at adapter creation (idempotent DDL).
var migrations = []string{"migrations/001_init.sql", "migrations/002_indexes.sql"}

// Compile-time check: the adapter satisfies the domain port.
var _ domainproject.Repository = (*Repository)(nil)

// Repository persists projects in PostgreSQL.
type Repository struct {
	db *sql.DB
}

// NewProjectRepository opens the connection, applies the migrations and
// returns the repository.
func NewProjectRepository(dsn string) (domainproject.Repository, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("sql : DSN vide")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql : ouverture de la connexion : %w", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql : base injoignable : %w", err)
	}
	r := &Repository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the connection pool.
func (r *Repository) Close() error { return r.db.Close() }

// migrate executes the embedded SQL migrations in order.
func (r *Repository) migrate(ctx context.Context) error {
	for _, name := range migrations {
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("sql : migration %s illisible : %w", name, err)
		}
		if _, err := r.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("sql : migration %s : %w", name, err)
		}
	}
	return nil
}

// Create inserts a new project; an existing identifier yields ErrDuplicate.
func (r *Repository) Create(ctx context.Context, p *domainproject.Project) error {
	schematicJSON, err := marshalSchematic(p.Schematic())
	if err != nil {
		return fmt.Errorf("sql : sérialisation du schéma : %w", err)
	}
	layoutJSON, err := marshalBoard(p.Board())
	if err != nil {
		return fmt.Errorf("sql : sérialisation du layout : %w", err)
	}
	constraintsJSON, err := marshalConstraints(p.Constraints())
	if err != nil {
		return fmt.Errorf("sql : sérialisation des contraintes : %w", err)
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO projects
			(id, name, description, layer_count, status, created_at, updated_at,
			 schematic, layout, "constraints")
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		p.ID(), p.Name(), p.Description(), p.LayerCount(), string(p.Status()),
		p.CreatedAt(), p.UpdatedAt(),
		schematicJSON, layoutJSON, constraintsJSON)
	if err != nil {
		return fmt.Errorf("sql : insertion du projet : %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w : %s", apperrors.ErrDuplicate, p.ID())
	}
	return nil
}

// Update replaces the stored state of an existing project.
func (r *Repository) Update(ctx context.Context, p *domainproject.Project) error {
	schematicJSON, err := marshalSchematic(p.Schematic())
	if err != nil {
		return fmt.Errorf("sql : sérialisation du schéma : %w", err)
	}
	layoutJSON, err := marshalBoard(p.Board())
	if err != nil {
		return fmt.Errorf("sql : sérialisation du layout : %w", err)
	}
	constraintsJSON, err := marshalConstraints(p.Constraints())
	if err != nil {
		return fmt.Errorf("sql : sérialisation des contraintes : %w", err)
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE projects SET
			name = $2, description = $3, layer_count = $4, status = $5,
			updated_at = $6, schematic = $7, layout = $8, "constraints" = $9
		WHERE id = $1::uuid`,
		p.ID(), p.Name(), p.Description(), p.LayerCount(), string(p.Status()),
		p.UpdatedAt(), schematicJSON, layoutJSON, constraintsJSON)
	if err != nil {
		return fmt.Errorf("sql : mise à jour du projet : %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("sql : %w (%s)", apperrors.ErrNotFound, p.ID())
	}
	return nil
}

// FindByID rehydrates a project (metadata + rich documents) or returns
// ErrNotFound.
func (r *Repository) FindByID(ctx context.Context, id string) (*domainproject.Project, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, layer_count, status, created_at, updated_at,
		       schematic, layout, "constraints"
		FROM projects WHERE id = $1::uuid`, id)
	return r.scanProject(row.Scan)
}

// List returns up to limit projects starting at offset, plus the total
// count. limit <= 0 means "no limit".
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*domainproject.Project, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sql : comptage des projets : %w", err)
	}

	var limitArg, offsetArg any
	if limit > 0 {
		limitArg = limit
	}
	if offset > 0 {
		offsetArg = offset
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, layer_count, status, created_at, updated_at,
		       schematic, layout, "constraints"
		FROM projects
		ORDER BY created_at
		LIMIT $1 OFFSET $2`, limitArg, offsetArg)
	if err != nil {
		return nil, 0, fmt.Errorf("sql : liste des projets : %w", err)
	}
	defer rows.Close()

	out := []*domainproject.Project{}
	for rows.Next() {
		p, err := r.scanProject(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("sql : liste des projets : %w", err)
	}
	return out, total, nil
}

// Delete removes the project; deleting an unknown identifier is a no-op.
func (r *Repository) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1::uuid`, id); err != nil {
		return fmt.Errorf("sql : suppression du projet : %w", err)
	}
	return nil
}

// scanProject hydrates one row through the provided scan function (works for
// both QueryRow and Query cursors).
func (r *Repository) scanProject(scan func(dest ...any) error) (*domainproject.Project, error) {
	var (
		id, name, description, status string
		layerCount                    int
		createdAt, updatedAt          time.Time
		schematicRaw, layoutRaw       []byte
		constraintsRaw                []byte
	)
	err := scan(&id, &name, &description, &layerCount, &status, &createdAt, &updatedAt,
		&schematicRaw, &layoutRaw, &constraintsRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sql : %w", apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("sql : lecture du projet : %w", err)
	}

	p := domainproject.FromSnapshot(domainproject.Snapshot{
		ID:          id,
		Name:        name,
		Description: description,
		LayerCount:  layerCount,
		Status:      status,
		CreatedAt:   createdAt.UTC(),
		UpdatedAt:   updatedAt.UTC(),
	})

	if sch, err := unmarshalSchematic(schematicRaw); err != nil {
		return nil, fmt.Errorf("sql : schéma du projet %s : %w", id, err)
	} else if sch != nil {
		p.SetSchematic(sch)
	}
	if board, err := unmarshalBoard(layoutRaw); err != nil {
		return nil, fmt.Errorf("sql : layout du projet %s : %w", id, err)
	} else if board != nil {
		p.SetBoard(board)
	}
	if cs, err := unmarshalConstraints(constraintsRaw); err != nil {
		return nil, fmt.Errorf("sql : contraintes du projet %s : %w", id, err)
	} else if cs != nil {
		p.SetConstraints(cs)
	}
	return p, nil
}

// --------------------------------------------------------------
// Sérialisation / désérialisation des documents riches
// --------------------------------------------------------------

// marshalSchematic wraps the schematic DTO in an interchange File and
// encodes it (pcbformat.Encode fills format/version).
func marshalSchematic(s *domainschematic.Schematic) (string, error) {
	if s == nil {
		return "", nil
	}
	data, err := pcbformat.Encode(&pcbformat.File{Schematic: pcbformat.FromSchematic(s)})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// marshalBoard wraps the layout DTO in an interchange File and encodes it.
func marshalBoard(b *domainlayout.Board) (string, error) {
	if b == nil {
		return "", nil
	}
	data, err := pcbformat.Encode(&pcbformat.File{Layout: pcbformat.FromBoard(b)})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// marshalConstraints serialises the constraint set (json tags provided by
// the domain).
func marshalConstraints(cs *domainconstraints.ConstraintSet) (string, error) {
	if cs == nil {
		return "", nil
	}
	data, err := json.Marshal(cs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func emptyOrNull(raw []byte) bool {
	return len(raw) == 0 || string(raw) == "null"
}

func unmarshalSchematic(raw []byte) (*domainschematic.Schematic, error) {
	if emptyOrNull(raw) {
		return nil, nil
	}
	f, err := pcbformat.Decode(raw)
	if err != nil {
		return nil, err
	}
	return pcbformat.ToSchematic(f.Schematic)
}

func unmarshalBoard(raw []byte) (*domainlayout.Board, error) {
	if emptyOrNull(raw) {
		return nil, nil
	}
	f, err := pcbformat.Decode(raw)
	if err != nil {
		return nil, err
	}
	if f.Layout == nil {
		return nil, nil
	}
	return pcbformat.ToBoard(f.Layout)
}

func unmarshalConstraints(raw []byte) (*domainconstraints.ConstraintSet, error) {
	if emptyOrNull(raw) {
		return nil, nil
	}
	cs := &domainconstraints.ConstraintSet{}
	if err := json.Unmarshal(raw, cs); err != nil {
		return nil, err
	}
	return cs, nil
}
