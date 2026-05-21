package articles

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// Handler exposes the articles Repository over HTTP.
type Handler struct {
	repo   Repository
	logger *slog.Logger
}

// NewHandler constructs an article Handler.
func NewHandler(repo Repository, logger *slog.Logger) *Handler {
	return &Handler{repo: repo, logger: logger}
}

// RegisterRoutes wires the article routes onto mux.
// Public: GET /articles, GET /articles/{id}
// Admin (require X-Admin-Key): POST, PUT, DELETE
func (h *Handler) RegisterRoutes(mux *http.ServeMux, adminMW func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /articles", h.list)
	mux.HandleFunc("GET /articles/{id}", h.get)
	mux.Handle("POST /articles", adminMW(http.HandlerFunc(h.create)))
	mux.Handle("PUT /articles/{id}", adminMW(http.HandlerFunc(h.update)))
	mux.Handle("DELETE /articles/{id}", adminMW(http.HandlerFunc(h.delete)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	if len(search) > MaxSearchLength {
		writeError(w, http.StatusBadRequest, "search query too long")
		return
	}

	limit, offset, ok := parsePagination(w, r)
	if !ok {
		return
	}

	result, total, err := h.repo.List(r.Context(), search, limit, offset)
	if err != nil {
		h.internalError(w, "list articles", err)
		return
	}
	if result == nil {
		result = []Article{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, result)
}

// parsePagination reads ?limit= and ?offset= from the request.
// Returns defaultListLimit when limit is absent; 0 when offset is absent.
// Writes a 400 and returns ok=false on invalid input.
func parsePagination(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	limit = defaultListLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxListLimit {
			writeError(w, http.StatusBadRequest, "limit must be an integer between 1 and 200")
			return 0, 0, false
		}
		limit = n
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r.PathValue("id"))
	if !ok {
		return
	}
	article, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "article not found")
		return
	}
	if err != nil {
		h.internalError(w, "get article", err)
		return
	}
	writeJSON(w, http.StatusOK, article)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	article, err := h.repo.Create(r.Context(), in)
	if err != nil {
		h.internalError(w, "create article", err)
		return
	}
	writeJSON(w, http.StatusCreated, article)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var in UpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	article, err := h.repo.Update(r.Context(), id, in)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "article not found")
		return
	}
	if err != nil {
		h.internalError(w, "update article", err)
		return
	}
	writeJSON(w, http.StatusOK, article)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r.PathValue("id"))
	if !ok {
		return
	}
	err := h.repo.Delete(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "article not found")
		return
	}
	if err != nil {
		h.internalError(w, "delete article", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// helpers

func parseUUID(w http.ResponseWriter, s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid article id")
		return uuid.UUID{}, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) internalError(w http.ResponseWriter, op string, err error) {
	h.logger.Error(op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
