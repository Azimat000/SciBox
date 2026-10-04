package notifications

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
)

// Handler — HTTP-часть уведомлений: колокольчик и страница «Уведомления».
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
	r.Route("/notifications", func(r chi.Router) {
		r.Use(h.requireUser)
		r.Get("/", h.list)
		r.Get("/unread-count", h.unreadCount)
		r.Post("/read-all", h.readAll)
		r.Post("/{id}/read", h.read)
	})
	r.With(h.requireUser).Get("/notification-settings", h.settings)
	r.With(h.requireUser).Put("/notification-settings", h.saveSettings)
}

func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такого уведомления нет")
		return
	}
	apierr.WriteInternal(w, r, h.logger, "notifications", err)
}

// intParam читает целое из адреса; пусто или не число — def.
func intParam(r *http.Request, name string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return n
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.List(r.Context(), actor(r), intParam(r, "limit", DefaultLimit), intParam(r, "offset", 0), r.URL.Query().Get("unread") == "1")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.UnreadCount(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]int64{"unread": n})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.MarkRead(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MarkAllRead(r.Context(), actor(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxBody — запрос с двумя переключателями короткий.
const maxBody = 1 << 10

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Settings(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EmailNewVacancies *bool `json:"email_new_vacancies"`
		EmailDeadlines    *bool `json:"email_deadlines"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	if in.EmailNewVacancies == nil || in.EmailDeadlines == nil {
		fields := map[string]string{}
		if in.EmailNewVacancies == nil {
			fields["email_new_vacancies"] = "Нужно «да» или «нет»"
		}
		if in.EmailDeadlines == nil {
			fields["email_deadlines"] = "Нужно «да» или «нет»"
		}
		apierr.WriteFieldErrors(w, "Проверьте настройки", fields)
		return
	}
	out, err := h.svc.SaveSettings(r.Context(), actor(r), Settings{EmailNewVacancies: *in.EmailNewVacancies, EmailDeadlines: *in.EmailDeadlines})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}
