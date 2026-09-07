// Package constraints models the design rules (DRC) attached to a project:
// clearances, track widths, via geometry and edge distances, with per net
// class / layer scoping.
package constraints

import (
	"errors"
	"fmt"
	"strings"
)

// RuleType enumerates the supported design rules.
type RuleType string

const (
	RuleMinClearance   RuleType = "min_clearance"
	RuleMinTrackWidth  RuleType = "min_track_width"
	RuleMaxTrackWidth  RuleType = "max_track_width"
	RuleMinViaDiameter RuleType = "min_via_diameter"
	RuleMinDrill       RuleType = "min_drill"
	RuleMinAnnularRing RuleType = "min_annular_ring"
	RuleEdgeClearance  RuleType = "edge_clearance"
)

// Severity of a rule violation.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// AnyNetClass and AnyLayer are the scope wildcards.
const (
	AnyNetClass = "*"
	AnyLayer    = -1
)

// Scope narrows a rule to a net class and/or a copper layer.
type Scope struct {
	NetClass string `json:"net_class"`
	Layer    int    `json:"layer"`
}

// Rule is a single design rule. A value of zero/unknown type is rejected by
// Validate.
type Rule struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     RuleType `json:"type"`
	Scope    Scope    `json:"scope"`
	ValueMM  float64  `json:"value_mm"`
	Severity Severity `json:"severity"`
	Enabled  bool     `json:"enabled"`
}

// AppliesTo reports whether the (enabled) rule applies to the given net
// class and copper layer.
func (r Rule) AppliesTo(netClass string, layer int) bool {
	if !r.Enabled {
		return false
	}
	if r.Scope.NetClass != AnyNetClass && r.Scope.NetClass != "" && r.Scope.NetClass != netClass {
		return false
	}
	if r.Scope.Layer != AnyLayer && r.Scope.Layer >= 0 && r.Scope.Layer != layer {
		return false
	}
	return true
}

// specificity scores how precisely a rule matches (higher = more specific):
// +2 exact net class, +1 exact layer. Used to resolve conflicting rules.
func (r Rule) specificity() int {
	s := 0
	if r.Scope.NetClass != AnyNetClass && r.Scope.NetClass != "" {
		s += 2
	}
	if r.Scope.Layer != AnyLayer && r.Scope.Layer >= 0 {
		s++
	}
	return s
}

// Validate checks the intrinsic consistency of the rule.
func (r Rule) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("contraintes : identifiant de règle vide")
	}
	if r.ValueMM <= 0 {
		return fmt.Errorf("contraintes : valeur invalide pour la règle %q", r.ID)
	}
	if r.Severity != SeverityError && r.Severity != SeverityWarning {
		return fmt.Errorf("contraintes : gravité invalide pour la règle %q", r.ID)
	}
	return nil
}
