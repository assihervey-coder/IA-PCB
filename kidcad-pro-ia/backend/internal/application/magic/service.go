// service.go binds the pure parser to the project aggregate: it loads the
// project, interprets the utterance and (optionally) executes the safe,
// reversible actions directly on the board and constraint set. Heavier
// operations (routing, optimisation, exports, DRC/ERC) are returned as
// structured actions so the caller triggers the matching use-case endpoint
// — the copilot orchestrates, it does not duplicate the use cases.
package magic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// ApplyReport summarises what Apply actually changed on the aggregate.
type ApplyReport struct {
	Interpretation *Interpretation `json:"interpretation"`
	Applied        []string        `json:"applied"`
	Skipped        []string        `json:"skipped"`
	BoardChanged   bool            `json:"board_changed"`
	RulesChanged   bool            `json:"rules_changed"`
}

// MagicService is the copilot use case.
type MagicService struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewMagicService builds the copilot.
func NewMagicService(projects domainproject.Repository, log *slog.Logger) *MagicService {
	if log == nil {
		log = slog.Default()
	}
	return &MagicService{projects: projects, log: log}
}

// defaultFootprint is used for components placed by kind ("deux
// condensateurs") that are neither on the board nor in the schematic.
func defaultFootprint(kind string) domainlayout.Footprint {
	w, h := 1.6, 0.8 // 0603-ish
	switch kind {
	case "connector":
		w, h = 8.0, 4.0
	case "led":
		w, h = 1.6, 0.8
	case "inductor":
		w, h = 3.2, 1.6
	}
	return domainlayout.Footprint{Name: "MAGIC-" + strings.ToUpper(kind), Value: kind,
		BodyWidthMM: w, BodyHeightMM: h, HeightMM: domainlayout.DefaultBoardHeightMM}
}

// interpret loads the project then runs the pure parser. A confidence of 0
// still returns 200: the client decides what to do with the suggestion.
func (s *MagicService) Interpret(ctx context.Context, projectID, utterance string) (*ApplyReport, error) {
	if _, err := s.load(ctx, projectID); err != nil {
		return nil, err
	}
	rep := &ApplyReport{Interpretation: Parse(utterance)}
	return rep, nil
}

// Apply interprets then executes the executable actions against the
// aggregate, persisting the project when something changed.
func (s *MagicService) Apply(ctx context.Context, projectID, utterance string) (*ApplyReport, error) {
	p, err := s.load(ctx, projectID)
	if err != nil {
		return nil, err
	}

	interp := Parse(utterance)
	rep := &ApplyReport{Interpretation: interp}
	if len(interp.Actions) == 0 {
		return rep, nil
	}

	board := p.Board()
	cs := p.Constraints()
	boardChanged, rulesChanged := false, false

	for _, a := range interp.Actions {
		if !a.Executable {
			rep.Skipped = append(rep.Skipped,
				a.Summary+" (action déléguée au module correspondant)")
			continue
		}
		switch a.Kind {
		case KindMoveComponent:
			if board == nil {
				rep.Skipped = append(rep.Skipped, "Aucune carte physique : importez ou générez d'abord un placement.")
				continue
			}
			if msg, ok := s.applyMove(board, a); ok {
				rep.Applied = append(rep.Applied, msg)
				boardChanged = true
			} else {
				rep.Skipped = append(rep.Skipped, msg)
			}

		case KindPlaceComponent:
			if board == nil {
				rep.Skipped = append(rep.Skipped, "Aucune carte physique : importez ou générez d'abord un placement.")
				continue
			}
			msgs, ok := s.applyPlaceKind(board, a)
			rep.Applied = append(rep.Applied, msgs...)
			if ok {
				boardChanged = true
			}

		case KindDeleteComponent:
			if board == nil {
				rep.Skipped = append(rep.Skipped, "Aucune carte physique à modifier.")
				continue
			}
			ref, _ := a.Params["ref"].(string)
			if board.RemoveComponent(ref) {
				board.RemoveRoutesForNet(ref) // tracks are net-named; safe cleanup attempt
				rep.Applied = append(rep.Applied, "Composant "+ref+" supprimé de la carte.")
				boardChanged = true
			} else {
				rep.Skipped = append(rep.Skipped, "Composant "+ref+" introuvable sur la carte.")
			}

		case KindSetTrackWidth:
			mm, _ := a.Params["width_mm"].(float64)
			target, _ := a.Params["target"].(string)
			if msg, ok := applyWidth(cs, board, target, mm); ok {
				rep.Applied = append(rep.Applied, msg)
				rulesChanged = true
				if strings.Contains(msg, "re-largeur") {
					boardChanged = true
				}
			} else {
				rep.Skipped = append(rep.Skipped, msg)
			}

		case KindAddNetClass:
			name, _ := a.Params["name"].(string)
			mm, _ := a.Params["width_mm"].(float64)
			if cs == nil {
				cs = domainconstraints.NewDefault()
				p.SetConstraints(cs)
			}
			if err := cs.Add(domainconstraints.Rule{
				ID:       "class-" + name + "-width",
				Name:     "Largeur minimale (classe " + name + ")",
				Type:     domainconstraints.RuleMinTrackWidth,
				Scope:    domainconstraints.Scope{NetClass: name, Layer: domainconstraints.AnyLayer},
				ValueMM:  math.Max(0.1, mm),
				Severity: domainconstraints.SeverityError,
				Enabled:  true,
			}); err != nil {
				rep.Skipped = append(rep.Skipped, "Classe "+name+" : "+err.Error())
			} else {
				rep.Applied = append(rep.Applied, fmt.Sprintf("Classe de nets %q ajoutée (largeur %.3f mm).", name, math.Max(0.1, mm)))
				rulesChanged = true
			}
		}
	}

	if boardChanged || rulesChanged {
		if boardChanged && board != nil {
			p.SetBoard(board)
		}
		if err := s.projects.Update(ctx, p); err != nil {
			return nil, fmt.Errorf("copilot : persistance : %w", err)
		}
	}
	rep.BoardChanged, rep.RulesChanged = boardChanged, rulesChanged
	s.log.Info("copilot magic appliqué", "project_id", projectID,
		"applied", len(rep.Applied), "skipped", len(rep.Skipped))
	return rep, nil
}

