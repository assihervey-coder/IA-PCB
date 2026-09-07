package rest

import (
	"net/http"

	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
)

// Handlers REST des classes de nets (autoroutage interactif) — additif,
// hors contrat figé contracts.md §2 :
//
//	GET /api/v1/projects/{id}/netclasses              → vue d'ensemble
//	PUT /api/v1/projects/{id}/netclasses/{net}        → classement d'un net
//	PUT /api/v1/projects/{id}/netclasses/{class}/rules → règles par classe

// handleNetClasses answers GET /api/v1/projects/{id}/netclasses.
func (d *Deps) handleNetClasses(w http.ResponseWriter, r *http.Request) {
	if d.NetClasses == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "classes de nets indisponibles")
		return
	}
	ov, err := d.NetClasses.Overview(r.Context(), r.PathValue("id"))
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// handleAssignNetClass answers PUT /api/v1/projects/{id}/netclasses/{net}.
func (d *Deps) handleAssignNetClass(w http.ResponseWriter, r *http.Request) {
	if d.NetClasses == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "classes de nets indisponibles")
		return
	}
	var req layoutapp.ClassAssignRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ov, err := d.NetClasses.Assign(r.Context(), r.PathValue("id"), r.PathValue("net"), req.NetClass)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// handleSetClassRules answers PUT /api/v1/projects/{id}/netclasses/{class}/rules.
func (d *Deps) handleSetClassRules(w http.ResponseWriter, r *http.Request) {
	if d.NetClasses == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "classes de nets indisponibles")
		return
	}
	var req layoutapp.ClassRulesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rules, err := d.NetClasses.SetRules(r.Context(), r.PathValue("id"), r.PathValue("class"), req)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": len(rules), "rules": rules})
}
