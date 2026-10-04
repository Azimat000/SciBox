package matching

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

// maxBody — запрос с названием и строкой условий поиска короткий.
const maxBody = 16 << 10

// Коды ошибок этого раздела.
const (
	CodeTooManyFavorites = "too_many_favorites"
	CodeTooManySearches  = "too_many_searches"
)

// Handler — HTTP-часть раздела.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth.
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса к роутеру, который уже живёт под /api и прошёл Authenticate. Всё только для вошедшего и только про него.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(h.requireUser)
		r.Get("/favorites", h.favorites)
		r.Get("/favorites/ids", h.favoriteIDs)
		r.Put("/favorites/{id}", h.addFavorite)
		r.Delete("/favorites/{id}", h.removeFavorite)
		r.Get("/saved-searches", h.searches)
		r.Post("/saved-searches", h.createSearch)
		r.Get("/saved-searches/{id}", h.search)
		r.Patch("/saved-searches/{id}", h.updateSearch)
		r.Delete("/saved-searches/{id}", h.deleteSearch)
		r.Get("/matches", h.matches)
		r.Get("/deadlines", h.deadlines)
	})
}

func actor(r *http.Request) auth.User {
	p, _ := auth.FromContext(r.Context())
	return p.User
}

func idParam(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	return id, err == nil
}

func intParam(r *http.Request, name string) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		apierr.WriteFieldErrors(w, "Проверьте поиск", verr.Fields)
	case errors.Is(err, ErrNotFound):
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такой вакансии или такого поиска нет")
	case errors.Is(err, ErrTooManyFavorites):
		apierr.WriteError(w, http.StatusConflict, CodeTooManyFavorites, "В избранном уже слишком много вакансий. Уберите ненужные, чтобы добавить новые")
	case errors.Is(err, ErrTooManySearches):
		apierr.WriteError(w, http.StatusConflict, CodeTooManySearches, "Сохранено слишком много поисков. Удалите ненужные, чтобы добавить новый")
	default:
		apierr.WriteInternal(w, r, h.logger, "matching", err)
	}
}

func (h *Handler) favorites(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Favorites(r.Context(), actor(r), intParam(r, "limit"), intParam(r, "offset"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) favoriteIDs(w http.ResponseWriter, r *http.Request) {
	ids, err := h.svc.FavoriteIDs(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string][]uuid.UUID{"ids": ids})
}

func (h *Handler) addFavorite(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.AddFavorite(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeFavorite(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.RemoveFavorite(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) searches(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Searches(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string][]SavedSearch{"items": items})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	res, err := h.svc.Search(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]SavedSearch{"search": res})
}

func (h *Handler) createSearch(w http.ResponseWriter, r *http.Request) {
	var in SearchInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	res, err := h.svc.CreateSearch(r.Context(), actor(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]SavedSearch{"search": res})
}

func (h *Handler) updateSearch(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	var in SearchInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	res, err := h.svc.UpdateSearch(r.Context(), actor(r), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]SavedSearch{"search": res})
}

func (h *Handler) deleteSearch(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		h.fail(w, r, ErrNotFound)
		return
	}
	if err := h.svc.DeleteSearch(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) matches(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Matches(r.Context(), actor(r), intParam(r, "limit"), intParam(r, "offset"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deadlines(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Deadlines(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, out)
}
