package rest

import (
	"net/http"
)

// Handler REST de la démo « carte cauchemar » — additif, hors contrat figé
// contracts.md §2 :
//
//	POST /api/v1/demo/nightmare → crée un projet volontairement mauvais
//
// La carte générée déclenche des violations DRC réelles (largeurs, vias,
// marge de bord), laisse des nets non routés et ne définit aucune règle :
// le Design Doctor affiche un score catastrophique et l'Auto-Healer DRC
// répare tout en un appel, ce qui en fait le tutoriel vivant de la plateforme.

// DemoNightmareReport is the response of POST /api/v1/demo/nightmare.
type DemoNightmareReport struct {
	ProjectID string              `json:"project_id"`
	Name      string              `json:"name"`
	Faults    []DemoFaultDTO      `json:"faults"`
	Board     DemoBoardSummaryDTO `json:"board"`
	NextSteps []string            `json:"next_steps"`
}

// DemoFaultDTO is one seeded fault.
type DemoFaultDTO struct {
	Code        string `json:"code"`
	Where       string `json:"where"`
	Description string `json:"description"`
	Repair      string `json:"repair"`
}

// DemoBoardSummaryDTO summarises the generated board.
type DemoBoardSummaryDTO struct {
	WidthMM    float64 `json:"width_mm"`
	HeightMM   float64 `json:"height_mm"`
	LayerCount int     `json:"layer_count"`
	Components int     `json:"components"`
	Tracks     int     `json:"tracks"`
	Vias       int     `json:"vias"`
}

// handleDemoNightmare answers POST /api/v1/demo/nightmare.
func (d *Deps) handleDemoNightmare(w http.ResponseWriter, r *http.Request) {
	if d.Demo == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "démo indisponible")
		return
	}
	rep, err := d.Demo.Nightmare(r.Context())
	if mapServiceError(w, r, err) {
		return
	}

	out := DemoNightmareReport{
		ProjectID: rep.ProjectID,
		Name:      rep.Name,
		Faults:    make([]DemoFaultDTO, 0, len(rep.Faults)),
		NextSteps: rep.NextSteps,
		Board: DemoBoardSummaryDTO{
			WidthMM:    rep.Board.WidthMM,
			HeightMM:   rep.Board.HeightMM,
			LayerCount: rep.Board.LayerCount,
			Components: rep.Board.Components,
			Tracks:     rep.Board.Tracks,
			Vias:       rep.Board.Vias,
		},
	}
	for _, f := range rep.Faults {
		out.Faults = append(out.Faults, DemoFaultDTO{
			Code: f.Code, Where: f.Where,
			Description: f.Description, Repair: f.Repair,
		})
	}
	writeJSON(w, http.StatusCreated, out)
}
