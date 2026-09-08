// Integration tests of the production hardening in the REST router:
// JWT enforcement through the full middleware chain, strict CORS, per-IP
// rate limiting of the AI routes and Prometheus exposition.
package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/auth"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/config"
)

// newSecuredRouter builds a router with JWT auth on and configurable CORS /
// rate limit, backed by no services (only public + auth routes answer
// meaningfully, the rest is behind 401 or 500).
func newSecuredRouter(t *testing.T, origins []string, rps float64) http.Handler {
	t.Helper()
	cfg := config.Config{JWTSecret: "ci-secret", JWTTTL: time.Hour}
	svc := auth.NewService(cfg.JWTSecret, cfg.JWTTTL, []string{"admin:admin"}, nil)
	return NewRouter(Deps{
		Auth:           svc,
		AllowedOrigins: origins,
		AIRateRPS:      rps,
		AIRateBurst:    3,
		Database:       "memory",
		Version:        "test",
	})
}

func login(t *testing.T, h http.Handler) string {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login : %d (%s), attendu 200", res.Code, res.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("réponse login invalide : %s", res.Body.String())
	}
	return out.Token
}

func TestAuthEnforcedOnProtectedRoutes(t *testing.T) {
	h := newSecuredRouter(t, nil, 0)

	// Publics : healthz et metrics restent accessibles sans jeton.
	for _, path := range []string{"/healthz", "/metrics"} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Errorf("%s sans jeton : %d, attendu 200", path, res.Code)
		}
	}

	// Protégé : 401 sans jeton.
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("/api/v1/projects sans jeton : %d, attendu 401", res.Code)
	}

	// 200 avec jeton (identité validée par /auth/me).
	token := login(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/auth/me avec jeton : %d, attendu 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"username":"admin"`) {
		t.Fatalf("identité inattendue : %s", res.Body.String())
	}

	// WebSocket : 401 sans jeton, 404 (hub absent) avec jeton valide.
	res = httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/ws/v1/progress", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("ws sans jeton : %d, attendu 401", res.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/ws/v1/progress?token="+token, nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("ws avec jeton : %d, attendu 404 (auth passée, hub non enregistré)", res.Code)
	}
}

func TestStrictCORS(t *testing.T) {
	h := newSecuredRouter(t, []string{"http://localhost:3000"}, 0)

	// Origine autorisée : reflétée.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("ACAO = %q, attendu origine reflétée", got)
	}

	// Origine inconnue : aucun en-tête CORS (le navigateur bloque).
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q pour une origine interdite, attendu vide", got)
	}

	// Client sans Origin (serveur à serveur) : pas d'en-tête CORS mais 200.
	res = httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if res.Code != http.StatusOK || res.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("sans Origin : %d ACAO=%q", res.Code, res.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRateLimitOnAIRoutes(t *testing.T) {
	// Burst 3 : les 3 premières requêtes authentifiées passent la limite
	// (elles répondent 500 car les services sont absents), la suivante
	// reçoit 429.
	h := newSecuredRouter(t, nil, 0.0001)
	token := login(t, h)

	var lastCode, saw429, sawNon429 int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/projects/p/route", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		lastCode = res.Code
		if res.Code == http.StatusTooManyRequests {
			saw429++
		} else {
			sawNon429++
		}
	}
	if saw429 == 0 {
		t.Fatalf("aucun 429 après le burst (dernier code %d)", lastCode)
	}
	if sawNon429 == 0 {
		t.Fatal("toutes les requêtes ont été limitées : burst non appliqué")
	}
}

func TestMetricsExposeCounters(t *testing.T) {
	h := newSecuredRouter(t, nil, 0)

	// Génère du trafic puis scrape /metrics.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("/metrics : %d", res.Code)
	}
	body := res.Body.String()
	for _, want := range []string{
		"yahriacad_http_requests_total",
		"yahriacad_http_request_duration_seconds",
		"yahriacad_build_info",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics sans la famille %s", want)
		}
	}
}

func TestNormalizeRoute(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/healthz":                             "/healthz",
		"/api/v1/projects":                     "/api/v1/projects",
		"/api/v1/projects/42":                  "/api/v1/projects/{id}",
		"/api/v1/projects/42/route":            "/api/v1/projects/{id}/route",
		"/api/v1/projects/42/jobs/abc":         "/api/v1/projects/{id}/jobs/{id}",
		"/api/v1/projects/42/snapshots/s/diff": "/api/v1/projects/{id}/snapshots/{id}/diff",
		"/api/v1/arena/leaderboard":            "/api/v1/arena/leaderboard",
		"/api/v1/auth/login":                   "/api/v1/auth/login",
	}
	for in, want := range cases {
		if got := normalizeRoute(in); got != want {
			t.Errorf("normalizeRoute(%q) = %q, attendu %q", in, got, want)
		}
	}
}