// load fetches the project mapping not-found onto the domain sentinel.
func (s *MagicService) load(ctx context.Context, projectID string) (*domainproject.Project, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("copilot : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("copilot : chargement du projet : %w", err)
	}
	return p, nil
}

// applyMove repositions one component, honouring "near <ref>" or explicit
// coordinates. Returns (message, success).
func (s *MagicService) applyMove(board *domainlayout.Board, a Action) (string, bool) {
	ref, _ := a.Params["ref"].(string)
	if ref == "" || ref == "*" {
		return "Déplacement global non exécutable ici : utilisez le placement automatique.", false
	}
	comp, ok := board.ComponentByRef(ref)
	if !ok {
		return "Composant " + ref + " introuvable sur la carte.", false
	}
	if near, has := a.Params["near"].(string); has && near != "" {
		anchor, ok := board.ComponentByRef(near)
		if !ok {
			return "Ancre " + near + " introuvable pour le placement près de.", false
		}
		comp.X, comp.Y = offsetAround(anchor.X, anchor.Y, board)
	} else if x, okX := a.Params["x"].(float64); okX {
		y, _ := a.Params["y"].(float64)
		comp.X, comp.Y = clampToBoard(x, board.WidthMM), clampToBoard(y, board.HeightMM)
	}
	return fmt.Sprintf("%s déplacé en (%.2f, %.2f).", ref, comp.X, comp.Y), true
}

// applyPlaceKind materialises "deux condensateurs près de U1": new placed
// components named after the first free reference prefix of the kind.
func (s *MagicService) applyPlaceKind(board *domainlayout.Board, a Action) ([]string, bool) {
	kind, _ := a.Params["component_kind"].(string)
	count, _ := a.Params["count"].(float64)
	if count < 1 {
		count = 1
	}
	prefix := map[string]string{"capacitor": "C", "resistor": "R", "led": "D",
		"inductor": "L", "connector": "J"}[kind]
	if prefix == "" {
		prefix = "U"
	}
	near, hasNear := a.Params["near"].(string)
	var cx, cy float64
	if hasNear {
		if anchor, ok := board.ComponentByRef(near); ok {
			cx, cy = anchor.X, anchor.Y
		}
	} else if x, ok := a.Params["x"].(float64); ok {
		cx, cy = x, a.Params["y"].(float64)
	} else {
		cx, cy = board.WidthMM/2, board.HeightMM/2
	}

	var msgs []string
	planted := 0
	for i := 0; i < int(count); i++ {
		ref := freeRef(board, prefix)
		fp := defaultFootprint(kind)
		if err := board.AddComponent(domainlayout.PlacedComponent{
			Ref: ref, Footprint: fp,
			X: clampToBoard(cx+float64(i%4)*2.2-3.3, board.WidthMM),
			Y: clampToBoard(cy+float64(i/4)*2.2, board.HeightMM),
		}); err != nil {
			break
		}
		msgs = append(msgs, fmt.Sprintf("%s (%s) placé sur la carte.", ref, kind))
		planted++
	}
	if planted == 0 {
		return msgs, false
	}
	return msgs, true
}

// offsetAround returns a free-ish spot 4 mm to the right of the anchor,
// wrapping back inside the board outline.
func offsetAround(ax, ay float64, board *domainlayout.Board) (float64, float64) {
	return clampToBoard(ax+4.0, board.WidthMM), clampToBoard(ay, board.HeightMM)
}

// clampToBoard keeps a coordinate inside [0, dim].
func clampToBoard(v, dim float64) float64 {
	return math.Min(math.Max(0, v), math.Max(0, dim))
}

// freeRef finds the first free reference like C1, C2… for the prefix.
func freeRef(board *domainlayout.Board, prefix string) string {
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s%d", prefix, i)
		if _, taken := board.ComponentByRef(cand); !taken {
			return cand
		}
	}
	return prefix + "X"
}

// applyWidth updates the matching min-track-width rule and (for explicit
// nets) rewrites the width of existing tracks. Returns (message, success).
func applyWidth(cs *domainconstraints.ConstraintSet, board *domainlayout.Board, target string, mm float64) (string, bool) {
	if mm <= 0 || mm >= 100 {
		return "Largeur hors bornes attendue (0 < w < 100 mm).", false
	}
	if cs == nil {
		return "Aucun jeu de contraintes initialisé.", false
	}
	cs.SetMinTrackWidth(mm, target)
	msg := fmt.Sprintf("Règle de largeur mise à jour : %.3f mm (%s).", mm, target)
	if board != nil {
		rewritten := 0
		for i := range board.Tracks {
			t := &board.Tracks[i]
			if target == "all" || strings.EqualFold(t.Net, target) {
				t.Width = mm
				rewritten++
			}
		}
		if rewritten > 0 {
			msg += fmt.Sprintf(" %d piste(s) existante(s) re-largeur(s).", rewritten)
		}
	}
	return msg, true
}
