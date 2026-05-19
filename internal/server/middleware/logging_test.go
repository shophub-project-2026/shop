package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogging_EmitsStructuredLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest(http.MethodPost, "/articles", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("User-Agent", "shop-test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status passthrough = %d, want %d", rec.Code, http.StatusCreated)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v -- raw: %q", err, buf.String())
	}

	wantFields := map[string]any{
		"method":     "POST",
		"path":       "/articles",
		"status":     float64(http.StatusCreated), // JSON unmarshals numbers as float64
		"bytes":      float64(2),
		"remote":     "10.0.0.1:1234",
		"user_agent": "shop-test",
	}
	for k, want := range wantFields {
		if got := entry[k]; got != want {
			t.Errorf("log field %q = %v, want %v", k, got, want)
		}
	}
	if _, ok := entry["duration"]; !ok {
		t.Errorf("log entry is missing duration field; have: %v", entry)
	}
}

func TestLogging_DefaultStatusIs200(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Handler calls Write without WriteHeader -- Go implicitly sends 200.
	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Errorf("log line should default status to 200, got: %s", buf.String())
	}
}
