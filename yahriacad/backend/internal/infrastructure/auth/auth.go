// Package auth provides the JWT (HS256) authentication of the YahriaCad
// backend: user credentials come from the configuration (plaintext for dev,
// bcrypt hashes for production), login issues a signed token and the HTTP
// middleware enforces it on every route except the public ones (healthz,
// metrics, login itself). The WebSocket upgrade accepts the token as a
// `token` query parameter because browsers cannot set headers on WS.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors mapped onto HTTP responses by the REST layer.
var (
	// ErrInvalidCredentials is returned when the username/password pair does
	// not match any configured user (401).
	ErrInvalidCredentials = errors.New("identifiants invalides")
	// ErrInvalidToken is returned for a malformed, forged or expired token
	// (401).
	ErrInvalidToken = errors.New("jeton invalide ou expiré")
)

// userRecord holds a configured account. passwordHash is either a bcrypt
// digest (values starting with "$2") or the raw plaintext kept in memory for
// constant-time comparison (dev convenience only).
type userRecord struct {
	name         string
	passwordHash string
}

// Service signs and verifies the JWT tokens of the backend.
type Service struct {
	secret []byte
	ttl    time.Duration
	users  map[string]userRecord
	log    *slog.Logger
}

// Identity is the authenticated subject attached to a request context.
type Identity struct {
	Username string
}

type claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// NewService builds an authentication service. The secret must not be empty
// (callers enforce it) and users may be empty: login then always fails while
// token verification keeps working (tokens previously issued stay verifiable
// across a user-list change).
func NewService(secret string, ttl time.Duration, users []string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	s := &Service{
		secret: []byte(secret),
		ttl:    ttl,
		users:  make(map[string]userRecord, len(users)),
		log:    log,
	}
	for _, spec := range users {
		rec, ok := parseUser(spec)
		if !ok {
			log.Warn("auth : entrée utilisateur ignorée (attendu \"user:motdepasse\")", "entree", spec)
			continue
		}
		if _, dup := s.users[rec.name]; dup {
			log.Warn("auth : utilisateur dupliqué ignoré", "utilisateur", rec.name)
			continue
		}
		if !strings.HasPrefix(rec.passwordHash, "$2") {
			log.Warn("auth : mot de passe en clair (dev) — préférez un hachage bcrypt",
				"utilisateur", rec.name)
		}
		s.users[rec.name] = rec
	}
	return s
}

// parseUser splits a "user:password" entry at the FIRST colon so bcrypt
// digests containing colons survive. bcrypt hashes ($2a$/$2b$/$2y$) are
// detected by their prefix and used as-is.
func parseUser(spec string) (userRecord, bool) {
	spec = strings.TrimSpace(spec)
	i := strings.Index(spec, ":")
	if i <= 0 || i == len(spec)-1 {
		return userRecord{}, false
	}
	return userRecord{name: spec[:i], passwordHash: spec[i+1:]}, true
}

// UserCount reports the number of configured accounts (for startup logs).
func (s *Service) UserCount() int { return len(s.users) }

// TTL exposes the token lifetime (for login responses and tests).
func (s *Service) TTL() time.Duration { return s.ttl }

// Login checks the credentials and returns a signed JWT with its expiry.
func (s *Service) Login(username, password string) (string, time.Time, error) {
	username = strings.TrimSpace(username)
	rec, ok := s.users[username]
	if !ok || !s.passwordMatches(rec, password) {
		return "", time.Time{}, ErrInvalidCredentials
	}

	now := time.Now()
	exp := now.Add(s.ttl)
	c := claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			Issuer:    "yahriacad",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signature du jeton impossible : %w", err)
	}
	s.log.Info("auth : connexion", "utilisateur", username, "expire", exp.Format(time.RFC3339))
	return token, exp, nil
}

// passwordMatches compares the candidate password against the stored secret:
// bcrypt digests via bcrypt, plaintext via constant-time comparison.
func (s *Service) passwordMatches(rec userRecord, password string) bool {
	if strings.HasPrefix(rec.passwordHash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(rec.passwordHash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(rec.passwordHash), []byte(password)) == 1
}

// Verify parses and validates a token: signature (HS256 only), expiry and
// issuer. It returns the authenticated identity.
func (s *Service) Verify(token string) (Identity, error) {
	var c claims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("yahriacad"),
		jwt.WithExpirationRequired(),
	)
	if _, err := parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) {
		return s.secret, nil
	}); err != nil {
		return Identity{}, ErrInvalidToken
	}
	if strings.TrimSpace(c.Username) == "" {
		return Identity{}, ErrInvalidToken
	}
	return Identity{Username: c.Username}, nil
}

// GenerateEphemeralSecret produces a 256-bit random secret for development
// environments where no secret was configured (tokens die with the process,
// which is acceptable locally but never in production).
func GenerateEphemeralSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("génération du secret impossible : %w", err)
	}
	return hex.EncodeToString(buf), nil
}
