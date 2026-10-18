package vacancies

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
	"scibox/server/internal/httpcache"
)

// maxBody — самый большой допустимый размер тела запроса (описание до 10 000 знаков по-русски занимает до 20 КБ).
const maxBody = 64 << 10

// Коды ошибок этого раздела.
const (
	CodeBadTransition = "invalid_status_change"
	CodeNotDraft      = "vacancy_not_draft"
	CodeChanged       = "vacancy_changed"
)

// Handler — HTTP-часть вакансий.
type Handler struct {
	svc         *Service
	logger      *slog.Logger
	requireUser func(http.Handler) http.Handler
	// searchCache — короткий кеш ответов поиска (срез 27); nil — без кеша.
	searchCache *httpcache.Keyed
}

// maxCachedSearchBody — ответ поиска больше этого размера не запоминается: страница из двадцати карточек укладывается
// в несколько десятков килобайт, а сто — уже ошибка данных, которую не нужно тиражировать.
const maxCachedSearchBody = 128 << 10

// WithSearchCache включает короткий кеш ответов поиска вакансий (срез 27, D-179). Результат поиска не зависит от того,
// кто спрашивает, поэтому ключ — сам запрос.
func (h *Handler) WithSearchCache(c *httpcache.Keyed) *Handler {
	h.searchCache = c
	return h
}

// NewHandler собирает обработчики. requireUser — проверка входа из пакета auth (auth.Handler.RequireUser).
func NewHandler(svc *Service, logger *slog.Logger, requireUser func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, logger: logger, requireUser: requireUser}
}

// Mount подключает адреса вакансий к роутеру, который уже живёт под /api и прошёл Authenticate.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/vacancies", h.list)
	r.With(h.requireUser).Post("/vacancies", h.create)
	r.Route("/vacancies/{id}", func(r chi.Router) {
		r.Get("/", h.get)
		r.With(h.requireUser).Patch("/", h.update)
		r.With(h.requireUser).Delete("/", h.delete)
		r.With(h.requireUser).Post("/status", h.setStatus)
	})
	r.With(h.requireUser).Get("/my/vacancies", h.mine)
	r.With(h.requireUser).Get("/my/vacancy-targets", h.targets)
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
	apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Такой вакансии нет")
}

// pathID читает номер вакансии из адреса; ok == false, если это не номер (такой страницы нет).
func pathID(r *http.Request) (uuid.UUID, bool) {
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
			Code: apierr.CodeRateLimited, Message: "Слишком много новых вакансий за сутки. Подождите немного и попробуйте снова", RetryAfter: secs,
		}})
	case errors.Is(err, ErrNotFound):
		notFound(w)
	case errors.Is(err, ErrForbidden):
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "У вас нет прав на это действие")
	case errors.Is(err, ErrBadTransition):
		apierr.WriteError(w, http.StatusConflict, CodeBadTransition, "Так менять статус вакансии нельзя")
	case errors.Is(err, ErrNotDraft):
		apierr.WriteError(w, http.StatusConflict, CodeNotDraft, "Удалить можно только черновик. Опубликованную вакансию закройте")
	case errors.Is(err, ErrChanged):
		apierr.WriteError(w, http.StatusConflict, CodeChanged, "Вакансию только что изменил кто-то другой. Обновите страницу")
	default:
		apierr.WriteInternal(w, r, h.logger, "vacancies", err)
	}
}

// body — поля формы вакансии в запросе.
type body struct {
	Organization   string   `json:"organization"` // только при создании
	Title          string   `json:"title"`
	PositionCode   string   `json:"position_code"`
	UnitID         *string  `json:"unit_id"`
	Summary        string   `json:"summary"`
	Description    string   `json:"description"`
	Requirements   string   `json:"requirements"`
	Focus          string   `json:"focus"`
	CareerLevel    *int     `json:"career_level"`
	WorkFormat     string   `json:"work_format"`
	RegionCode     string   `json:"region_code"`
	City           string   `json:"city"`
	Housing        string   `json:"housing"`
	RatePercent    *int     `json:"rate_percent"`
	SalaryFrom     *int     `json:"salary_from"`
	SalaryTo       *int     `json:"salary_to"`
	ContractType   string   `json:"contract_type"`
	ContractMonths *int     `json:"contract_months"`
	FundingSource  string   `json:"funding_source"`
	FundingNote    string   `json:"funding_note"`
	Degree         string   `json:"degree_required"`
	AcademicTitle  string   `json:"title_required"`
	IsCompetition  bool     `json:"is_competition"`
	Deadline       string   `json:"deadline"`
	Specialties    []string `json:"specialties"`

	EquipmentAccess  bool    `json:"equipment_access"`
	EquipmentNote    string  `json:"equipment_note"`
	EquipmentUnitID  *string `json:"equipment_unit_id"`
	PublicationsPaid bool    `json:"publications_paid"`
	ConferenceTravel bool    `json:"conference_travel"`
}

