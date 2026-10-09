package dataexport

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
)

// Handler — HTTP-часть выгрузки.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчик. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адрес к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Get("/account/export", h.export)
}

// export отдаёт архив ZIP. Всё, что может не получиться (пароль, лимит, чтение базы), выясняется до первого байта
// ответа: человек получает понятную ошибку, а не битый файл.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	arch, err := h.svc.Prepare(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/zip")
	hdr.Set("Content-Disposition", `attachment; filename="`+arch.FileName()+`"`)
	if err := h.svc.Write(r.Context(), w, arch); err != nil {
		if r.Context().Err() == nil {
			h.logger.Error("data export stream failed", "path", r.URL.Path, "err", err)
		}
		// Заголовки уже ушли: честно оборвать передачу, чтобы браузер не принял недописанный архив за готовый.
		panic(http.ErrAbortHandler)
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var rerr *auth.RateLimitedError
	switch {
	case errors.Is(err, auth.ErrReauthRequired):
		auth.WriteReauthRequired(w)
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Архив с вашими данными можно скачать не чаще трёх раз в сутки. Попробуйте позже", RetryAfter: secs,
		}})
	default:
		apierr.WriteInternal(w, r, h.logger, "data export", err)
	}
}
