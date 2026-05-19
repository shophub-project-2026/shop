package articles

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
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
	articles, err := h.repo.List(r.Context(), search)
	if err != nil {
		h.internalError(w, "list articles", err)
		return
	}
	if articles == nil {
		articles = []Article{}
	}
	writeJSON(w, http.StatusOK, articles)
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
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.Quantity < 0 {
		writeError(w, http.StatusBadRequest, "quantity must be >= 0")
		return
	}
	if in.Price <= 0 {
		writeError(w, http.StatusBadRequest, "price must be > 0")
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
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
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
