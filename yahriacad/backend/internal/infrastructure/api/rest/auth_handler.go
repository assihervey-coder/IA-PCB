// REST handlers for authentication: POST /api/v1/auth/login issues a JWT
// from the configured accounts, GET /api/v1/auth/me echoes the authenticated
// identity (used by the frontend to validate a stored token at boot).
package rest

import (
	"errors"
	"net/http"
	"time"

	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/auth"
)

// authLoginRequest is the login payload.
type authLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authLoginResponse is the issued token (contrat docs/architecture/contracts.md §2).
type authLoginResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresAt string `json:"expires_at"`
	Username  string `json:"username"`
}

// authMeResponse echoes the authenticated identity.
type authMeResponse struct {
	Username string `json:"username"`
}

// handleLogin answers POST /api/v1/auth/login. Available only when the
// service is wired; without auth the route answers 503 so clients understand
// credentials are not the access path (the API stays open).
func (d *Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	if d.Auth == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_disabled",
			"authentification désactivée (API ouverte) : configurez YAHRIACAD_JWT_SECRET")
		return
	}

	var req authLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, r, http.StatusBadRequest, "invalid", "username et password requis")
		return
	}

	token, exp, err := d.Auth.Login(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// 401 volontairement générique : ne pas divulguer si le compte existe.
			writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "identifiants invalides")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal", "erreur interne du serveur")
		return
	}

	writeJSON(w, http.StatusOK, authLoginResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: exp.Format(time.RFC3339),
		Username:  req.Username,
	})
}

// handleMe answers GET /api/v1/auth/me for a valid token.
func (d *Deps) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "jeton manquant ou invalide")
		return
	}
	writeJSON(w, http.StatusOK, authMeResponse{Username: id.Username})
}
