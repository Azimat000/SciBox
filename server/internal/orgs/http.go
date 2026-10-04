package orgs

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

// maxBody — самый большой допустимый размер тела запроса.
const maxBody = 64 << 10

// Коды ошибок этого раздела.
const (
	CodeLastOwner            = "last_owner"
	CodeAlreadyMember        = "already_member"
	CodeInvalidInvitation    = "invalid_invitation"
	CodeInvitationWrongEmail = "invitation_wrong_email"
	CodeUnitHasVacancies     = "unit_has_vacancies"
)

// Handler — HTTP-часть организаций.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth (auth.Handler.RequireUser).
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса организаций к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/organizations", func(r chi.Router) {
		r.Get("/", h.list)
		r.With(h.requireUser).Post("/", h.create)
		r.Route("/{slug}", func(r chi.Router) {
			r.Get("/", h.get)
			r.With(h.requireUser).Patch("/", h.update)
			r.Route("/units", func(r chi.Router) {
				r.With(h.requireUser).Post("/", h.createUnit)
				r.Route("/{unitID}", func(r chi.Router) {
					r.Get("/", h.getUnit)
					r.With(h.requireUser).Patch("/", h.updateUnit)
					r.With(h.requireUser).Delete("/", h.deleteUnit)
					r.With(h.requireUser).Put("/head", h.setHead)
				})
			})
			r.Group(func(r chi.Router) {
				r.Use(h.requireUser)
				r.Get("/members", h.members)
				r.Patch("/members/{userID}", h.changeRole)
				r.Delete("/members/{userID}", h.removeMember)
				r.Post("/invitations", h.invite)
				r.Delete("/invitations/{id}", h.revokeInvitation)
			})
		})
	})
	r.With(h.requireUser).Get("/my/organizations", h.mine)
	r.Post("/invitations/lookup", h.lookup)
	r.With(h.requireUser).Post("/invitations/accept", h.accept)
	r.With(h.requireUser).Post("/invitations/{id}/accept", h.acceptByID)
}

// viewer — вошедший человек или nil.
func viewer(r *http.Request) *auth.User {
	if p, ok := auth.FromContext(r.Context()); ok {
		return &p.User
	}
	return nil
}

// actor — вошедший человек; за requireUser он всегда есть.
func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

// pathUUID читает номер из адреса; ok == false, если это не номер (такой страницы нет).
func pathUUID(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	return id, err == nil
}

// fail превращает ошибку сервиса в ответ API.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr *auth.ValidationError
		rerr *auth.RateLimitedError
	)
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, "Проверьте поля формы", verr.Fields)
	case errors.As(err, &rerr):
		secs := int(math.Ceil(rerr.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
			Code: apierr.CodeRateLimited, Message: "Слишком много попыток. Подождите немного и попробуйте снова", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такой страницы нет")
	case errors.Is(err, ErrForbidden):
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "У вас нет прав на это действие")
	case errors.Is(err, ErrLastOwner):
		apierr.WriteError(w, http.StatusConflict, CodeLastOwner, "В организации должен остаться хотя бы один владелец")
	case errors.Is(err, ErrAlreadyMember):
		apierr.WriteError(w, http.StatusConflict, CodeAlreadyMember, "Вы уже работаете в этой организации")
	case errors.Is(err, ErrInvalidInvitation):
		apierr.WriteError(w, http.StatusBadRequest, CodeInvalidInvitation, "Приглашение устарело, отозвано или уже принято. Попросите отправить новое")
	case errors.Is(err, ErrUnitHasVacancies):
		apierr.WriteError(w, http.StatusConflict, CodeUnitHasVacancies, "В подразделении есть вакансии. Сначала перенесите их в другое подразделение или удалите")
	case errors.Is(err, ErrWrongEmail):
		apierr.WriteError(w, http.StatusForbidden, CodeInvitationWrongEmail, "Приглашение отправлено на другую почту. Войдите в аккаунт с той почтой, на которую пришло письмо")
	default:
		apierr.WriteInternal(w, r, h.logger, "organizations", err)
	}
}

func notFound(w http.ResponseWriter) {
	apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такой страницы нет")
}

// ---- каталог и страницы ----

