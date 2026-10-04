// Package refdata отдаёт справочники: научные специальности ВАК, регионы, должности и источники этих данных (срез 5).
// Данные лежат в таблицах (миграция 00004) и меняются только миграциями.
package refdata

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
	"scibox/server/internal/dbgen"
)

// Specialty — научная специальность (1.1.1).
type Specialty struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Group — группа научных специальностей (1.1).
type Group struct {
	Code        string      `json:"code"`
	Name        string      `json:"name"`
	Specialties []Specialty `json:"specialties"`
}

// Field — область науки (1).
type Field struct {
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Groups []Group `json:"groups"`
}

// Region — субъект РФ.
type Region struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Position — должность и её тип (research, teaching, early_career, management).
type Position struct {
	Code string `json:"code"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// Source — откуда взяты данные справочника.
type Source struct {
	Catalog   string `json:"catalog"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Edition   string `json:"edition"`
	CheckedOn string `json:"checked_on"`
}

// Catalog — все справочники одним ответом.
type Catalog struct {
	Science   []Field    `json:"science"`
	Regions   []Region   `json:"regions"`
	Positions []Position `json:"positions"`
	Sources   []Source   `json:"sources"`
}

// Service читает справочники.
type Service struct{ q *dbgen.Queries }

// NewService собирает сервис.
func NewService(db dbgen.DBTX) *Service { return &Service{q: dbgen.New(db)} }

// Catalog читает все справочники.
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	fields, err := s.q.ListScienceFields(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: fields: %w", err)
	}
	groups, err := s.q.ListScienceGroups(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: groups: %w", err)
	}
	specs, err := s.q.ListSpecialties(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: specialties: %w", err)
	}
	regions, err := s.q.ListRegions(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: regions: %w", err)
	}
	positions, err := s.q.ListPositions(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: positions: %w", err)
	}
	sources, err := s.q.ListReferenceSources(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("refdata: sources: %w", err)
	}

	cat := Catalog{Science: make([]Field, 0, len(fields)), Regions: make([]Region, 0, len(regions)),
		Positions: make([]Position, 0, len(positions)), Sources: make([]Source, 0, len(sources))}
	fieldAt := map[string]int{}
	for _, f := range fields {
		fieldAt[f.Code] = len(cat.Science)
		cat.Science = append(cat.Science, Field{Code: f.Code, Name: f.Name, Groups: []Group{}})
	}
	type at struct{ field, group int }
	groupAt := map[string]at{}
	for _, g := range groups {
		fi := fieldAt[g.FieldCode]
		groupAt[g.Code] = at{fi, len(cat.Science[fi].Groups)}
		cat.Science[fi].Groups = append(cat.Science[fi].Groups, Group{Code: g.Code, Name: g.Name, Specialties: []Specialty{}})
	}
	for _, sp := range specs {
		p := groupAt[sp.GroupCode]
		grp := &cat.Science[p.field].Groups[p.group]
		grp.Specialties = append(grp.Specialties, Specialty{Code: sp.Code, Name: sp.Name})
	}
	for _, r := range regions {
		cat.Regions = append(cat.Regions, Region{Code: r.Code, Name: r.Name})
	}
	for _, p := range positions {
		cat.Positions = append(cat.Positions, Position{Code: p.Code, Type: p.PositionType, Name: p.Name})
	}
	for _, src := range sources {
		cat.Sources = append(cat.Sources, Source{Catalog: src.Catalog, Title: src.Title, URL: src.Url, Edition: src.Edition, CheckedOn: src.CheckedOn.Format("2006-01-02")})
	}
	return cat, nil
}

// Handler — HTTP-часть справочников.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler собирает обработчик.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Mount подключает GET /reference к роутеру, который уже живёт под /api.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/reference", func(w http.ResponseWriter, r *http.Request) {
		cat, err := h.svc.Catalog(r.Context())
		if err != nil {
			apierr.WriteInternal(w, r, h.logger, "reference", err)
			return
		}
		apierr.WriteJSON(w, http.StatusOK, cat)
	})
}
