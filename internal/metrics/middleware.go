package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// visitorSet tracks unique (IP + UA + date) tuples for the current day.
var (
	visitorMu  sync.Mutex
	visitorDay string
	visitorSet = make(map[string]struct{})
)

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// Middleware instruments every HTTP request with the Prometheus metrics
// defined in this package.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := sanitizePath(r.URL.Path)

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		statusStr := strconv.Itoa(rw.status)

		HTTPRequestsTotal.WithLabelValues(r.Method, path, statusStr).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		HTTPResponseBytesTotal.Add(float64(rw.bytes))

		trackVisitor(r)
	})
}

// trackVisitor updates the unique-visitor gauge.
// Visitor key = IP + User-Agent + date (UTC), reset daily.
func trackVisitor(r *http.Request) {
	today := time.Now().UTC().Format("2006-01-02")
	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = xff
	}
	key := fmt.Sprintf("%s|%s|%s", ip, r.UserAgent(), today)

	visitorMu.Lock()
	defer visitorMu.Unlock()

	if visitorDay != today {
		visitorDay = today
		visitorSet = make(map[string]struct{})
	}

	if _, seen := visitorSet[key]; !seen {
		visitorSet[key] = struct{}{}
		HTTPUniqueVisitorsTotal.Set(float64(len(visitorSet)))
	}
}

// knownRoutes is the whitelist of route patterns the Shop service exposes.
// Anything outside this list (e.g. probes from random scanners) maps to
// "unknown" — keeping the cardinality of the `path` label strictly bounded.
//
// Each entry is matched after numeric/UUID segments have been replaced with
// {id} by sanitizePath. Keep this in sync with handlers registered in
// internal/server, internal/articles, internal/orders, internal/cart,
// internal/payment and internal/ui.
var knownRoutes = map[string]struct{}{
	"/":                             {},
	"/healthz":                      {},
	"/readyz":                       {},
	"/metrics":                      {},
	"/articles":                     {},
	"/articles/{id}":                {},
	"/cart":                         {},
	"/cart/remove":                  {},
	"/cart/{id}":                    {},
	"/orders":                       {},
	"/checkout":                     {},
	"/payment/pending":              {},
	"/payment/verify":               {},
	"/admin/login":                  {},
	"/admin/logout":                 {},
	"/admin/articles":               {},
	"/admin/articles/new":           {},
	"/admin/articles/{id}/edit":     {},
	"/admin/articles/{id}/delete":   {},
	"/admin/orders":                 {},
}

// sanitizePath returns the canonical route label for path.
//   1. Replaces numeric/UUID segments with {id}.
//   2. Drops the result to "unknown" if it is not in knownRoutes,
//      preventing label cardinality blow-up from scanner traffic.
func sanitizePath(path string) string {
	canonical := normalizeIDs(path)
	if _, ok := knownRoutes[canonical]; ok {
		return canonical
	}
	return "unknown"
}

func normalizeIDs(path string) string {
	var out []byte
	segment := make([]byte, 0, 64)
	flush := func() {
		if isIDSegment(string(segment)) {
			out = append(out, '{', 'i', 'd', '}')
		} else {
			out = append(out, segment...)
		}
		segment = segment[:0]
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			flush()
			out = append(out, '/')
		} else {
			segment = append(segment, path[i])
		}
	}
	flush()
	return string(out)
}

func isIDSegment(s string) bool {
	if len(s) == 0 {
		return false
	}
	if len(s) == 36 && s[8] == '-' && s[13] == '-' {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
