package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// CSRF cookie / form-field name used by the double-submit pattern.
const (
	CSRFCookieName = "shop_csrf"
	CSRFFieldName  = "csrf_token"
	csrfTokenBytes = 32
)

// CSRF returns a middleware that implements the double-submit-cookie CSRF
// pattern, suitable for browser-driven HTML forms.
//
//   - For every request without a shop_csrf cookie we issue one with a
//     cryptographically random value.
//   - "Safe" methods (GET/HEAD/OPTIONS) are not checked.
//   - Other methods are accepted only when the form/header value matches
//     the cookie. A request reaching us from a foreign origin will not be
//     able to set or read this cookie, so the form value cannot match.
//
// Skip(r) lets callers carve out exceptions (e.g. JSON APIs hit from cURL).
// Returning true bypasses the check; returning false applies it.
func CSRF(skip func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := readOrIssueToken(w, r)

			if !isSafeMethod(r.Method) && (skip == nil || !skip(r)) {
				submitted := submittedToken(r)
				if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
					http.Error(w, "csrf token mismatch", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFToken returns the current request's CSRF token. Use it in templates
// to populate hidden form fields. Returns "" if no token has been issued
// yet (which should not happen once CSRF middleware is wired up).
func CSRFToken(r *http.Request) string {
	if c, err := r.Cookie(CSRFCookieName); err == nil {
		return c.Value
	}
	return ""
}

func readOrIssueToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(CSRFCookieName); err == nil && len(c.Value) == csrfTokenBytes*2 {
		return c.Value
	}
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// Extremely unlikely; fall back to empty so the check fails closed.
		return ""
	}
	tok := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: false, // readable by inline JS that auto-fills form fields
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 60 * 60,
	})
	// Make the new cookie visible to handlers further down the chain that
	// might re-read it via r.Cookie.
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: tok})
	return tok
}

func submittedToken(r *http.Request) string {
	if v := r.Header.Get("X-CSRF-Token"); v != "" {
		return v
	}
	if err := r.ParseForm(); err == nil {
		if v := r.FormValue(CSRFFieldName); v != "" {
			return v
		}
	}
	return ""
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
