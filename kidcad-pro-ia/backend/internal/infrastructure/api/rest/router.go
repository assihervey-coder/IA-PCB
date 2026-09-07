package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	exportapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/export"
	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
	schematicapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/schematic"
	verificationapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/verification"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	kidcadws "github.com/kidcad/kidcad-pro-ia/backend/internal/infrastructure/api/websocket"
)

// healthProbeTimeout bounds the AI engine probe of /healthz.
const healthProbeTimeout = time.Second

// Deps carries every service the REST layer needs (contracts.md §5). The
// AI and Database fields are additive: /healthz probes the engine and
// reports the persistence mode without reaching into the services.
type Deps struct {
	Projects   domainproject.Repository
	Import     *schematicapp.ImportService
	Validation *schematicapp.ValidationService
	Place      *layoutapp.PlaceService
	Route      *layoutapp.RouteService
	Optimize   *layoutapp.OptimizeService
	DRC        *verificationapp.DRCChecker
	ERC        *verificationapp.ERCChecker
	Gerber     *exportapp.GerberService
	BOM        *exportapp.BOMService
	STEP       *exportapp.STEPService
	Hub        *kidcadws.Hub
	Logger     *slog.Logger
	Version    string

	// Champs additionnels (hors contrat figé) pour /healthz.
	AI       layoutapp.AIService // sonde du moteur IA (peut être nil)
	Database string              // "memory" | "postgres"
}

// NewRouter builds the HTTP handler: Go 1.22 method patterns, recovery,
// request logging (debug) and permissive CORS.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Database == "" {
		d.Database = "memory"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", d.handleHealth)

	mux.HandleFunc("GET /api/v1/projects", d.handleListProjects)
	mux.HandleFunc("POST /api/v1/projects", d.handleCreateProject)
	mux.HandleFunc("GET /api/v1/projects/{id}", d.handleGetProject)
	mux.HandleFunc("PUT /api/v1/projects/{id}", d.handleUpdateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", d.handleDeleteProject)

	mux.HandleFunc("POST /api/v1/projects/{id}/import", d.handleImport)
	mux.HandleFunc("GET /api/v1/projects/{id}/layout", d.handleGetLayout)
	mux.HandleFunc("PUT /api/v1/projects/{id}/layout", d.handlePutLayout)
	mux.HandleFunc("POST /api/v1/projects/{id}/place", d.handlePlace)
	mux.HandleFunc("POST /api/v1/projects/{id}/route", d.handleRoute)
	mux.HandleFunc("POST /api/v1/projects/{id}/optimize", d.handleOptimize)
	mux.HandleFunc("GET /api/v1/projects/{id}/jobs/{jobID}", d.handleGetJob)
	mux.HandleFunc("POST /api/v1/projects/{id}/drc", d.handleDRC)
	mux.HandleFunc("POST /api/v1/projects/{id}/erc", d.handleERC)

	mux.HandleFunc("GET /api/v1/projects/{id}/export/gerber", d.handleExportGerber)
	mux.HandleFunc("GET /api/v1/projects/{id}/export/bom", d.handleExportBOM)
	mux.HandleFunc("GET /api/v1/projects/{id}/export/step", d.handleExportSTEP)

	if d.Hub != nil {
		mux.HandleFunc("GET /ws/v1/progress", d.Hub.ServeWS)
	}

	return d.middleware(mux)
}

// middleware wraps the mux with panic recovery, request logging and CORS
// handling (Access-Control-Allow-Origin + OPTIONS 204).
func (d *Deps) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if p := recover(); p != nil {
				d.Logger.Error("panic http",
					"panic", fmt.Sprint(p),
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path)
				writeError(rec, r, http.StatusInternalServerError, "internal", "erreur interne du serveur")
				return
			}
		}()

		// CORS : origine permissive (le durcissement par environnement est
		// géré par le reverse proxy en production).
		rec.Header().Set("Access-Control-Allow-Origin", "*")
		rec.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		rec.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			rec.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(rec, r)

		d.Logger.Log(r.Context(), slog.LevelDebug, "requete http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder captures the response status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status then delegates.
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer
// (required for the WebSocket upgrade inside the hub).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// --------------------------------------------------------------
// Helpers communs
// --------------------------------------------------------------

// writeJSON serialises payload with the given status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload != nil {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(payload)
	}
}

// writeError emits the uniform error payload.
func writeError(w http.ResponseWriter, _ *http.Request, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: errorBody{Code: code, Message: message}})
}

// decodeJSON parses the request body into dst, returning false when the
// payload is invalid (the error response is already written).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid", "corps JSON invalide : "+err.Error())
		return false
	}
	return true
}

// mapServiceError converts use-case errors onto HTTP responses and reports
// whether a response was written.
func mapServiceError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, apperrors.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, apperrors.ErrDuplicate):
		writeError(w, r, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, layoutapp.ErrAIUnreachable):
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "internal", err.Error())
	}
	return true
}

// handleHealth answers GET /healthz per contracts.md §2.
func (d *Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	aiState := "unreachable"
	if d.AI != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthProbeTimeout)
		err := d.AI.Health(ctx)
		cancel()
		if err == nil {
			aiState = "ok"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"version":   d.Version,
		"ai_engine": aiState,
		"database":  d.Database,
	})
}
