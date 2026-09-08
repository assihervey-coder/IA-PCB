// Prometheus instrumentation: /metrics exposition and the red-metrics
// middleware (request counter + latency histogram by method, normalized
// route and status). Routes are normalized so high-cardinality ids
// (project/job/snapshot) land on template labels.
package rest

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metrics holds the package-level registry (default prometheus registry:
// compatible with any already-running exporter in the process).
var (
	metricRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yahriacad_http_requests_total",
			Help: "Requêtes HTTP traitées, par méthode, route normalisée et statut.",
		},
		[]string{"method", "route", "status"},
	)
	metricRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yahriacad_http_request_duration_seconds",
			Help:    "Latence des requêtes HTTP (secondes), par méthode et route normalisée.",
			Buckets: prometheus.ExponentialBuckets(0.005, 2, 12), // 5ms → ~10s
		},
		[]string{"method", "route"},
	)
	metricBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yahriacad_build_info",
			Help: "Version et mode de persistance du serveur.",
		},
		[]string{"version", "database"},
	)
	metricsBuildInfoSet bool
)

// handleMetrics answers GET /metrics (Prometheus exposition format).
func (d *Deps) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !metricsBuildInfoSet {
		metricBuildInfo.WithLabelValues(d.Version, d.Database).Set(1)
		metricsBuildInfoSet = true
	}
	promhttp.Handler().ServeHTTP(w, r)
}

// instrument records counter/histogram for one request. The route label uses
// the normalized template to keep cardinality bounded.
func (d *Deps) instrument(rec *statusRecorder, r *http.Request, elapsedSeconds float64) {
	route := normalizeRoute(r.URL.Path)
	metricRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
	metricRequestDuration.WithLabelValues(r.Method, route).Observe(elapsedSeconds)
}

// collection markers whose next path segment is an identifier.
var idAfterSegment = map[string]bool{
	"projects":   true, // /api/v1/projects/{id}
	"jobs":       true, // /api/v1/projects/{id}/jobs/{jobID}
	"snapshots":  true, // /api/v1/projects/{id}/snapshots/{sid}
	"pours":      true, // DELETE /api/v1/projects/{id}/pours/{pourID}
	"netclasses": true, // /api/v1/projects/{id}/netclasses/{net|class}
}

// knownAPISegments are the /api/v1/{segment} collections with fixed route
// templates: their normalized path is kept whole (bounded cardinality by
// construction). Any other /api/v1 subpath collapses to one series.
var knownAPISegments = map[string]bool{
	"projects": true,
	"arena":    true,
	"auth":     true,
	"ai":       true,
	"demo":     true,
}

// normalizeRoute maps concrete paths onto bounded templates:
// /api/v1/projects/42/route → /api/v1/projects/{id}/route, /healthz stays
// /healthz. Unknown prefixes collapse so request spam cannot create
// unbounded metric series.
func normalizeRoute(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 0 || segs[0] == "" {
		return "/"
	}

	// Routes API connues : substitution des identifiants, pas de troncature
	// (chaque gabarit est fixe, enregistré dans le mux).
	if segs[0] == "api" && len(segs) >= 3 && segs[1] == "v1" && knownAPISegments[segs[2]] {
		out := make([]string, 0, len(segs))
		for i := 0; i < len(segs); i++ {
			seg := segs[i]
			if idAfterSegment[seg] {
				out = append(out, seg)
				if i+1 < len(segs) {
					out = append(out, "{id}")
					i++
				}
				continue
			}
			out = append(out, seg)
		}
		return "/" + strings.Join(out, "/")
	}

	// Chemins inconnus : /api/v1 hors gabarits → une seule série ; les
	// autres préfixes sont tronqués à 4 segments.
	if segs[0] == "api" {
		return "/api/v1/{unmatched}"
	}
	if len(segs) > 4 {
		segs = segs[:4]
	}
	return "/" + strings.Join(segs, "/")
}
