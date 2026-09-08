// netclass.go implements interactive routing support: the overview of
// every net with its class and effective constraints, the class assignment
// of a net and the per-class rule upserts. The autorouter consumes these
// values through RouteBoard: selecting nets ("ne router que USB_D+ et
// USB_D-") and constraining them ("les nets power à 0.5 mm") is exactly
// what this use case exposes.
package layoutapp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// KnownNetClasses lists the built-in net classes (schematic domain values).
var KnownNetClasses = []string{
	string(domainschematic.ClassDefault),
	string(domainschematic.ClassPower),
	string(domainschematic.ClassSignal),
	string(domainschematic.ClassHighSpeed),
}

// NetOverview is one net with its class and the effective constraint
// values the router will apply to it.
type NetOverview struct {
	Name      string          `json:"name"`
	Class     string          `json:"net_class"`
	PadCount  int             `json:"pad_count"`
	Effective EffectiveValues `json:"effective"`
	Routed    bool            `json:"routed"`
}

// EffectiveValues are the resolved per-class constraint values (falling
// back to the wildcard rules, then to the engine defaults).
type EffectiveValues struct {
	MinTrackWidthMM  float64 `json:"min_track_width_mm"`
	MinClearanceMM   float64 `json:"min_clearance_mm"`
	MinViaDiameterMM float64 `json:"min_via_diameter_mm"`
}

// ClassOverview is one net class with its scoped rules.
type ClassOverview struct {
	Name  string                   `json:"name"`
	Nets  int                      `json:"nets"`
	Rules []domainconstraints.Rule `json:"rules"`
}

// NetClassOverview is the response of GET .../netclasses.
type NetClassOverview struct {
	KnownClasses []string        `json:"known_classes"`
	Nets         []NetOverview   `json:"nets"`
	Classes      []ClassOverview `json:"classes"`
}

// ClassAssignRequest is the payload of PUT .../netclasses/{net}.
type ClassAssignRequest struct {
	NetClass string `json:"net_class"`
}

// ClassRulesRequest is the payload of PUT .../netclasses/{class}/rules.
// Zero values are ignored (only positive mm values are stored).
type ClassRulesRequest struct {
	MinTrackWidthMM  float64 `json:"min_track_width_mm"`
	MinClearanceMM   float64 `json:"min_clearance_mm"`
	MinViaDiameterMM float64 `json:"min_via_diameter_mm"`
	MinDrillMM       float64 `json:"min_drill_mm"`
}

// NetClassService manages net classes and their per-class constraints.
type NetClassService struct {
	projects domainproject.Repository
}

// NewNetClassService builds the net-class use case.
func NewNetClassService(projects domainproject.Repository) *NetClassService {
	return &NetClassService{projects: projects}
}

// Overview lists every net with class + effective values, and every class
// with its scoped rules.
func (s *NetClassService) Overview(ctx context.Context, projectID string) (*NetClassOverview, error) {
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	out := &NetClassOverview{
		KnownClasses: append([]string(nil), KnownNetClasses...),
		Nets:         []NetOverview{},
		Classes:      []ClassOverview{},
	}

	for _, n := range netsForProject(p, nil) {
		class := string(n.Class)
		if class == "" {
			class = string(domainschematic.ClassDefault)
		}
		ov := NetOverview{
			Name: n.Name, Class: class, PadCount: len(n.Connections),
			Effective: effectiveValues(cs, class),
		}
		out.Nets = append(out.Nets, ov)
	}

	// Comptage des nets par classe + règles scopées.
	counts := map[string]int{}
	for _, n := range out.Nets {
		counts[n.Class]++
	}
	for _, class := range cs.Classes() {
		out.Classes = append(out.Classes, ClassOverview{
			Name: class, Nets: counts[class], Rules: cs.ClassRules(class),
		})
	}
	sort.Slice(out.Nets, func(i, j int) bool { return out.Nets[i].Name < out.Nets[j].Name })
	return out, nil
}

