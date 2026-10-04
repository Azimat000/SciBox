package journals

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
)

// Handler — HTTP-часть справочника.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчик. requireUser — проверка входа (auth.Handler.RequireUser): справочник нужен только
// в форме публикации, своей копией данных SCImago наружу не делимся.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// SearchResult — ответ поиска.
type SearchResult struct {
	Items []Found `json:"items"`
}

// Mount подключает GET /journals?q=&limit= к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Get("/journals", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := h.svc.Search(r.Context(), r.URL.Query().Get("q"), limit)
		if err != nil {
			apierr.WriteInternal(w, r, h.logger, "journals", err)
			return
		}
		apierr.WriteJSON(w, http.StatusOK, SearchResult{Items: items})
	})
}