// input переводит запрос в поля вакансии. Если номер подразделения написан не как номер, это ошибка поля.
func (b body) input() (Input, error) {
	in := Input{
		Title: b.Title, PositionCode: b.PositionCode, Summary: b.Summary, Description: b.Description,
		Requirements: b.Requirements, Focus: b.Focus, CareerLevel: b.CareerLevel, WorkFormat: b.WorkFormat,
		RegionCode: b.RegionCode, City: b.City, Housing: b.Housing, RatePercent: b.RatePercent, SalaryFrom: b.SalaryFrom,
		SalaryTo: b.SalaryTo, ContractType: b.ContractType, ContractMonth: b.ContractMonths, FundingSource: b.FundingSource,
		FundingNote: b.FundingNote, Degree: b.Degree, AcademicTitle: b.AcademicTitle, IsCompetition: b.IsCompetition,
		Deadline: b.Deadline, Specialties: b.Specialties, EquipmentAccess: b.EquipmentAccess,
		EquipmentNote: b.EquipmentNote, PublicationsPaid: b.PublicationsPaid, ConferenceTravel: b.ConferenceTravel,
	}
	if b.UnitID != nil && *b.UnitID != "" {
		u, err := uuid.Parse(*b.UnitID)
		if err != nil {
			return Input{}, &auth.ValidationError{Fields: map[string]string{"unit_id": msgUnitUnknown}}
		}
		in.UnitID = &u
	}
	if b.EquipmentUnitID != nil && *b.EquipmentUnitID != "" {
		u, err := uuid.Parse(*b.EquipmentUnitID)
		if err != nil {
			return Input{}, &auth.ValidationError{Fields: map[string]string{"equipment_unit_id": msgEquipmentUnit}}
		}
		in.EquipmentUnitID = &u
	}
	return in, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var b body
	if !apierr.DecodeJSON(w, r, &b, maxBody) {
		return
	}
	in, err := b.input()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d, err := h.svc.Create(r.Context(), actor(r), b.Organization, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Detail{"vacancy": d})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	d, err := h.svc.Get(r.Context(), id, viewer(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Detail{"vacancy": d})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	var b body
	if !apierr.DecodeJSON(w, r, &b, maxBody) {
		return
	}
	in, err := b.input()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d, err := h.svc.Update(r.Context(), actor(r), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Detail{"vacancy": d})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	if err := h.svc.Delete(r.Context(), actor(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	d, err := h.svc.SetStatus(r.Context(), actor(r), id, in.Status)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Detail{"vacancy": d})
}

// searchParams переводит строку запроса в параметры поиска (разбор в ParseSearchQuery).
func searchParams(r *http.Request) (SearchParams, error) { return ParseSearchQuery(r.URL.Query()) }

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, err := searchParams(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	key, keyOK := cacheKey(p)
	if h.searchCache == nil || !keyOK {
		res, err := h.svc.Search(r.Context(), p)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		apierr.WriteJSON(w, http.StatusOK, res)
		return
	}
	// Ключ — разобранные условия, а не строка адреса: лишние параметры и запись «1»/«true» не дают новых записей кеша,
	// и перебором мусорных параметров кеш не обойти (срез 29, INJ-5).
	doc, err := h.searchCache.Get(r.Context(), key, maxCachedSearchBody, func(ctx context.Context) (any, error) {
		return h.svc.Search(ctx, p)
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Body) // клиент мог уйти: писать больше некуда
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.ListMine(r.Context(), actor(r), r.URL.Query().Get("status"), intParam(r, "limit"), intParam(r, "offset"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) targets(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Targets(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string][]Target{"targets": res})
}

// cacheKey — ключ кеша: условия поиска в одном определённом виде. Не записалось (не бывает) — без кеша.
func cacheKey(p SearchParams) (string, bool) {
	b, err := json.Marshal(p)
	return string(b), err == nil
}
