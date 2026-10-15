package chat

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
)

// Коды ошибок раздела (contracts/chat/errors.json).
const (
	CodeChatClosed       = "chat_closed"
	CodeEditWindowClosed = "edit_window_closed"
	CodeSystemMessage    = "system_message"
	CodeNotAuthor        = "not_author"
	CodeAlreadyBlocked   = "already_blocked"
	CodeLimitReached     = "limit_reached"
	messageNoSuchChat    = "Такой переписки нет"
	messageNoSuchMessage = "Такого сообщения нет"
	messageCheckFields   = "Проверьте поля"
	messageChatClosed    = "Переписка закрыта, отправить сообщение нельзя"
	messageEditWindow    = "Сообщение можно исправить или удалить только в течение суток"
	messageSystemMessage = "Это сообщение нельзя изменить"
	messageNotAuthor     = "Изменить можно только своё сообщение"
	messageForbidden     = "Недостаточно прав"
	messageAlreadyBlock  = "Эта организация уже заблокирована"
	messageTemplateLimit = "У организации уже 50 шаблонов"
	messageNoTemplate    = "Такого шаблона нет"
)

// Handler — HTTP-часть чата.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
	// hub — живой поток (срез 39); nil — адрес потока отвечает 404.
	hub      *Hub
	onStream func(context.Context)
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireUser).Get("/chats", h.list)
	r.With(h.requireUser).Get("/chats/events", h.events)
	r.With(h.requireUser).Get("/chats/unread-count", h.unreadCount)
	r.With(h.requireUser).Get("/chats/vacancies", h.vacancies)
	r.With(h.requireUser).Get("/chats/{id}", h.get)
	r.With(h.requireUser).Get("/chats/{id}/messages", h.messages)
	r.With(h.requireUser).Post("/chats/{id}/messages", h.send)
	r.With(h.requireUser).Patch("/chats/{id}/messages/{seq}", h.edit)
	r.With(h.requireUser).Delete("/chats/{id}/messages/{seq}", h.remove)
	r.With(h.requireUser).Post("/chats/{id}/read", h.read)
	r.With(h.requireUser).Post("/chats/{id}/archive", h.archive)
	r.With(h.requireUser).Post("/chats/{id}/unarchive", h.unarchive)
	r.With(h.requireUser).Post("/chats/{id}/typing", h.typing)
	r.With(h.requireUser).Post("/chats/{id}/block", h.block)
	r.With(h.requireUser).Post("/chats/{id}/complaints", h.complain)
	r.With(h.requireUser).Get("/chat-blocks", h.blocks)
	r.With(h.requireUser).Delete("/chat-blocks/{orgId}", h.unblock)
	r.With(h.requireUser).Get("/orgs/{orgId}/chat-templates", h.templates)
	r.With(h.requireUser).Post("/orgs/{orgId}/chat-templates", h.createTemplate)
	r.With(h.requireUser).Patch("/orgs/{orgId}/chat-templates/{id}", h.updateTemplate)
	r.With(h.requireUser).Delete("/orgs/{orgId}/chat-templates/{id}", h.deleteTemplate)
}

func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

// chatID разбирает номер переписки из адреса; негодный номер даёт тот же 404, что и чужая переписка.
func chatID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	return id, err == nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr *auth.ValidationError
		perr *BadParamError
	)
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, messageCheckFields, verr.Fields)
	case errors.As(err, &perr):
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeBadRequest, "Неверный параметр запроса: "+perr.Param)
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, messageNoSuchChat)
	case errors.Is(err, ErrMessageNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, messageNoSuchMessage)
	case errors.Is(err, ErrClosed):
		apierr.WriteError(w, http.StatusConflict, CodeChatClosed, messageChatClosed)
	case errors.Is(err, ErrEditWindowClosed):
		apierr.WriteError(w, http.StatusConflict, CodeEditWindowClosed, messageEditWindow)
	case errors.Is(err, ErrSystemMessage):
		apierr.WriteError(w, http.StatusConflict, CodeSystemMessage, messageSystemMessage)
	case errors.Is(err, ErrNotAuthor):
		apierr.WriteError(w, http.StatusForbidden, CodeNotAuthor, messageNotAuthor)
	case errors.Is(err, ErrForbidden):
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, messageForbidden)
	case errors.Is(err, ErrAlreadyBlocked):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyBlocked, messageAlreadyBlock)
	case errors.Is(err, ErrTemplateLimit):
		apierr.WriteError(w, http.StatusConflict, CodeLimitReached, messageTemplateLimit)
	case errors.Is(err, ErrTemplateNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, messageNoTemplate)
	default:
		apierr.WriteInternal(w, r, h.logger, "chat", err)
	}
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) { h.fail(w, r, ErrNotFound) }

// intParam читает необязательное целое из адреса; пусто — nil.
func intParam(r *http.Request, name string) (*int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, &BadParamError{Param: name}
	}
	return &n, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lq := ListQuery{Side: q.Get("side"), Box: q.Get("box"), Search: q.Get("q"), Cursor: q.Get("cursor")}
	if raw := q.Get("vacancy"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			h.fail(w, r, &BadParamError{Param: "vacancy"})
			return
		}
		lq.VacancyID = &id
	}
	limit, err := intParam(r, "limit")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if limit != nil {
		lq.Limit = *limit
		if lq.Limit == 0 { // «0» не значит «по умолчанию»: это неверное значение
			h.fail(w, r, &BadParamError{Param: "limit"})
			return
		}
	}
	res, err := h.svc.List(r.Context(), actor(r), lq)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.UnreadCount(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) vacancies(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Vacancies(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	c, err := h.svc.Get(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Chat{"chat": c})
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var mq MessagesQuery
	var err error
	if mq.Before, err = intParam(r, "before"); err != nil {
		h.fail(w, r, err)
		return
	}
	if mq.After, err = intParam(r, "after"); err != nil {
		h.fail(w, r, err)
		return
	}
	limit, err := intParam(r, "limit")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if limit != nil {
		mq.Limit = *limit
		if mq.Limit == 0 {
			h.fail(w, r, &BadParamError{Param: "limit"})
			return
		}
	}
	page, err := h.svc.Messages(r.Context(), actor(r), id, mq)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var in SendInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	m, created, err := h.svc.Send(r.Context(), actor(r), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	apierr.WriteJSON(w, status, map[string]Message{"message": m})
}

// messageSeq разбирает номер сообщения в адресе; негодный номер даёт 404 о сообщении.
func messageSeq(r *http.Request) (int, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, "seq"))
	return n, err == nil && n > 0
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var in EditInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	seq, ok := messageSeq(r)
	if !ok {
		h.messageMissing(w, r, id)
		return
	}
	m, err := h.svc.Edit(r.Context(), actor(r), id, seq, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Message{"message": m})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	seq, ok := messageSeq(r)
	if !ok {
		h.messageMissing(w, r, id)
		return
	}
	if err := h.svc.Delete(r.Context(), actor(r), id, seq); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// messageMissing отвечает на негодный номер сообщения: сначала права на переписку (чужая даёт её 404), потом «нет такого».
func (h *Handler) messageMissing(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := h.svc.Check(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.fail(w, r, ErrMessageNotFound)
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var in ReadInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	if in.UpTo == nil {
		h.fail(w, r, &BadParamError{Param: "up_to"})
		return
	}
	if err := h.svc.MarkRead(r.Context(), actor(r), id, *in.UpTo); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request)   { h.setArchived(w, r, true) }
func (h *Handler) unarchive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, false) }

func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if err := h.svc.Archive(r.Context(), actor(r), id, archived); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) typing(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if err := h.svc.Typing(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