// Assign changes the class of one net. The change is applied to the
// schematic net list (a net without schematic membership is rejected: the
// class is defined by the design, not by the router).
func (s *NetClassService) Assign(ctx context.Context, projectID, netName, class string) (*NetOverview, error) {
	if !validClass(class) {
		return nil, fmt.Errorf("classes de nets : classe %q inconnue (classes : %v)", class, KnownNetClasses)
	}
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	sch := p.Schematic()
	if sch == nil {
		return nil, fmt.Errorf("classes de nets : importez d'abord un schéma pour classer %q", netName)
	}
	found := false
	for i := range sch.Nets {
		if sch.Nets[i].Name == netName {
			sch.Nets[i].Class = domainschematic.NetClass(class)
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("classes de nets : net %q introuvable dans le schéma : %w", netName, apperrors.ErrNotFound)
	}

	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}
	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("classes de nets : persistance : %w", err)
	}
	return &NetOverview{
		Name: netName, Class: class,
		Effective: effectiveValues(cs, class),
	}, nil
}

// SetRules upserts the per-class design rules and persists the aggregate.
func (s *NetClassService) SetRules(ctx context.Context, projectID, class string, req ClassRulesRequest) ([]domainconstraints.Rule, error) {
	if !validClass(class) {
		return nil, fmt.Errorf("classes de nets : classe %q inconnue (classes : %v)", class, KnownNetClasses)
	}
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	type upsert struct {
		t domainconstraints.RuleType
		v float64
	}
	updates := []upsert{
		{domainconstraints.RuleMinTrackWidth, req.MinTrackWidthMM},
		{domainconstraints.RuleMinClearance, req.MinClearanceMM},
		{domainconstraints.RuleMinViaDiameter, req.MinViaDiameterMM},
		{domainconstraints.RuleMinDrill, req.MinDrillMM},
	}
	stored := []domainconstraints.Rule{}
	for _, u := range updates {
		if u.v <= 0 {
			continue
		}
		rule, err := cs.SetClassRule(u.t, class, u.v)
		if err != nil {
			return nil, fmt.Errorf("classes de nets : %w", err)
		}
		stored = append(stored, rule)
	}
	if len(stored) == 0 {
		return nil, errors.New("classes de nets : aucune valeur positive fournie")
	}

	p.SetConstraints(cs)
	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("classes de nets : persistance : %w", err)
	}
	return stored, nil
}

// --------------------------------------------------------------
// Helpers
// --------------------------------------------------------------

func (s *NetClassService) load(ctx context.Context, projectID string) (*domainproject.Project, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("classes de nets : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("classes de nets : chargement du projet : %w", err)
	}
	return p, nil
}

func validClass(class string) bool {
	for _, c := range KnownNetClasses {
		if c == class {
			return true
		}
	}
	return false
}

// effectiveValues resolves the constraint values the router will apply for
// a net of the given class (class-scoped rule > wildcard rule > engine
// defaults).
func effectiveValues(cs *domainconstraints.ConstraintSet, class string) EffectiveValues {
	const (
		fallbackWidth = 0.25
		fallbackClear = 0.2
		fallbackVia   = 0.6
	)
	out := EffectiveValues{MinTrackWidthMM: fallbackWidth, MinClearanceMM: fallbackClear, MinViaDiameterMM: fallbackVia}
	if cs == nil {
		return out
	}
	if v, ok := cs.Value(domainconstraints.RuleMinTrackWidth, class, domainconstraints.AnyLayer); ok {
		out.MinTrackWidthMM = v
	}
	if v, ok := cs.Value(domainconstraints.RuleMinClearance, class, domainconstraints.AnyLayer); ok {
		out.MinClearanceMM = v
	}
	if v, ok := cs.Value(domainconstraints.RuleMinViaDiameter, class, domainconstraints.AnyLayer); ok {
		out.MinViaDiameterMM = v
	}
	return out
}
