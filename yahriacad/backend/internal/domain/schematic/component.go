// Package schematic holds the electrical design: components, nets and the
// schematic container with its intrinsic validation rules (ERC primitives).
package schematic

import "errors"

// ErrMissingRef is raised when a component has an empty reference designator.
var ErrMissingRef = errors.New("schéma : référence de composant vide")

// Severity qualifies the gravity of a validation issue.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Pin is an electrical pin of a symbol. Coordinates are sheet coordinates
// relative to the component origin, in millimeters.
type Pin struct {
	Number string
	Name   string
	X, Y   float64
}

// Component is a symbol instance placed on the schematic sheet.
type Component struct {
	Ref       string // "R1", "U2", ...
	Value     string // "10k", "NE555", ...
	Footprint string // nom d'empreinte cible, peut être vide avant affectation
	X, Y      float64
	Rotation  float64
	Fixed     bool
	Pins      []Pin
}

// Validate checks the intrinsic consistency of the component.
func (c *Component) Validate() error {
	if c.Ref == "" {
		return ErrMissingRef
	}
	return nil
}
