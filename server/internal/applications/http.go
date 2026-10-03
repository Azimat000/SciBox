package applications

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
	CodeVacancyClosed   = "vacancy_closed"
	CodeDeadlinePassed  = "deadline_passed"
	CodeOwnVacancy      = "own_vacancy"
	CodeAlreadyApplied  = "already_applied"
	CodeInvalidStatusCh = "invalid_status_change"
)

// Handler — HTTP-часть откликов.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса к роутеру, который уже живёт под /api и прошёл Authenticate. Адреса /applications/...
// задаются по одному, а не поддеревом: те же начала у обработчика рекомендаций.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Post("/applications", h.apply)
	r.With(h.requireUser).Get("/applications", h.mine)
	r.With(h.requireUser).Get("/applications/for-vacancy/{vacancyId}", h.forVacancy)
	r.With(h.requireUser).Get("/applications/{id}", h.get)
	r.With(h.requireUser).Get("/applications/{id}/files/{fileId}", h.file)
	r.With(h.requireUser).Post("/applications/{id}/withdraw", h.withdraw)
}

func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

func param(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	return id, err == nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr *auth.ValidationError
		rerr *auth.RateLimitedError
	)
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, "Проверьте отклик", verr.Fields)
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Вы отправили много откликов за сутки. Продолжите завтра", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такого отклика нет")
	case errors.Is(err, ErrVacancyClosed):
		apierr.WriteError(w, http.StatusConflict, CodeVacancyClosed, "Вакансия закрыта: отклики не принимаются")
	case errors.Is(err, ErrDeadlinePassed):
		apierr.WriteError(w, http.StatusConflict, CodeDeadlinePassed, "Срок подачи заявок прошёл")
	case errors.Is(err, ErrOwnVacancy):
		apierr.WriteError(w, http.StatusForbidden, CodeOwnVacancy, "Вы ведёте эту вакансию и видите отклики на неё, поэтому откликнуться нельзя")
	case errors.Is(err, ErrAlreadyApplied):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyApplied, "Вы уже откликнулись на эту вакансию")
	case errors.Is(err, ErrBadStatus):
		apierr.WriteError(w, http.StatusConflict, CodeInvalidStatusCh, "Этот отклик уже нельзя отозвать: по нему принято решение")
	default:
		h.logger.Error("applications request failed", "path", r.URL.Path, "err", err)
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternal, "Что-то сломалось на сервере. Попробуйте ещё раз")
	}
}

func intParam(r *http.Request, name string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return n
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	var in Input
	ups, ok := files.Decode(w, r, &in, maxFormJSON, files.MaxAttachments+1, "files")
	if !ok {
		return
	}
	d, err := h.svc.Apply(r.Context(), actor(r), in, ups)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Detail{"application": d})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Mine(r.Context(), actor(r), intParam(r, "limit", 20), intParam(r, "offset", 0))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) forVacancy(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "vacancyId")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	st, err := h.svc.ForVacancy(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	d, err := h.svc.Get(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Detail{"application": d})
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	id, ok1 := param(r, "id")
	fid, ok2 := param(r, "fileId")
	if !ok1 || !ok2 {
		h.fail(w, r, ErrNotFound)
		return
	}
	name, data, err := h.svc.File(r.Context(), actor(r), id, fid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	files.Serve(w, name, data)
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.Withdraw(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
