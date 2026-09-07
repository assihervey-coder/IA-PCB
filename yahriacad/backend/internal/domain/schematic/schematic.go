package schematic

import (
	"fmt"
	"strings"
)

// Issue is a validation problem found on the schematic; it feeds the ERC
// use case (application/verification/erc.go).
type Issue struct {
	Code     string
	Severity Severity
	Message  string
	Ref      string // composant ou net concerné, le cas échéant
}

// Schematic is the container aggregate of the electrical design.
type Schematic struct {
	Name       string
	Components []Component
	Nets       []Net
}

// New returns an empty schematic.
func New(name string) *Schematic {
	return &Schematic{
		Name:       name,
		Components: []Component{},
		Nets:       []Net{},
	}
}

// AddComponent inserts a component, rejecting duplicates and empty refs.
func (s *Schematic) AddComponent(c Component) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, ok := s.ComponentByRef(c.Ref); ok {
		return fmt.Errorf("schéma : référence dupliquée %q", c.Ref)
	}
	s.Components = append(s.Components, c)
	return nil
}

// ComponentByRef looks a component up by reference designator.
func (s *Schematic) ComponentByRef(ref string) (*Component, bool) {
	for i := range s.Components {
		if s.Components[i].Ref == ref {
			return &s.Components[i], true
		}
	}
	return nil, false
}

// AddNet inserts a net, rejecting duplicates and blank names.
func (s *Schematic) AddNet(n Net) error {
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		return ErrEmptyNetName
	}
	if _, ok := s.NetByName(n.Name); ok {
		return fmt.Errorf("schéma : net dupliqué %q", n.Name)
	}
	if n.Class == "" {
		n.Class = ClassDefault
	}
	s.Nets = append(s.Nets, n)
	return nil
}

// NetByName looks a net up by name.
func (s *Schematic) NetByName(name string) (*Net, bool) {
	for i := range s.Nets {
		if s.Nets[i].Name == name {
			return &s.Nets[i], true
		}
	}
	return nil, false
}

// ComponentCount returns the number of placed symbols.
func (s *Schematic) ComponentCount() int { return len(s.Components) }

// NetCount returns the number of logical nets.
func (s *Schematic) NetCount() int { return len(s.Nets) }

// Validate performs the schematic-level checks consumed by the ERC use case.
// It never mutates the schematic.
func (s *Schematic) Validate() []Issue {
	var issues []Issue

	// Nets : nom vide, net relié à moins de deux broches.
	for i := range s.Nets {
		n := &s.Nets[i]
		if strings.TrimSpace(n.Name) == "" {
			issues = append(issues, Issue{
				Code:     "ERC_EMPTY_NET_NAME",
				Severity: SeverityError,
				Message:  "net sans nom",
			})
			continue
		}
		if len(n.Connections) < 2 {
			issues = append(issues, Issue{
				Code:     "ERC_SINGLE_PIN_NET",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("le net %q n'est relié qu'à une seule broche", n.Name),
				Ref:      n.Name,
			})
		}
	}

	// Composants : références dupliquées, empreinte manquante, isolation.
	seen := make(map[string]bool, len(s.Components))
	for i := range s.Components {
		c := &s.Components[i]
		if seen[c.Ref] {
			issues = append(issues, Issue{
				Code:     "ERC_DUPLICATE_REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("référence dupliquée %q", c.Ref),
				Ref:      c.Ref,
			})
		}
		seen[c.Ref] = true

		if strings.TrimSpace(c.Footprint) == "" {
			issues = append(issues, Issue{
				Code:     "ERC_MISSING_FOOTPRINT",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("le composant %q n'a pas d'empreinte affectée", c.Ref),
				Ref:      c.Ref,
			})
		}

		connected := false
		for j := range s.Nets {
			for _, conn := range s.Nets[j].Connections {
				if conn.ComponentRef == c.Ref {
					connected = true
					break
				}
			}
			if connected {
				break
			}
		}
		if !connected {
			issues = append(issues, Issue{
				Code:     "ERC_UNCONNECTED_COMPONENT",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("le composant %q n'est relié à aucun net", c.Ref),
				Ref:      c.Ref,
			})
		}
	}

	// Connexions pointant vers des composants inexistants.
	for i := range s.Nets {
		for _, conn := range s.Nets[i].Connections {
			if _, ok := s.ComponentByRef(conn.ComponentRef); !ok {
				issues = append(issues, Issue{
					Code:     "ERC_UNKNOWN_COMPONENT",
					Severity: SeverityError,
					Message: fmt.Sprintf("le net %q référence le composant inconnu %q",
						s.Nets[i].Name, conn.ComponentRef),
					Ref: conn.ComponentRef,
				})
			}
		}
	}

	return issues
}
