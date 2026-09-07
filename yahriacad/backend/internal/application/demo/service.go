// Package demo generates deliberate teaching material: the "nightmare
// board", a project seeded with classic design mistakes so the DRC
// Auto-Healer and the Design Doctor can be demonstrated end-to-end in one
// click. Every fault maps onto a repair action the platform actually
// implements (widen_track, enlarge_via, nudge_edge, RL routing).
package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// Fault codes surfaced in the report (aligned with the DRC / doctor codes).
const (
	FaultThinTrack    = "DRC_TRACK_WIDTH"
	FaultTinyVia      = "DRC_DRILL"
	FaultEdgeHug      = "DRC_EDGE_CLEARANCE"
	FaultUnrouted     = "ROUTING_MISSING"
	FaultHotCluster   = "THERMAL_CLUSTER"
	FaultNoConstraint = "CONSTRAINTS_MISSING"
)

// Fault is one seeded mistake with its repair path.
type Fault struct {
	Code        string `json:"code"`
	Where       string `json:"where"`
	Description string `json:"description"`
	Repair      string `json:"repair"` // action attendue de l'Auto-Healer / du routeur
}

// Report is the response of the nightmare generation.
type Report struct {
	ProjectID string       `json:"project_id"`
	Name      string       `json:"name"`
	Faults    []Fault      `json:"faults"`
	Board     BoardSummary `json:"board"`
	NextSteps []string     `json:"next_steps"`
}

// BoardSummary summarises the generated board.
type BoardSummary struct {
	WidthMM    float64 `json:"width_mm"`
	HeightMM   float64 `json:"height_mm"`
	LayerCount int     `json:"layer_count"`
	Components int     `json:"components"`
	Tracks     int     `json:"tracks"`
	Vias       int     `json:"vias"`
}

// Service builds the demo projects.
type Service struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewService builds the demo use case.
func NewService(projects domainproject.Repository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{projects: projects, log: log}
}

