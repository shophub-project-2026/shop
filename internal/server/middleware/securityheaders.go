package middleware

import "net/http"

// SecurityHeaders sets a defence-in-depth set of HTTP response headers on
// every response. Values are chosen to be compatible with the JS we
// actually load (ethers.js and Tailwind from public CDNs) — tighten the
// CSP further if you remove those CDN dependencies.
//
// HSTS is intentionally short (max-age=60) because this template ships
// without TLS termination; raise it once the service is fronted by HTTPS.
func SecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; " +
		"img-src 'self' data:; " +
		"connect-src 'self' https:; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", csp)
		h.Set("Strict-Transport-Security", "max-age=60")
		next.ServeHTTP(w, r)
	})
}
