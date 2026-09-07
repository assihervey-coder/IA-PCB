package constraints

import (
	"fmt"
	"sort"
	"strings"
)

// ConstraintSet is the named collection of design rules of a project.
type ConstraintSet struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

// NewDefault returns the factory default constraint set (metric units,
// conservative hobby-class values).
func NewDefault() *ConstraintSet {
	full := func(t RuleType, id, name string, v float64) Rule {
		return Rule{
			ID:       id,
			Name:     name,
			Type:     t,
			Scope:    Scope{NetClass: AnyNetClass, Layer: AnyLayer},
			ValueMM:  v,
			Severity: SeverityError,
			Enabled:  true,
		}
	}
	return &ConstraintSet{
		Name: "default",
		Rules: []Rule{
			full(RuleMinClearance, "def-clearance", "Isolation minimale entre nets", 0.2),
			full(RuleMinTrackWidth, "def-track-width", "Largeur de piste minimale", 0.2),
			full(RuleMinViaDiameter, "def-via-diameter", "Diamètre de via minimal", 0.6),
			full(RuleMinDrill, "def-drill", "Perçage minimal", 0.3),
			full(RuleMinAnnularRing, "def-annular", "Anneau annulaire minimal", 0.15),
			full(RuleEdgeClearance, "def-edge", "Distance cuivre / bord de carte", 0.5),
		},
	}
}

// Clone deep-copies the constraint set.
func (cs *ConstraintSet) Clone() *ConstraintSet {
	if cs == nil {
		return NewDefault()
	}
	out := &ConstraintSet{Name: cs.Name, Rules: make([]Rule, len(cs.Rules))}
	copy(out.Rules, cs.Rules)
	return out
}

// Add appends a rule, rejecting duplicate identifiers.
func (cs *ConstraintSet) Add(r Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	for _, existing := range cs.Rules {
		if existing.ID == r.ID {
			return fmt.Errorf("contraintes : règle dupliquée %q", r.ID)
		}
	}
	cs.Rules = append(cs.Rules, r)
	return nil
}

// RulesFor returns every enabled rule of the given type applying to the net
// class / layer, most specific first.
func (cs *ConstraintSet) RulesFor(t RuleType, netClass string, layer int) []Rule {
	var out []Rule
	for _, r := range cs.Rules {
		if r.Type == t && r.AppliesTo(netClass, layer) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].specificity() > out[j].specificity()
	})
	return out
}

// Value returns the effective value for a rule type: the most specific
// matching rule wins. ok is false when no rule applies.
func (cs *ConstraintSet) Value(t RuleType, netClass string, layer int) (float64, bool) {
	rules := cs.RulesFor(t, netClass, layer)
	if len(rules) == 0 {
		return 0, false
	}
	return rules[0].ValueMM, true
}

// SetMinTrackWidth upserts the min-track-width rule for a target: "all"
// (or empty) rewrites the wildcard rule, a net-class name upserts a scoped
// rule. Used by the Magic Copilot ("largeur de piste 0.3 mm").
func (cs *ConstraintSet) SetMinTrackWidth(mm float64, target string) {
	target = strings.TrimSpace(target)
	if target == "" || target == "all" || target == "*" {
		for i := range cs.Rules {
			if cs.Rules[i].Type == RuleMinTrackWidth && cs.Rules[i].Scope.NetClass == AnyNetClass {
				cs.Rules[i].ValueMM = mm
				return
			}
		}
		_ = cs.Add(Rule{
			ID: "def-track-width", Name: "Largeur de piste minimale",
			Type: RuleMinTrackWidth, Scope: Scope{NetClass: AnyNetClass, Layer: AnyLayer},
			ValueMM: mm, Severity: SeverityError, Enabled: true,
		})
		return
	}
	id := "class-" + target + "-width"
	for i := range cs.Rules {
		if cs.Rules[i].ID == id {
			cs.Rules[i].ValueMM = mm
			return
		}
	}
	_ = cs.Add(Rule{
		ID: id, Name: "Largeur minimale (classe " + target + ")",
		Type: RuleMinTrackWidth, Scope: Scope{NetClass: target, Layer: AnyLayer},
		ValueMM: mm, Severity: SeverityError, Enabled: true,
	})
}

// Validate reports the identifiers of inconsistent rules (empty ID, invalid
// value, duplicated ID).
func (cs *ConstraintSet) Validate() []string {
	var problems []string
	seen := make(map[string]bool, len(cs.Rules))
	for _, r := range cs.Rules {
		if err := r.Validate(); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if seen[r.ID] {
			problems = append(problems, fmt.Sprintf("contraintes : règle dupliquée %q", r.ID))
		}
		seen[r.ID] = true
	}
	return problems
}
