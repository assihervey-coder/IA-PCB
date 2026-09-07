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

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	arenaapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/arena"
	collabapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/collab"
	demoapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/demo"
	dfmapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/dfm"
	doctorapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/doctor"
	exportapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/export"
	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	magicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/magic"
	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	statsapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/stats"
	timemachineapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/timemachine"
	verificationapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/verification"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	yahriacadws "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/api/websocket"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/auth"
)

// healthProbeTimeout bounds the AI engine probe of /healthz.
const healthProbeTimeout = time.Second

// Deps carries every service the REST layer needs (contracts.md §5). The
// AI and Database fields are additive: /healthz probes the engine and
// reports the persistence mode without reaching into the services.
type Deps struct {
	Projects    domainproject.Repository
	Import      *schematicapp.ImportService
	Validation  *schematicapp.ValidationService
	Place       *layoutapp.PlaceService
	Route       *layoutapp.RouteService
	Optimize    *layoutapp.OptimizeService
	DRC         *verificationapp.DRCChecker
	ERC         *verificationapp.ERCChecker
	Thermal     *verificationapp.ThermalChecker
	SI          *verificationapp.SIChecker
	Magic       *magicapp.MagicService
	Arena       *arenaapp.ArenaService
	AutoFix     *verificationapp.AutoFixService
	Doctor      *doctorapp.DoctorService
	DFM         *dfmapp.Service
	TimeMachine *timemachineapp.Service
	Stats       *statsapp.Service
	Pours       *layoutapp.PourService
	NetClasses  *layoutapp.NetClassService
	Collab      *collabapp.Service
	Demo        *demoapp.Service
	Impedance   *verificationapp.ImpedanceService
	Gerber      *exportapp.GerberService
	BOM         *exportapp.BOMService
	STEP        *exportapp.STEPService
	ODB         *exportapp.ODBService
	Hub         *yahriacadws.Hub
	Logger      *slog.Logger
	Version     string

	// Champs additionnels (hors contrat figé) pour /healthz.
	AI       layoutapp.AIService // sonde du moteur IA (peut être nil)
	Database string              // "memory" | "postgres"

	// Durcissement production (additif, valeur zéro = comportement dev) :
	// Auth non nil => JWT exigé sur les routes protégées ; AllowedOrigins
	// vide => CORS permissif "*" ; AIRateRPS <= 0 => pas de limite IA.
	Auth           *auth.Service
	AllowedOrigins []string
	AIRateRPS      float64
	AIRateBurst    int

	// AIRate est construit par NewRouter quand AIRateRPS > 0.
	AIRate *ipRateLimiter
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
	if d.AIRateRPS > 0 {
		burst := d.AIRateBurst
		if burst <= 0 {
			burst = int(d.AIRateRPS) * 5
		}
		d.AIRate = newIPRateLimiter(d.AIRateRPS, burst)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", d.handleHealth)
	mux.HandleFunc("GET /metrics", d.handleMetrics)

	// Authentification JWT (additif, §13) : login public, /me protégé
	// par le middleware d'authentification global.
	mux.HandleFunc("POST /api/v1/auth/login", d.handleLogin)
	mux.HandleFunc("GET /api/v1/auth/me", d.handleMe)

	// Démo « carte cauchemar » (tutoriel vivant Auto-Healer + Doctor).
	mux.HandleFunc("POST /api/v1/demo/nightmare", d.aiLimited(d.handleDemoNightmare))

	// Modèle RL (PyTorch) : inspection + rechargement à chaud (additif).
	mux.HandleFunc("GET /api/v1/ai/model", d.handleAIModelInfo)
	mux.HandleFunc("POST /api/v1/ai/model/reload", d.aiLimited(d.handleAIModelReload))

	mux.HandleFunc("GET /api/v1/projects", d.handleListProjects)
	mux.HandleFunc("POST /api/v1/projects", d.handleCreateProject)
	mux.HandleFunc("GET /api/v1/projects/{id}", d.handleGetProject)
	mux.HandleFunc("PUT /api/v1/projects/{id}", d.handleUpdateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", d.handleDeleteProject)

	mux.HandleFunc("POST /api/v1/projects/{id}/import", d.handleImport)
	mux.HandleFunc("GET /api/v1/projects/{id}/layout", d.handleGetLayout)
	mux.HandleFunc("PUT /api/v1/projects/{id}/layout", d.handlePutLayout)
	mux.HandleFunc("POST /api/v1/projects/{id}/place", d.aiLimited(d.handlePlace))
	mux.HandleFunc("POST /api/v1/projects/{id}/route", d.aiLimited(d.handleRoute))
	mux.HandleFunc("POST /api/v1/projects/{id}/optimize", d.aiLimited(d.handleOptimize))
	mux.HandleFunc("GET /api/v1/projects/{id}/jobs/{jobID}", d.handleGetJob)
	mux.HandleFunc("POST /api/v1/projects/{id}/drc", d.handleDRC)
	mux.HandleFunc("POST /api/v1/projects/{id}/erc", d.handleERC)

	// Plans de masse (copper pours) — additif, hors contrat figé.
	mux.HandleFunc("POST /api/v1/projects/{id}/pours", d.handleGeneratePours)
	mux.HandleFunc("GET /api/v1/projects/{id}/pours", d.handleListPours)
	mux.HandleFunc("DELETE /api/v1/projects/{id}/pours", d.handleDeletePoursForNet)
	mux.HandleFunc("DELETE /api/v1/projects/{id}/pours/{pourID}", d.handleDeletePour)

	// Classes de nets (autoroutage interactif) — additif, hors contrat figé.
	mux.HandleFunc("GET /api/v1/projects/{id}/netclasses", d.handleNetClasses)
	mux.HandleFunc("PUT /api/v1/projects/{id}/netclasses/{net}", d.handleAssignNetClass)
	mux.HandleFunc("PUT /api/v1/projects/{id}/netclasses/{class}/rules", d.handleSetClassRules)

	// Collaboration CRDT (multi-utilisateurs) - additif, hors contrat fige.
	mux.HandleFunc("POST /api/v1/projects/{id}/collab/ops", d.handleCollabOps)
	mux.HandleFunc("GET /api/v1/projects/{id}/collab/state", d.handleCollabState)
	mux.HandleFunc("POST /api/v1/projects/{id}/collab/undo", d.handleCollabUndo)
	mux.HandleFunc("POST /api/v1/projects/{id}/collab/redo", d.handleCollabRedo)

	// Magic Pack (additif, hors contrat figé).
	mux.HandleFunc("POST /api/v1/projects/{id}/magic", d.aiLimited(d.handleMagic))
	mux.HandleFunc("POST /api/v1/projects/{id}/thermal", d.handleThermal)
	mux.HandleFunc("POST /api/v1/projects/{id}/si", d.handleSI)
	mux.HandleFunc("POST /api/v1/projects/{id}/impedance", d.handleImpedance)
	mux.HandleFunc("POST /api/v1/projects/{id}/arena", d.aiLimited(d.handleArena))
	mux.HandleFunc("POST /api/v1/projects/{id}/arena/benchmark", d.aiLimited(d.handleArenaBenchmark))
	mux.HandleFunc("GET /api/v1/arena/leaderboard", d.handleArenaLeaderboard)

	// Pack WOW (additif, hors contrat figé).
	mux.HandleFunc("POST /api/v1/projects/{id}/drc/autofix", d.aiLimited(d.handleAutoFix))
	mux.HandleFunc("GET /api/v1/projects/{id}/doctor", d.aiLimited(d.handleDoctor))
	mux.HandleFunc("POST /api/v1/projects/{id}/dfm", d.handleDFM)
	mux.HandleFunc("POST /api/v1/projects/{id}/snapshots", d.handleSnapshotCapture)
	mux.HandleFunc("GET /api/v1/projects/{id}/snapshots", d.handleSnapshotList)
	mux.HandleFunc("GET /api/v1/projects/{id}/snapshots/{sid}/diff", d.handleSnapshotDiff)
	mux.HandleFunc("POST /api/v1/projects/{id}/snapshots/{sid}/restore", d.handleSnapshotRestore)
	mux.HandleFunc("GET /api/v1/projects/{id}/stats", d.handleStats)

	mux.HandleFunc("GET /api/v1/projects/{id}/export/gerber", d.handleExportGerber)
	mux.HandleFunc("GET /api/v1/projects/{id}/export/bom", d.handleExportBOM)
	mux.HandleFunc("GET /api/v1/projects/{id}/export/step", d.handleExportSTEP)
	mux.HandleFunc("GET /api/v1/projects/{id}/export/odbpp", d.handleExportODBPP)

	if d.Hub != nil {
		mux.HandleFunc("GET /ws/v1/progress", d.Hub.ServeWS)
	}

	// Chaîne production : CORS strict → recovery + logging + métriques →
	// authentification JWT (quand configurée). L'auth est la couche la plus
	// externe après CORS : les préflights OPTIONS passent sans jeton.
	handler := d.middleware(mux)
	if d.Auth != nil {
		handler = d.Auth.Middleware(handler)
	}
	return handler
}

// middleware wraps the mux with panic recovery, request logging, Prometheus
// instrumentation and strict CORS: Access-Control-Allow-Origin echoes the
// request Origin only when it appears in AllowedOrigins (or when the list is
// the permissive default "*"), so cross-origin access follows the
// environment configuration instead of staying wide open.
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

		// CORS strict : l'origine n'est reflétée que si elle est autorisée.
		// Sans en-tête Origin (clients non navigateurs), aucun en-tête CORS
		// n'est émis — les appels serveur-à-serveur restent possibles.
		if origin := r.Header.Get("Origin"); origin != "" {
			rec.Header().Add("Vary", "Origin")
			if d.originAllowed(origin) {
				if len(d.AllowedOrigins) == 1 && d.AllowedOrigins[0] == "*" {
					rec.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					rec.Header().Set("Access-Control-Allow-Origin", origin)
				}
				rec.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				rec.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			} else if r.Method == http.MethodOptions {
				d.Logger.Warn("cors : origine refusée", "origin", origin, "path", r.URL.Path)
			}
		}

		if r.Method == http.MethodOptions {
			rec.WriteHeader(http.StatusNoContent)
			d.instrument(rec, r, time.Since(start).Seconds())
			return
		}

		next.ServeHTTP(rec, r)
		d.instrument(rec, r, time.Since(start).Seconds())

		d.Logger.Log(r.Context(), slog.LevelDebug, "requete http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// originAllowed reports whether the request origin may receive CORS headers.
func (d *Deps) originAllowed(origin string) bool {
	if len(d.AllowedOrigins) == 0 {
		return true // défaut historique "*" (dev) : permissif
	}
	for _, o := range d.AllowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
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
	case errors.Is(err, arenaapp.ErrModelNotLoaded):
		// Benchmark demandé sans checkpoint RL chargé : l'action est
		// comprise mais impossible en l'état (entraîner + recharger).
		writeError(w, r, http.StatusServiceUnavailable, "rl_model_not_loaded", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "internal", err.Error())
	}
	return true
}

// handleHealth answers GET /healthz per contracts.md §2. Additive fields
// (ai_device, ai_model_loaded, ai_version) surface the RL runtime state.
func (d *Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	aiState := "unreachable"
	resp := map[string]any{
		"status":    "ok",
		"version":   d.Version,
		"ai_engine": aiState,
		"database":  d.Database,
	}
	if d.AI != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthProbeTimeout)
		info, err := d.AI.EngineInfo(ctx)
		cancel()
		if err == nil {
			resp["ai_engine"] = "ok"
			resp["ai_device"] = info.Device
			resp["ai_model_loaded"] = info.ModelLoaded
			if info.Version != "" {
				resp["ai_version"] = info.Version
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
