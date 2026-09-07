// Package project contains the Project aggregate root: metadata, layer
// stack and references to the schematic, board (layout) and constraint set
// that together compose a complete PCB design.
package project

import (
	"errors"
	"strings"
	"time"

	"github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	"github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	"github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/utils"
)

// Status is the lifecycle status of a project.
type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

// LayerType classifies a board layer.
type LayerType string

const (
	LayerSignal LayerType = "signal"
	LayerPlane  LayerType = "plane"
	LayerSilk   LayerType = "silk"
	LayerMask   LayerType = "mask"
	LayerEdge   LayerType = "edge"
)

// Supported layer count bounds.
const (
	MinLayers = 1
	MaxLayers = 34
)

var (
	ErrInvalidName   = errors.New("projet : le nom ne doit pas être vide")
	ErrInvalidLayers = errors.New("projet : le nombre de couches doit être compris entre 1 et 34")
)

// Layer describes one entry of the copper layer stack.
type Layer struct {
	Index int
	Name  string
	Type  LayerType
}

// LayerStack is the ordered list of copper layers of a board.
type LayerStack []Layer

// DefaultLayerStack returns the canonical copper layer names for n layers:
// F.Cu, In1.Cu, ..., B.Cu.
func DefaultLayerStack(n int) LayerStack {
	stack := make(LayerStack, 0, n)
	for i := 0; i < n; i++ {
		name := "F.Cu"
		switch {
		case i == 0:
			name = "F.Cu"
		case i == n-1 && n > 1:
			name = "B.Cu"
		default:
			name = "In" + itoa(i) + ".Cu"
		}
		stack = append(stack, Layer{Index: i, Name: name, Type: LayerSignal})
	}
	return stack
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// Snapshot is a plain serialisable view of the project metadata (used by
// persistence adapters; rich content is serialised separately).
type Snapshot struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	LayerCount  int       `json:"layer_count"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Project is the aggregate root of a PCB design. It owns the schematic, the
// physical board and the constraint set. Persistence adapters rehydrate it
// via FromSnapshot + the association setters.
type Project struct {
	id          string
	name        string
	description string
	layerCount  int
	status      Status
	createdAt   time.Time
	updatedAt   time.Time

	schematic   *schematic.Schematic
	board       *layout.Board
	constraints *constraints.ConstraintSet
}

// New creates a validated project with a fresh identifier.
func New(name, description string, layerCount int) (*Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	if layerCount < MinLayers || layerCount > MaxLayers {
		return nil, ErrInvalidLayers
	}
	now := time.Now().UTC()
	return &Project{
		id:          utils.NewID(),
		name:        name,
		description: description,
		layerCount:  layerCount,
		status:      StatusActive,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// FromSnapshot rehydrates a project from its persisted metadata view.
func FromSnapshot(s Snapshot) *Project {
	status := Status(s.Status)
	if status != StatusActive && status != StatusArchived {
		status = StatusActive
	}
	return &Project{
		id:          s.ID,
		name:        s.Name,
		description: s.Description,
		layerCount:  s.LayerCount,
		status:      status,
		createdAt:   s.CreatedAt,
		updatedAt:   s.UpdatedAt,
	}
}

// Snapshot returns the serialisable metadata view of the project.
func (p *Project) Snapshot() Snapshot {
	return Snapshot{
		ID:          p.id,
		Name:        p.name,
		Description: p.description,
		LayerCount:  p.layerCount,
		Status:      string(p.status),
		CreatedAt:   p.createdAt,
		UpdatedAt:   p.updatedAt,
	}
}

// ID returns the unique identifier.
func (p *Project) ID() string { return p.id }

// Name returns the project name.
func (p *Project) Name() string { return p.name }

// Description returns the project description.
func (p *Project) Description() string { return p.description }

// LayerCount returns the number of copper layers.
func (p *Project) LayerCount() int { return p.layerCount }

// Status returns the lifecycle status.
func (p *Project) Status() Status { return p.status }

// CreatedAt returns the creation timestamp (UTC).
func (p *Project) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns the last modification timestamp (UTC).
func (p *Project) UpdatedAt() time.Time { return p.updatedAt }

// Layers returns the canonical copper layer stack.
func (p *Project) Layers() LayerStack { return DefaultLayerStack(p.layerCount) }

// Rename changes the project name after validation.
func (p *Project) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalidName
	}
	if name == p.name {
		return nil
	}
	p.name = name
	p.touch()
	return nil
}

// SetDescription updates the free-form description.
func (p *Project) SetDescription(description string) {
	p.description = description
	p.touch()
}

// Archive marks the project as archived (idempotent).
func (p *Project) Archive() {
	if p.status == StatusArchived {
		return
	}
	p.status = StatusArchived
	p.touch()
}

// Restore brings an archived project back to active state.
func (p *Project) Restore() {
	if p.status == StatusActive {
		return
	}
	p.status = StatusActive
	p.touch()
}

// IsArchived reports whether the project is archived.
func (p *Project) IsArchived() bool { return p.status == StatusArchived }

// SetSchematic attaches (or replaces) the electrical design.
func (p *Project) SetSchematic(s *schematic.Schematic) {
	p.schematic = s
	p.touch()
}

// Schematic returns the electrical design (nil if not imported yet).
func (p *Project) Schematic() *schematic.Schematic { return p.schematic }

// SetBoard attaches (or replaces) the physical board.
func (p *Project) SetBoard(b *layout.Board) {
	p.board = b
	p.touch()
}

// Board returns the physical board (nil if not imported yet).
func (p *Project) Board() *layout.Board { return p.board }

// SetConstraints attaches (or replaces) the design rule set.
func (p *Project) SetConstraints(cs *constraints.ConstraintSet) {
	p.constraints = cs
	p.touch()
}

// Constraints returns the design rule set (nil if none).
func (p *Project) Constraints() *constraints.ConstraintSet { return p.constraints }

// touch refreshes the modification timestamp.
func (p *Project) touch() { p.updatedAt = time.Now().UTC() }
