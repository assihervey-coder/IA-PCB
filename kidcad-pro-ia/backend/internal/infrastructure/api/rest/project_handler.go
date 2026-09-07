package rest

import (
	"errors"
	"net/http"
	"strconv"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// project limit bounds for the list endpoint.
const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// handleListProjects answers GET /api/v1/projects?limit=&offset=.
func (d *Deps) handleListProjects(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", defaultListLimit)
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	offset := queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	projects, total, err := d.Projects.List(r.Context(), limit, offset)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	list := ProjectList{Projects: []Project{}, Total: total}
	for _, p := range projects {
		list.Projects = append(list.Projects, projectToDTO(p))
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateProject answers POST /api/v1/projects (201).
func (d *Deps) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body ProjectCreate
	if !decodeJSON(w, r, &body) {
		return
	}
	name := body.Name
	if name == "" {
		writeError(w, r, http.StatusBadRequest, "invalid", "le nom du projet est obligatoire")
		return
	}
	layerCount := body.LayerCount
	if layerCount <= 0 {
		layerCount = 2
	}

	p, err := domainproject.New(name, body.Description, layerCount)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if err := d.Projects.Create(r.Context(), p); err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectToDTO(p))
}

// handleGetProject answers GET /api/v1/projects/{id}.
func (d *Deps) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := d.Projects.FindByID(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectToDTO(p))
}

// handleUpdateProject answers PUT /api/v1/projects/{id} with
// {name?, description?}.
func (d *Deps) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, err := d.Projects.FindByID(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	var body ProjectUpdate
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name != nil {
		if err := p.Rename(*body.Name); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}
	if body.Description != nil {
		p.SetDescription(*body.Description)
	}
	if err := d.Projects.Update(r.Context(), p); err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectToDTO(p))
}

// handleDeleteProject answers DELETE /api/v1/projects/{id} (204).
func (d *Deps) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.Projects.FindByID(r.Context(), id); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			// La suppression est idempotente : 204 même si absent.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mapServiceError(w, r, err)
		return
	}
	if err := d.Projects.Delete(r.Context(), id); err != nil {
		mapServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queryInt reads an integer query parameter with a fallback default.
func queryInt(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