// Nightmare creates the deliberately bad board and returns its report.
// Le projet est un projet normal : toute la plateforme (DRC, Auto-Healer,
// Doctor, routage RL, exports) s'applique dessus sans cas spécial.
func (s *Service) Nightmare(ctx context.Context) (*Report, error) {
	p, err := domainproject.New(
		"Carte Cauchemar — démo Auto-Healer",
		"Projet de démonstration : fautes classiques semées pour montrer l'Auto-Healer DRC, le Design Doctor et le routage IA.",
		2,
	)
	if err != nil {
		return nil, fmt.Errorf("démo : création du projet : %w", err)
	}

	// Schéma : nets d'alimentation, bus I2C et deux nets sans routage.
	sch := domainschematic.New("cauchemar")
	// Les composants sont déclarés côté schéma avec les mêmes décalages
	// de broches que leurs empreintes board (miroir obligatoire : l'arène
	// et le benchmark localisent les broches via sch.ComponentByRef).
	addComp := func(ref string, pins ...domainschematic.Pin) {
		_ = sch.AddComponent(domainschematic.Component{Ref: ref, Pins: pins})
	}
	addComp("U1",
		domainschematic.Pin{Number: "1", Name: "SDA", X: -1.905, Y: 2.7},
		domainschematic.Pin{Number: "2", Name: "SCL", X: -0.635, Y: 2.7},
		domainschematic.Pin{Number: "4", Name: "GND", X: 1.905, Y: 2.7},
		domainschematic.Pin{Number: "8", Name: "VCC", X: -1.905, Y: -2.7},
	)
	addComp("J1",
		domainschematic.Pin{Number: "1", Name: "VCC", X: -3, Y: 0},
		domainschematic.Pin{Number: "2", Name: "GND", X: -1, Y: 0},
		domainschematic.Pin{Number: "3", Name: "SDA", X: 1, Y: 0},
		domainschematic.Pin{Number: "4", Name: "SCL", X: 3, Y: 0},
	)
	addComp("R1",
		domainschematic.Pin{Number: "1", Name: "A", X: -0.8, Y: 0},
		domainschematic.Pin{Number: "2", Name: "B", X: 0.8, Y: 0},
	)
	addComp("R2",
		domainschematic.Pin{Number: "1", Name: "A", X: -0.8, Y: 0},
		domainschematic.Pin{Number: "2", Name: "B", X: 0.8, Y: 0},
	)
	addComp("C1",
		domainschematic.Pin{Number: "1", Name: "A", X: -0.8, Y: 0},
		domainschematic.Pin{Number: "2", Name: "B", X: 0.8, Y: 0},
	)
	addNet := func(name string, class domainschematic.NetClass, pins ...domainschematic.PinRef) {
		_ = sch.AddNet(domainschematic.Net{Name: name, Class: class, Connections: pins})
	}
	addNet("VCC", domainschematic.ClassPower,
		domainschematic.PinRef{ComponentRef: "U1", PinNumber: "8"},
		domainschematic.PinRef{ComponentRef: "J1", PinNumber: "1"})
	addNet("GND", domainschematic.ClassPower,
		domainschematic.PinRef{ComponentRef: "U1", PinNumber: "4"},
		domainschematic.PinRef{ComponentRef: "J1", PinNumber: "2"})
	addNet("SDA", domainschematic.ClassSignal,
		domainschematic.PinRef{ComponentRef: "U1", PinNumber: "1"},
		domainschematic.PinRef{ComponentRef: "J1", PinNumber: "3"})
	addNet("SCL", domainschematic.ClassSignal,
		domainschematic.PinRef{ComponentRef: "U1", PinNumber: "2"},
		domainschematic.PinRef{ComponentRef: "J1", PinNumber: "4"})
	addNet("N1", domainschematic.ClassDefault,
		domainschematic.PinRef{ComponentRef: "R1", PinNumber: "1"},
		domainschematic.PinRef{ComponentRef: "R2", PinNumber: "2"})
	addNet("N2", domainschematic.ClassDefault,
		domainschematic.PinRef{ComponentRef: "R2", PinNumber: "1"},
		domainschematic.PinRef{ComponentRef: "C1", PinNumber: "1"})
	p.SetSchematic(sch)

	board, err := domainlayout.NewBoard(50, 40, 2)
	if err != nil {
		return nil, fmt.Errorf("démo : création de la carte : %w", err)
	}

	// Empreintes minimales mais réalistes (pads rattachés aux nets).
	soic8 := domainlayout.Footprint{
		Name: "SOIC-8", Value: "capteur", BodyWidthMM: 4.9, BodyHeightMM: 3.9,
		Pads: []domainlayout.Pad{
			{Name: "1", X: -1.905, Y: 2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "SDA"},
			{Name: "2", X: -0.635, Y: 2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "SCL"},
			{Name: "3", X: 0.635, Y: 2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "N2"},
			{Name: "4", X: 1.905, Y: 2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "GND"},
			{Name: "5", X: 1.905, Y: -2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "N1"},
			{Name: "6", X: 0.635, Y: -2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: ""},
			{Name: "7", X: -0.635, Y: -2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: ""},
			{Name: "8", X: -1.905, Y: -2.7, Width: 0.6, Height: 1.5, Layer: 0, Net: "VCC"},
		},
	}
	r0603 := func(netA, netB string) domainlayout.Footprint {
		return domainlayout.Footprint{
			Name: "R0603", Value: "10k", BodyWidthMM: 1.6, BodyHeightMM: 0.8,
			Pads: []domainlayout.Pad{
				{Name: "1", X: -0.8, Y: 0, Width: 0.6, Height: 0.6, Layer: 0, Net: netA},
				{Name: "2", X: 0.8, Y: 0, Width: 0.6, Height: 0.6, Layer: 0, Net: netB},
			},
		}
	}
	conn4 := domainlayout.Footprint{
		Name: "JST-4", Value: "bus", BodyWidthMM: 9, BodyHeightMM: 4,
		Pads: []domainlayout.Pad{
			{Name: "1", X: -3, Y: 0, Width: 1, Height: 1.6, Layer: 0, Net: "VCC"},
			{Name: "2", X: -1, Y: 0, Width: 1, Height: 1.6, Layer: 0, Net: "GND"},
			{Name: "3", X: 1, Y: 0, Width: 1, Height: 1.6, Layer: 0, Net: "SDA"},
			{Name: "4", X: 3, Y: 0, Width: 1, Height: 1.6, Layer: 0, Net: "SCL"},
		},
	}
	place := func(ref string, fp domainlayout.Footprint, x, y float64) {
		if err := board.AddComponent(domainlayout.PlacedComponent{
			Ref: ref, Footprint: fp, X: x, Y: y,
		}); err != nil {
			s.log.Warn("démo : composant ignoré", "ref", ref, "err", err)
		}
	}
	place("U1", soic8, 15, 20)
	place("R1", r0603("N1", ""), 30, 12)
	place("R2", r0603("N2", "N1"), 30, 20)
	place("C1", r0603("N2", "GND"), 30, 28)
	place("J1", conn4, 44, 20)

	// --- Faute 1 : pistes trop fines (0.08 mm << 0.2 mm) -----------------
	board.AddTrack(domainlayout.Track{Net: "VCC", Layer: 0, Width: 0.08,
		Points: []domainlayout.TrackPoint{{X: 13.095, Y: 17.3}, {X: 41, Y: 20}}})
	board.AddTrack(domainlayout.Track{Net: "SDA", Layer: 0, Width: 0.12,
		Points: []domainlayout.TrackPoint{{X: 13.095, Y: 22.7}, {X: 43, Y: 20}}})

	// --- Faute 2 : piste collée au bord (0.2 mm << 0.5 mm) ---------------
	board.AddTrack(domainlayout.Track{Net: "SCL", Layer: 0, Width: 0.25,
		Points: []domainlayout.TrackPoint{{X: 14.365, Y: 0.2}, {X: 14.365, Y: 22.7}}})

	// --- Faute 3 : via sous-dimensionné (perçage 0.12 mm << 0.3 mm) ------
	if err := board.AddVia(domainlayout.Via{
		Net: "GND", X: 20, Y: 27, FromLayer: 0, ToLayer: 1,
		Diameter: 0.25, Drill: 0.12,
	}); err != nil {
		s.log.Warn("démo : via ignoré", "err", err)
	}
	if err := board.AddVia(domainlayout.Via{
		Net: "VCC", X: 25, Y: 14, FromLayer: 0, ToLayer: 1,
		Diameter: 0.3, Drill: 0.15,
	}); err != nil {
		s.log.Warn("démo : via ignoré", "err", err)
	}

	// --- Faute 4 : nets N1/N2 laissés sans routage -----------------------
	// (aucune piste : le Doctor proposera le routage IA / RL)

	p.SetBoard(board)
	// --- Faute 5 : aucun jeu de contraintes personnalisé -----------------
	// On n'attache volontairement PAS de ConstraintSet : le défaut s'applique,
	// ce qui est exactement le scénario « projet importé sans règles ».

	if err := s.projects.Create(ctx, p); err != nil {
		if errors.Is(err, apperrors.ErrDuplicate) {
			return nil, fmt.Errorf("démo : %w", err)
		}
		return nil, fmt.Errorf("démo : persistance du projet : %w", err)
	}

	rep := &Report{
		ProjectID: p.ID(),
		Name:      p.Name(),
		Faults: []Fault{
			{
				Code: FaultThinTrack, Where: "net VCC (F.Cu)",
				Description: "Piste de 0.08 mm pour de l'alimentation : sous toute règle de largeur minimale.",
				Repair:      "POST /api/v1/projects/{id}/drc/autofix → widen_track",
			},
			{
				Code: FaultEdgeHug, Where: "net SCL (F.Cu)",
				Description: "Piste à 0.2 mm du bord de carte : violation de la distance cuivre/bord.",
				Repair:      "POST .../drc/autofix → nudge_edge",
			},
			{
				Code: FaultTinyVia, Where: "via (20, 27) net GND",
				Description: "Perçage de 0.12 mm et anneau annulaire quasi nul : forage laser obligatoire.",
				Repair:      "POST .../drc/autofix → enlarge_via",
			},
			{
				Code: FaultUnrouted, Where: "nets N1, N2",
				Description: "Deux nets totalement non routés : l'analyse SI et la fabrication en souffrent.",
				Repair:      "POST .../route (strategy=rl si modèle entraîné, sinon astar)",
			},
			{
				Code: FaultNoConstraint, Where: "projet",
				Description: "Aucune règle personnalisée : les valeurs par défaut s'appliquent, classes de nets ignorées.",
				Repair:      "PUT .../netclasses/power/rules puis GET .../doctor",
			},
		},
		Board: BoardSummary{
			WidthMM: board.WidthMM, HeightMM: board.HeightMM,
			LayerCount: board.LayerCount,
			Components: len(board.Components),
			Tracks:     len(board.Tracks),
			Vias:       len(board.Vias),
		},
		NextSteps: []string{
			"GET  /api/v1/projects/" + p.ID() + "/doctor          → score catastrophique garanti",
			"POST /api/v1/projects/" + p.ID() + "/drc/autofix    → réparation en un clic",
			"POST /api/v1/projects/" + p.ID() + "/route          → compléter le routage (RL/A*)",
			"GET  /api/v1/projects/" + p.ID() + "/doctor          → le score a grimpé",
			"POST /api/v1/projects/" + p.ID() + "/pours          → plans de masse GND",
		},
	}

	s.log.Info("carte cauchemar générée", "project_id", p.ID(),
		"faults", len(rep.Faults))
	return rep, nil
}

// constraintSetDefaults is referenced for documentation symmetry: the
// nightmare project deliberately relies on the platform defaults.
var _ = domainconstraints.NewDefault
