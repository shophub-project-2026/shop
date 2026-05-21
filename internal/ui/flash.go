package ui

import (
	"net/http"
	"net/url"
	"time"
)

// Flash kinds — kept short so they fit comfortably in a cookie value.
const (
	flashSuccess = "ok"
	flashInfo    = "info"
	flashError   = "err"

	flashCookie = "shop_flash"
)

// FlashMessage is what the layout template receives.
// Empty Kind means "no flash".
type FlashMessage struct {
	Kind string // "ok" | "info" | "err"
	Text string
}

// setFlash drops a short-lived cookie containing the flash. The cookie is
// URL-encoded "kind|text" so it survives one redirect and is consumed by
// the next render.
func setFlash(w http.ResponseWriter, kind, text string) {
	val := url.QueryEscape(kind + "|" + text)
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30,
	})
}

// takeFlash returns the pending flash, if any, and clears the cookie on the
// same response. Safe to call once per request.
func takeFlash(w http.ResponseWriter, r *http.Request) FlashMessage {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return FlashMessage{}
	}
	decoded, err := url.QueryUnescape(c.Value)
	if err != nil {
		return FlashMessage{}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	for i := 0; i < len(decoded); i++ {
		if decoded[i] == '|' {
			return FlashMessage{Kind: decoded[:i], Text: decoded[i+1:]}
		}
	}
	return FlashMessage{}
}
