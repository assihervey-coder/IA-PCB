package rest

import (
	"net/http"

	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
)

// Handlers REST des plans de masse (copper pours) — additif, hors contrat
// figé contracts.md §2 :
//
//	POST   /api/v1/projects/{id}/pours            → génération / re-fill
//	GET    /api/v1/projects/{id}/pours            → liste
//	DELETE /api/v1/projects/{id}/pours?net=GND    → suppression par net
//	DELETE /api/v1/projects/{id}/pours/{pourID}   → suppression unitaire

// handleGeneratePours answers POST /api/v1/projects/{id}/pours.
func (d *Deps) handleGeneratePours(w http.ResponseWriter, r *http.Request) {
	if d.Pours == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "plans de masse indisponibles")
		return
	}
	projectID := r.PathValue("id")

	req := layoutapp.PourGenerateRequest{}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	report, err := d.Pours.Generate(r.Context(), projectID, req)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleListPours answers GET /api/v1/projects/{id}/pours.
func (d *Deps) handleListPours(w http.ResponseWriter, r *http.Request) {
	if d.Pours == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "plans de masse indisponibles")
		return
	}
	pours, err := d.Pours.List(r.Context(), r.PathValue("id"))
	if mapServiceError(w, r, err) {
		return
	}
	out := make([]LayoutPour, 0, len(pours))
	for i := range pours {
		p := &pours[i]
		lp := LayoutPour{
			ID: p.ID, Net: p.Net, Name: p.Name, Layer: p.Layer,
			ClearanceMM: p.ClearanceMM, HatchMM: p.HatchMM,
			IsGround: p.IsGround, FillPct: p.FillPct,
			AreaMM2: p.AreaMM2, Stitched: p.Stitched,
			Outline: []LayoutPourPoint{},
		}
		for _, pt := range p.Outline {
			lp.Outline = append(lp.Outline, LayoutPourPoint{X: pt.X, Y: pt.Y})
		}
		out = append(out, lp)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeletePour answers DELETE /api/v1/projects/{id}/pours/{pourID}.
func (d *Deps) handleDeletePour(w http.ResponseWriter, r *http.Request) {
	if d.Pours == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "plans de masse indisponibles")
		return
	}
	if err := d.Pours.Delete(r.Context(), r.PathValue("id"), r.PathValue("pourID")); mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleDeletePoursForNet answers DELETE /api/v1/projects/{id}/pours.
// Optional ?net=GND filters by net; without the parameter every pour is
// removed.
func (d *Deps) handleDeletePoursForNet(w http.ResponseWriter, r *http.Request) {
	if d.Pours == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "plans de masse indisponibles")
		return
	}
	removed, err := d.Pours.DeleteForNet(r.Context(), r.PathValue("id"), r.URL.Query().Get("net"))
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}
