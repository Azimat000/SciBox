package profiles

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
)

// maxBody — самый большой допустимый размер тела запроса (самое длинное поле, «о себе», до 3000 знаков по-русски).
const maxBody = 32 << 10

// Коды ошибок этого раздела.
const (
	CodeTooMany        = "too_many_items"
	CodeDOINotFound    = "doi_not_found"
	CodeDOIUnavailable = "doi_unavailable"
)

// Handler — HTTP-часть профилей.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth (auth.Handler.RequireUser).
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса профилей к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/profile", func(r chi.Router) {
		r.Use(h.requireUser)
		r.Get("/", h.own)
		r.Put("/", h.saveCore)
		r.Put("/privacy", h.setPrivacy)
		r.Post("/items", h.addItem)
		r.Put("/items/{id}", h.updateItem)
		r.Delete("/items/{id}", h.deleteItem)
		r.Get("/doi", h.lookupDOI)
		r.Get("/cv", h.ownCV)
	})
	r.Get("/scientists", h.catalog)
	r.Get("/scientists/{id}", h.get)
	r.With(h.requireUser).Get("/scientists/{id}/cv", h.cv)
}

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

func notFound(w http.ResponseWriter) {
	apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такого профиля нет")
}

// pathID читает номер из адреса; ok == false, если это не номер (такой страницы нет).
func pathID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
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
			Code: apierr.CodeRateLimited, Message: "Слишком много поисков по DOI за час. Подождите немного или заполните публикацию вручную", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		notFound(w)
	case errors.Is(err, ErrTooMany):
		apierr.WriteError(w, http.StatusConflict, CodeTooMany, "В этом разделе уже слишком много записей. Удалите ненужные, чтобы добавить новые")
	case errors.Is(err, ErrDOINotFound):
		apierr.WriteError(w, http.StatusNotFound, CodeDOINotFound, "Crossref не знает такого DOI. Проверьте номер или заполните публикацию вручную")
	case errors.Is(err, ErrDOIUnavailable):
		h.logger.Warn("crossref lookup failed", "err", err)
		apierr.WriteError(w, http.StatusBadGateway, CodeDOIUnavailable, "Crossref сейчас недоступен. Заполните публикацию вручную")
	default:
		apierr.WriteInternal(w, r, h.logger, "profiles", err)
	}
}

func (h *Handler) own(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.Own(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) saveCore(w http.ResponseWriter, r *http.Request) {
	var in CoreInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	page, err := h.svc.SaveCore(r.Context(), actor(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) setPrivacy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Visibility   string `json:"visibility"`
		OpenToOffers bool   `json:"open_to_offers"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	page, err := h.svc.SetPrivacy(r.Context(), actor(r), in.Visibility, in.OpenToOffers)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	var in ItemInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	item, err := h.svc.AddItem(r.Context(), actor(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Item{"item": item})
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	var in ItemInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	item, err := h.svc.UpdateItem(r.Context(), actor(r), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Item{"item": item})
}

func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	if err := h.svc.DeleteItem(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) lookupDOI(w http.ResponseWriter, r *http.Request) {
	work, err := h.svc.LookupDOI(r.Context(), actor(r), r.URL.Query().Get("doi"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"work": work})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	page, err := h.svc.Get(r.Context(), id, viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ownCV(w http.ResponseWriter, r *http.Request) {
	h.sendCV(w, r, nil)
}

func (h *Handler) cv(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	h.sendCV(w, r, &id)
}

func (h *Handler) sendCV(w http.ResponseWriter, r *http.Request, id *uuid.UUID) {
	pdf, name, err := h.svc.CV(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="cv.pdf"; filename*=UTF-8''`+url.PathEscape(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	_, _ = w.Write(pdf)
}

// catalogParams переводит строку запроса в параметры каталога. Ошибку формата (не число) отдаёт как ошибку поля;
// допустимость значений проверяет сам каталог.
func catalogParams(r *http.Request) (CatalogParams, error) {
	q := r.URL.Query()
	errs := map[string]string{}
	p := CatalogParams{
		Query: q.Get("q"), Fields: q["field"], Region: q.Get("region"), Degrees: q["degree"], Titles: q["title"],
		OpenOnly: q.Get("open") == "1" || q.Get("open") == "true", Sort: q.Get("sort"),
	}
	for name, dst := range map[string]*int{"h_min": &p.HMin, "q12_min": &p.Q12Min, "limit": &p.Limit, "offset": &p.Offset} {
		raw := q.Get(name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			errs[name] = "Нужно число: " + raw
			continue
		}
		*dst = n
	}
	if len(errs) > 0 {
		return CatalogParams{}, &auth.ValidationError{Fields: errs}
	}
	return p, nil
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	p, err := catalogParams(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.svc.Catalog(r.Context(), p, viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}
