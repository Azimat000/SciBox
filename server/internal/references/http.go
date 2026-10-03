package references

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
	"scibox/server/internal/files"
)

// Коды ошибок этого раздела.
const (
	CodeInvalidLink     = "invalid_link"
	CodeLinkExpired     = "link_expired"
	CodeWithdrawn       = "application_withdrawn"
	CodeAlreadyAnswered = "already_answered"
	CodeTooMany         = "too_many_references"
	CodeNotPending      = "reference_not_pending"
	CodeClosed          = "application_closed"
	CodeResendTooSoon   = "resend_too_soon"
)

// maxJSON — самое большое тело запроса с JSON (письмо до 12 000 знаков по-русски).
const maxJSON = 64 << 10

// Handler — HTTP-часть рекомендательных писем.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса к роутеру, который уже живёт под /api и прошёл Authenticate. Адреса /applications/... задаются
// по одному, а не поддеревом: те же начала у обработчиков откликов.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Post("/applications/{id}/references", h.add)
	r.With(h.requireUser).Post("/applications/{id}/references/{refId}/resend", h.resend)
	r.With(h.requireUser).Delete("/applications/{id}/references/{refId}", h.cancel)
	// Рекомендатель приходит по ссылке без аккаунта.
	r.Post("/recommendations/lookup", h.lookup)
	r.Post("/recommendations/submit", h.submit)
	r.Post("/recommendations/decline", h.decline)
}

func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

func ids(r *http.Request) (app, ref uuid.UUID, ok bool) {
	app, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	if p := chi.URLParam(r, "refId"); p != "" {
		if ref, err = uuid.Parse(p); err != nil {
			return uuid.Nil, uuid.Nil, false
		}
	}
	return app, ref, true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr *auth.ValidationError
		rerr *auth.RateLimitedError
		serr *TooSoonError
	)
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, "Проверьте поля формы", verr.Fields)
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Слишком много просьб о рекомендациях за сутки. Попробуйте завтра", RetryAfter: secs,
		}})
	case errors.As(err, &serr):
		secs := int(math.Ceil(serr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: CodeResendTooSoon, Message: "Письмо с просьбой уже отправлено недавно. Повторить можно позже", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такого отклика или просьбы нет")
	case errors.Is(err, ErrTooMany):
		apierr.WriteError(w, http.StatusConflict, CodeTooMany, "В отклике уже три рекомендателя — больше нельзя")
	case errors.Is(err, ErrClosed):
		apierr.WriteError(w, http.StatusConflict, CodeClosed, "Отклик закрыт: просить рекомендации уже нельзя")
	case errors.Is(err, ErrNotPending):
		apierr.WriteError(w, http.StatusConflict, CodeNotPending, "Рекомендатель уже ответил: это действие недоступно")
	case errors.Is(err, ErrInvalidLink):
		apierr.WriteError(w, http.StatusNotFound, CodeInvalidLink, "Ссылка недействительна. Возможно, вам прислали новую: откройте последнее письмо")
	case errors.Is(err, ErrExpired):
		apierr.WriteError(w, http.StatusGone, CodeLinkExpired, "Срок ссылки вышел. Попросите кандидата отправить просьбу ещё раз")
	case errors.Is(err, ErrGone):
		apierr.WriteError(w, http.StatusGone, CodeWithdrawn, "Кандидат отозвал отклик, поэтому письмо уже не нужно. Спасибо")
	case errors.Is(err, ErrAlreadyAnswered):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyAnswered, "На эту ссылку уже ответили")
	default:
		h.logger.Error("references request failed", "path", r.URL.Path, "err", err)
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternal, "Что-то сломалось на сервере. Попробуйте ещё раз")
	}
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	app, _, ok := ids(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	var in RefereeInput
	if !apierr.DecodeJSON(w, r, &in, maxJSON) {
		return
	}
	req, err := h.svc.Add(r.Context(), actor(r), app, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Request{"reference": req})
}

func (h *Handler) resend(w http.ResponseWriter, r *http.Request) {
	app, ref, ok := ids(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	req, err := h.svc.Resend(r.Context(), actor(r), app, ref)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Request{"reference": req})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	app, ref, ok := ids(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.Cancel(r.Context(), actor(r), app, ref); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tokenBody struct {
	Token string `json:"token"`
}

func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	var in tokenBody
	if !apierr.DecodeJSON(w, r, &in, maxJSON) {
		return
	}
	info, err := h.svc.Lookup(r.Context(), in.Token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Info{"request": info})
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		tokenBody
		LetterInput
	}
	ups, ok := files.Decode(w, r, &in, maxJSON, 1, "file")
	if !ok {
		return
	}
	var up *files.Upload
	if len(ups) == 1 {
		up = &ups[0]
	}
	if err := h.svc.Submit(r.Context(), in.Token, in.LetterInput, up); err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]string{"status": StatusReceived})
}

func (h *Handler) decline(w http.ResponseWriter, r *http.Request) {
	var in tokenBody
	if !apierr.DecodeJSON(w, r, &in, maxJSON) {
		return
	}
	if err := h.svc.Decline(r.Context(), in.Token); err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]string{"status": StatusDeclined})
}
