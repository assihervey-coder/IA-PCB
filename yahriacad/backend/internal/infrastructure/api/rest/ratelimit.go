// Per-IP rate limiting for the AI-costly routes (place, route, optimize,
// magic, arena…). Each client address owns a token bucket
// (golang.org/x/time/rate); idle buckets are reaped by a background
// sweeper so the map stays bounded under IP churn.
package rest

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// visitor couples a bucket with its last-seen timestamp for the sweeper.
type visitor struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// ipRateLimiter is the per-address bucket registry.
type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rps      rate.Limit
	burst    int
}

// newIPRateLimiter creates the registry (rps <= 0 is rejected by the caller).
func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	l := &ipRateLimiter{
		visitors: make(map[string]*visitor),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go l.sweep()
	return l
}

// sweep deletes buckets idle for more than 10 minutes, once per minute.
func (l *ipRateLimiter) sweep() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		l.mu.Lock()
		for k, v := range l.visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(l.visitors, k)
			}
		}
		l.mu.Unlock()
	}
}

// allow reports whether the request from r may pass, and returns the bucket
// reservation delay for the Retry-After hint.
func (l *ipRateLimiter) allow(r *http.Request) (bool, time.Duration) {
	key := clientKey(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.visitors[key]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.rps, l.burst)}
		l.visitors[key] = v
	}
	v.lastSeen = time.Now()
	if v.lim.Allow() {
		return true, 0
	}
	// Délai avant le prochain jeton (plafonné pour l'en-tête Retry-After).
	d := v.lim.Reserve().Delay()
	return false, d
}

// clientKey resolves the client identity: X-Forwarded-For first (reverse
// proxy documented in configs/production/nginx.conf), then the socket
// address without port.
func clientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Premier saut = client d'origine (le proxy ajoute en tête).
		if i := indexByte(xff, ','); i > 0 {
			return trimSpace(xff[:i])
		}
		return xff
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// indexByte/trimSpace avoid pulling strings just for two trivial uses.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// aiLimited wraps an AI-costly handler with the per-IP bucket. When no
// limiter is wired (Deps.AIRate == nil, CI/tests) the handler passes
// through untouched.
func (d *Deps) aiLimited(next http.HandlerFunc) http.HandlerFunc {
	if d.AIRate == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ok, wait := d.AIRate.allow(r)
		if !ok {
			seconds := int(wait/time.Second) + 1
			w.Header().Set("Retry-After", trimSpace(itoa(seconds)))
			writeError(w, r, http.StatusTooManyRequests, "rate_limited",
				"trop de requêtes IA : ralentissez (fenêtre par adresse IP)")
			return
		}
		next(w, r)
	}
}

// itoa renders a small non-negative int (rate-limit header only).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
