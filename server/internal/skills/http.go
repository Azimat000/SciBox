package skills

import (
	"errors"
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

// NewHandler собирает обработчик. requireUser — проверка входа (auth.Handler.RequireUser): подсказки нужны только
// форме профиля, копию справочника по частям не отдаём гостям.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает GET /skills?kind=&q=&limit= к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Get("/skills", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		res, err := h.svc.Search(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("q"), limit)
		switch {
		case errors.Is(err, ErrKind):
			apierr.WriteFieldErrors(w, "Укажите вид навыков: research или general.", map[string]string{"kind": "Допустимо research или general."})
		case err != nil:
			apierr.WriteInternal(w, r, h.logger, "skills", err)
		default:
			apierr.WriteJSON(w, http.StatusOK, res)
		}
	})
}
