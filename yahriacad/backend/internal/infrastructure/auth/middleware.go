// Middleware enforcing JWT authentication on the HTTP stack. Public paths
// (health, metrics, login) and CORS preflight (OPTIONS) stay open; every
// other request must present `Authorization: Bearer <jwt>` — or a `token`
// query parameter, used by the WebSocket handshake where browsers cannot
// attach headers.
package auth

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const identityKey ctxKey = 1

// PublicPaths are the endpoints reachable without a token.
var PublicPaths = map[string]bool{
	"/healthz":           true,
	"/metrics":           true,
	"/api/v1/auth/login": true,
}

// Middleware returns a handler that verifies the token before delegating to
// next. The response shape mirrors the REST error contract:
// {"error":{"code":"unauthorized","message":…}}.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preflight CORS n'embarque jamais de jeton : laissez la couche
		// CORS répondre 204, sinon tout navigateur cross-origin casse.
		if r.Method == http.MethodOptions || PublicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		token := bearerToken(r)
		if token == "" {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if token == "" {
			unauthorized(w, "jeton manquant")
			return
		}

		id, err := s.Verify(token)
		if err != nil {
			unauthorized(w, err.Error())
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, id)))
	})
}

// bearerToken extracts "Authorization: Bearer <token>" (case-insensitive
// scheme).
func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// unauthorized writes the 401 payload with the standard error envelope.
func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="yahriacad"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"` + message + `"}}`))
}

// IdentityFrom retrieves the authenticated identity stored by the middleware.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey).(Identity)
	return id, ok
}