func intParam(r *http.Request, name string) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.ListOrganizations(r.Context(), ListFilter{
		Query: r.URL.Query().Get("q"), Kind: r.URL.Query().Get("kind"), Limit: intParam(r, "limit"), Offset: intParam(r, "offset"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.GetOrganization(r.Context(), chi.URLParam(r, "slug"), viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) getUnit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "unitID")
	if !ok {
		notFound(w)
		return
	}
	res, err := h.svc.GetUnit(r.Context(), chi.URLParam(r, "slug"), id, viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

// ---- организации ----

type orgBody struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	City        string `json:"city"`
	Website     string `json:"website"`
	Description string `json:"description"`
}

func (b orgBody) input() OrgInput {
	return OrgInput{Name: b.Name, Kind: b.Kind, City: b.City, Website: b.Website, Description: b.Description}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in orgBody
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	org, err := h.svc.CreateOrganization(r.Context(), actor(r), in.input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Organization{"organization": org})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in orgBody
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	org, err := h.svc.UpdateOrganization(r.Context(), actor(r), chi.URLParam(r, "slug"), in.input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Organization{"organization": org})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.svc.MyOrganizations(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	invs, err := h.svc.MyInvitations(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"organizations": orgs, "invitations": invs})
}

// ---- подразделения ----

type unitBody struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
}

func (b unitBody) input() UnitInput {
	return UnitInput{Name: b.Name, Kind: b.Kind, Description: b.Description, Topics: b.Topics}
}

func (h *Handler) createUnit(w http.ResponseWriter, r *http.Request) {
	var in unitBody
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	unit, err := h.svc.CreateUnit(r.Context(), actor(r), chi.URLParam(r, "slug"), in.input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Unit{"unit": unit})
}

func (h *Handler) updateUnit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "unitID")
	if !ok {
		notFound(w)
		return
	}
	var in unitBody
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	unit, err := h.svc.UpdateUnit(r.Context(), actor(r), chi.URLParam(r, "slug"), id, in.input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Unit{"unit": unit})
}

func (h *Handler) deleteUnit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "unitID")
	if !ok {
		notFound(w)
		return
	}
	if err := h.svc.DeleteUnit(r.Context(), actor(r), chi.URLParam(r, "slug"), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setHead(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "unitID")
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		UserID *string `json:"user_id"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	var head *uuid.UUID
	if in.UserID != nil {
		u, err := uuid.Parse(*in.UserID)
		if err != nil {
			h.fail(w, r, &auth.ValidationError{Fields: map[string]string{"user_id": msgHeadNotMember}})
			return
		}
		head = &u
	}
	unit, err := h.svc.SetUnitHead(r.Context(), actor(r), chi.URLParam(r, "slug"), id, head)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Unit{"unit": unit})
}

// ---- сотрудники и приглашения ----

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Members(r.Context(), actor(r), chi.URLParam(r, "slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "userID")
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	if err := h.svc.ChangeRole(r.Context(), actor(r), chi.URLParam(r, "slug"), id, in.Role); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "userID")
	if !ok {
		notFound(w)
		return
	}
	if err := h.svc.RemoveMember(r.Context(), actor(r), chi.URLParam(r, "slug"), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email  string  `json:"email"`
		Role   string  `json:"role"`
		UnitID *string `json:"unit_id"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	input := InviteInput{Email: in.Email, Role: in.Role}
	if in.UnitID != nil {
		u, err := uuid.Parse(*in.UnitID)
		if err != nil {
			h.fail(w, r, &auth.ValidationError{Fields: map[string]string{"unit_id": msgUnitUnknown}})
			return
		}
		input.UnitID = &u
	}
	inv, err := h.svc.Invite(r.Context(), actor(r), chi.URLParam(r, "slug"), input)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]PendingInvitation{"invitation": inv})
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		notFound(w)
		return
	}
	if err := h.svc.RevokeInvitation(r.Context(), actor(r), chi.URLParam(r, "slug"), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	res, err := h.svc.LookupInvitation(r.Context(), in.Token, viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	joined, err := h.svc.AcceptInvitation(r.Context(), actor(r), in.Token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, joined)
}

func (h *Handler) acceptByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		notFound(w)
		return
	}
	var in struct{}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	joined, err := h.svc.AcceptInvitationByID(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, joined)
}
