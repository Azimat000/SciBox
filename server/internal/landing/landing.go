// Package landing считает числа для главной страницы (срез 12): сколько открытых вакансий, сколько организаций ищут людей,
// сколько учёных видит гость и сколько вакансий в каждой области науки и в каждом виде позиций. Это только счётчики,
// никаких записей и имён; запрос публичный.
package landing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/privacy"
)

// moscow — сроки подачи считаются по московскому дню (D-057).
var moscow = time.FixedZone("MSK", 3*60*60)

// FieldCount — открытые вакансии в одной области науки (код области: 1–5).
type FieldCount struct {
	Code      string `json:"code"`
	Vacancies int64  `json:"vacancies"`
}

// TypeCount — открытые вакансии одного вида позиции (research, teaching, admin, phd, masters, project, internship).
type TypeCount struct {
	Type      string `json:"type"`
	Vacancies int64  `json:"vacancies"`
}

// Stats — всё, что показывает главная.
type Stats struct {
	// OpenVacancies — опубликованные вакансии, срок подачи которых не прошёл: столько же находит поиск без условий.
	OpenVacancies int64 `json:"open_vacancies"`
	// HiringOrganizations — организации хотя бы с одной такой вакансией.
	HiringOrganizations int64 `json:"hiring_organizations"`
	// Scientists — профили, которые гость видит в каталоге учёных (D-097).
	Scientists int64        `json:"scientists"`
	Fields     []FieldCount `json:"fields"`
	Types      []TypeCount  `json:"types"`
}

// Service считает числа.
type Service struct {
	q   *dbgen.Queries
	now func() time.Time
}

// NewService собирает сервис.
func NewService(db dbgen.DBTX) *Service { return &Service{q: dbgen.New(db), now: time.Now} }

// Stats читает числа на сегодняшний день по Москве.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	n := s.now().In(moscow)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)

	// Гость видит в каталоге ровно то, что решает приватность для него (CatalogModes), поэтому и считаем эти режимы.
	var modes []string
	for _, m := range privacy.CatalogModes(privacy.Viewer{}) {
		modes = append(modes, string(m))
	}

	totals, err := s.q.LandingCounts(ctx, dbgen.LandingCountsParams{Today: today, Modes: modes})
	if err != nil {
		return Stats{}, fmt.Errorf("landing: counts: %w", err)
	}
	fields, err := s.q.LandingVacanciesByField(ctx, today)
	if err != nil {
		return Stats{}, fmt.Errorf("landing: by field: %w", err)
	}
	types, err := s.q.LandingVacanciesByType(ctx, today)
	if err != nil {
		return Stats{}, fmt.Errorf("landing: by type: %w", err)
	}

	out := Stats{
		OpenVacancies:       totals.OpenVacancies,
		HiringOrganizations: totals.HiringOrganizations,
		Scientists:          totals.Scientists,
		Fields:              make([]FieldCount, 0, len(fields)),
		Types:               make([]TypeCount, 0, len(types)),
	}
	for _, f := range fields {
		out.Fields = append(out.Fields, FieldCount{Code: f.FieldCode, Vacancies: f.Vacancies})
	}
	for _, t := range types {
		out.Types = append(out.Types, TypeCount{Type: t.PositionType, Vacancies: t.Vacancies})
	}
	return out, nil
}

// Handler — HTTP-часть.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler собирает обработчик.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Mount подключает GET /landing к роутеру, который уже живёт под /api.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/landing", func(w http.ResponseWriter, r *http.Request) {
		stats, err := h.svc.Stats(r.Context())
		if err != nil {
			apierr.WriteInternal(w, r, h.logger, "landing", err)
			return
		}
		apierr.WriteJSON(w, http.StatusOK, stats)
	})
}
