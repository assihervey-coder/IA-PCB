// Unit tests of the JWT authentication service: credential parsing, login,
// token verification (validity, expiry, algorithm confusion) and the
// middleware behaviour (bearer header, query token for WebSocket, public
// paths).
package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testSecret = "test-secret-0123456789abcdef"

func newTestService(t *testing.T, users ...string) *Service {
	t.Helper()
	return NewService(testSecret, time.Hour, users, nil)
}

func TestParseUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spec   string
		ok     bool
		name   string
		secret string
	}{
		{"admin:admin", true, "admin", "admin"},
		{"ops:$2a$10:x:y:z", true, "ops", "$2a$10:x:y:z"},
		{" spaced:user ", true, "spaced", "user"},
		{"nocolon", false, "", ""},
		{":nopass", false, "", ""},
		{"user:", false, "", ""},
		{"", false, "", ""},
	}
	for _, c := range cases {
		rec, ok := parseUser(c.spec)
		if ok != c.ok || rec.name != c.name || rec.passwordHash != c.secret {
			t.Errorf("parseUser(%q) = (%q,%v), attendu (%q,%v)",
				c.spec, rec.name, ok, c.name, c.ok)
		}
	}
}

func TestLoginOK(t *testing.T) {
	t.Parallel()
	s := newTestService(t, "admin:admin")

	token, exp, err := s.Login("admin", "admin")
	if err != nil {
		t.Fatalf("login refusé : %v", err)
	}
	if token == "" || strings.Count(token, ".") != 2 {
		t.Fatalf("jeton JWT mal formé : %q", token)
	}
	if !exp.After(time.Now()) {
		t.Fatalf("expiration passée : %v", exp)
	}

	id, err := s.Verify(token)
	if err != nil {
		t.Fatalf("vérification refusée : %v", err)
	}
	if id.Username != "admin" {
		t.Fatalf("identité = %q, attendu admin", id.Username)
	}
}

func TestLoginBadCredentials(t *testing.T) {
	t.Parallel()
	s := newTestService(t, "admin:admin")

	for _, attempt := range [][2]string{
		{"admin", "wrong"},
		{"ghost", "admin"},
		{"", "admin"},
	} {
		if _, _, err := s.Login(attempt[0], attempt[1]); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("login %v : attendu ErrInvalidCredentials, obtenu %v", attempt, err)
		}
	}
}

func TestLoginBcryptUser(t *testing.T) {
	t.Parallel()
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash bcrypt impossible : %v", err)
	}
	s := newTestService(t, "ops:"+string(hash))

	if _, _, err := s.Login("ops", "s3cret"); err != nil {
		t.Fatalf("login bcrypt refusé : %v", err)
	}
	if _, _, err := s.Login("ops", "bad"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("mauvais mot de passe accepté")
	}
}

func TestVerifyExpired(t *testing.T) {
	t.Parallel()
	s := NewService(testSecret, 10*time.Millisecond, []string{"admin:admin"}, nil)

	token, _, err := s.Login("admin", "admin")
	if err != nil {
		t.Fatalf("login refusé : %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if _, err := s.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("jeton expiré accepté : %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	s := newTestService(t, "admin:admin")

	if _, err := s.Verify("not.a.jwt"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("jeton forgé accepté : %v", err)
	}

	// Jeton signé avec un autre secret (clé inconnue).
	other := NewService("another-secret", time.Hour, nil, nil)
	token, _, _ := other.Login("admin", "admin")
	if _, err := s.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("signature étrangère acceptée : %v", err)
	}
}

func TestMiddlewareBearerAndQueryToken(t *testing.T) {
	t.Parallel()
	s := newTestService(t, "admin:admin")
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFrom(r.Context())
		if !ok || id.Username != "admin" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := s.Middleware(protected)

	// 401 sans jeton.
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("sans jeton : %d, attendu 401", res.Code)
	}

	token, _, _ := s.Login("admin", "admin")

	// 200 avec Bearer.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("avec Bearer : %d, attendu 200", res.Code)
	}

	// 200 avec ?token= (poignée de main WebSocket).
	req = httptest.NewRequest(http.MethodGet, "/ws/v1/progress?project_id=p&token="+token, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("avec query token : %d, attendu 200", res.Code)
	}
}

func TestMiddlewarePublicPathsAndPreflight(t *testing.T) {
	t.Parallel()
	s := newTestService(t, "admin:admin")
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/healthz", "/metrics", "/api/v1/auth/login"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Errorf("chemin public %s : %d, attendu 200", path, res.Code)
		}
	}

	// Préflight OPTIONS : passe sans jeton.
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil))
	if res.Code != http.StatusOK {
		t.Errorf("préflight OPTIONS : %d, attendu 200", res.Code)
	}
}

func TestGenerateEphemeralSecret(t *testing.T) {
	t.Parallel()
	a, err := GenerateEphemeralSecret()
	if err != nil || len(a) < 32 {
		t.Fatalf("secret éphémère trop court : %q (%v)", a, err)
	}
	b, _ := GenerateEphemeralSecret()
	if a == b {
		t.Fatal("deux secrets éphémères identiques")
	}
}
