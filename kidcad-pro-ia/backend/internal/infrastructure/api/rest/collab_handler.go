package rest

import (
	"net/http"
	"strconv"

	collabapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/collab"
)

// Handlers REST de la collaboration multi-utilisateurs (CRDT) — additif,
// hors contrat figé contracts.md §2 :
//
//	POST /api/v1/projects/{id}/collab/ops   → application d'un lot d'ops
//	GET  /api/v1/projects/{id}/collab/state → séquence + horloge + rattrapage
//	POST /api/v1/projects/{id}/collab/undo  → annulation (par acteur)
//	POST /api/v1/projects/{id}/collab/redo  → rétablissement (par acteur)
//
// Les ops appliquées sont aussi diffusées sur /ws/v1/progress (type
// "collab") pour la synchro temps réel des autres éditeurs.

// handleCollabOps answers POST /api/v1/projects/{id}/collab/ops.
func (d *Deps) handleCollabOps(w http.ResponseWriter, r *http.Request) {
	if d.Collab == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "collaboration indisponible")
		return
	}
	var req collabapp.ApplyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := d.Collab.Apply(r.Context(), r.PathValue("id"), req)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleCollabState answers GET /api/v1/projects/{id}/collab/state.
// Optional ?since=N returns only the log entries after sequence N.
func (d *Deps) handleCollabState(w http.ResponseWriter, r *http.Request) {
	if d.Collab == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "collaboration indisponible")
		return
	}
	since := int64(-1)
	if raw := r.URL.Query().Get("since"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			writeError(w, r, http.StatusBadRequest, "invalid", "paramètre since invalide")
			return
		}
		since = v
	}
	st, err := d.Collab.State(r.Context(), r.PathValue("id"), since)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleCollabUndo answers POST /api/v1/projects/{id}/collab/undo.
func (d *Deps) handleCollabUndo(w http.ResponseWriter, r *http.Request) {
	d.collabUnRe(w, r, true)
}

// handleCollabRedo answers POST /api/v1/projects/{id}/collab/redo.
func (d *Deps) handleCollabRedo(w http.ResponseWriter, r *http.Request) {
	d.collabUnRe(w, r, false)
}

func (d *Deps) collabUnRe(w http.ResponseWriter, r *http.Request, undo bool) {
	if d.Collab == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "collaboration indisponible")
		return
	}
	var req struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	projectID := r.PathValue("id")

	var res *collabapp.UndoResult
	var err error
	if undo {
		res, err = d.Collab.Undo(r.Context(), projectID, req.Actor)
	} else {
		res, err = d.Collab.Redo(r.Context(), projectID, req.Actor)
	}
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}
