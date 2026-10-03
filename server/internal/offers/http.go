package offers

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
)

// maxBody — самый большой допустимый размер тела запроса (сообщение до 1000 знаков по-русски).
const maxBody = 16 << 10

// Коды ошибок этого раздела.
const (
	CodeVacancyClosed  = "vacancy_closed"
	CodeDeadlinePassed = "deadline_passed"
	CodeAlreadyOffered = "already_offered"
	CodeAlreadyApplied = "already_applied"
	CodeStaffInvitee   = "invitee_is_staff"
	CodeInvalidState   = "invalid_offer_state"
)

// Handler — HTTP-часть приглашений.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Post("/offers", h.invite)
	r.With(h.requireUser).Get("/offers", h.mine)
	r.With(h.requireUser).Get("/offers/{id}", h.get)
	r.With(h.requireUser).Post("/offers/{id}/answer", h.answer)
	r.With(h.requireUser).Post("/offers/{id}/cancel", h.cancel)
	r.With(h.requireUser).Get("/my/sent-offers", h.sent)
	r.With(h.requireUser).Get("/scientists/{id}/offer-targets", h.targets)
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
		apierr.WriteFieldErrors(w, "Проверьте приглашение", verr.Fields)
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Вы отправили много приглашений за сутки. Продолжите завтра", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такого приглашения, вакансии или учёного нет")
	case errors.Is(err, ErrVacancyClosed):
		apierr.WriteError(w, http.StatusConflict, CodeVacancyClosed, "Вакансия не опубликована или закрыта: приглашать на неё нельзя")
	case errors.Is(err, ErrDeadlinePassed):
		apierr.WriteError(w, http.StatusConflict, CodeDeadlinePassed, "Срок подачи заявок прошёл: приглашать на вакансию уже поздно")
	case errors.Is(err, ErrAlreadyOffered):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyOffered, "Этого человека уже приглашали на эту вакансию")
	case errors.Is(err, ErrAlreadyApplied):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyApplied, "Этот человек уже откликнулся на вакансию")
	case errors.Is(err, ErrStaffInvitee):
		apierr.WriteError(w, http.StatusConflict, CodeStaffInvitee, "Этот человек сам ведёт вакансию: приглашать его нельзя")
	case errors.Is(err, ErrBadState):
		apierr.WriteError(w, http.StatusConflict, CodeInvalidState, "С этим приглашением это действие уже невозможно: оно изменилось. Обновите страницу")
	default:
		h.logger.Error("offers request failed", "path", r.URL.Path, "err", err)
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternal, "Что-то сломалось на сервере. Попробуйте ещё раз")
	}
}

func intParam(r *http.Request, name string) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	var in InviteInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	o, err := h.svc.Invite(r.Context(), actor(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Offer{"offer": o})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Mine(r.Context(), actor(r), r.URL.Query().Get("status"), intParam(r, "limit"), intParam(r, "offset"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	o, err := h.svc.Get(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Offer{"offer": o})
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	var in AnswerInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	if err := h.svc.Answer(r.Context(), actor(r), id, in); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.Cancel(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sent(w http.ResponseWriter, r *http.Request) {
	f := SentFilter{Status: r.URL.Query().Get("status"), Limit: intParam(r, "limit"), Offset: intParam(r, "offset")}
	if raw := r.URL.Query().Get("vacancy"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			h.fail(w, r, &auth.ValidationError{Fields: map[string]string{"vacancy": "Неверный номер вакансии"}})
			return
		}
		f.VacancyID = &id
	}
	res, err := h.svc.Sent(r.Context(), actor(r), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) targets(w http.ResponseWriter, r *http.Request) {
	id, ok := param(r, "id")
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	items, err := h.svc.Targets(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
