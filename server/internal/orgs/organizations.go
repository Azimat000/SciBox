package orgs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// Organization — организация целиком (публичная страница).
type Organization struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	City        string    `json:"city"`
	Website     string    `json:"website"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func organizationFrom(o dbgen.Organization) Organization {
	return Organization{ID: o.ID, Slug: o.Slug, Name: o.Name, Kind: o.Kind, City: o.City, Website: o.Website, Description: o.Description, CreatedAt: o.CreatedAt}
}

// Unit — подразделение. HeadUserID виден только сотрудникам организации (в публичном ответе его нет).
type Unit struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	Topics      []string   `json:"topics"`
	HeadName    *string    `json:"head_name"`
	HeadUserID  *uuid.UUID `json:"head_user_id,omitempty"`
}

func unitFrom(row dbgen.ListUnitsRow, showHeadID bool) Unit {
	u := Unit{ID: row.ID, Name: row.Name, Kind: row.Kind, Description: row.Description, Topics: row.Topics, HeadName: row.HeadName}
	if showHeadID {
		u.HeadUserID = row.HeadUserID
	}
	return u
}

// OrgView — страница организации.
type OrgView struct {
	Organization Organization `json:"organization"`
	Units        []Unit       `json:"units"`
	Viewer       *Viewer      `json:"viewer"`
}

// GetOrganization собирает публичную страницу организации. viewer — вошедший человек или nil.
func (s *Service) GetOrganization(ctx context.Context, slug string, viewer *auth.User) (OrgView, error) {
	org, err := s.loadOrg(ctx, s.q, slug)
	if err != nil {
		return OrgView{}, err
	}
	rows, err := s.q.ListUnits(ctx, org.ID)
	if err != nil {
		return OrgView{}, fmt.Errorf("orgs: list units: %w", err)
	}
	v, err := s.viewerOf(ctx, s.q, org.ID, viewer, rows)
	if err != nil {
		return OrgView{}, err
	}
	units := make([]Unit, 0, len(rows))
	for _, r := range rows {
		units = append(units, unitFrom(r, v != nil && v.Role != ""))
	}
	return OrgView{Organization: organizationFrom(org), Units: units, Viewer: v}, nil
}

// OrgSummary — карточка организации в каталоге.
type OrgSummary struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	City      string    `json:"city"`
	Summary   string    `json:"summary"`
	UnitCount int       `json:"unit_count"`
}

// OrgList — страница каталога.
type OrgList struct {
	Items []OrgSummary `json:"items"`
	Total int          `json:"total"`
}

// ListFilter — поиск по каталогу. Query ищется в названии и в городе без учёта регистра; пустой Kind — любой тип.
type ListFilter struct {
	Query  string
	Kind   string
	Limit  int
	Offset int
}

// Размер страницы каталога.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
	summaryRunes    = 240
)

// likePattern делает из слов человека шаблон для ILIKE, экранируя служебные знаки (% _ \).
func likePattern(q string) string {
	q = collapse(q)
	if q == "" {
		return ""
	}
	if utf8.RuneCountInString(q) > maxQueryLen {
		q = string([]rune(q)[:maxQueryLen])
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// summaryOf берёт начало описания для карточки.
func summaryOf(desc string) string {
	desc = collapse(desc)
	if utf8.RuneCountInString(desc) <= summaryRunes {
		return desc
	}
	return strings.TrimSpace(string([]rune(desc)[:summaryRunes])) + "…"
}

// ListOrganizations отдаёт страницу каталога. Неизвестный Kind даёт пустой результат, а не ошибку.
func (s *Service) ListOrganizations(ctx context.Context, f ListFilter) (OrgList, error) {
	if f.Limit <= 0 {
		f.Limit = DefaultPageSize
	}
	f.Limit = min(f.Limit, MaxPageSize)
	f.Offset = max(f.Offset, 0)
	pattern := likePattern(f.Query)
	total, err := s.q.CountOrganizations(ctx, dbgen.CountOrganizationsParams{Kind: f.Kind, Pattern: pattern})
	if err != nil {
		return OrgList{}, fmt.Errorf("orgs: count organizations: %w", err)
	}
	rows, err := s.q.ListOrganizations(ctx, dbgen.ListOrganizationsParams{Kind: f.Kind, Pattern: pattern, RowLimit: int32(f.Limit), RowOffset: int32(f.Offset)})
	if err != nil {
		return OrgList{}, fmt.Errorf("orgs: list organizations: %w", err)
	}
	out := OrgList{Items: make([]OrgSummary, 0, len(rows)), Total: int(total)}
	for _, r := range rows {
		out.Items = append(out.Items, OrgSummary{ID: r.ID, Slug: r.Slug, Name: r.Name, Kind: r.Kind, City: r.City, Summary: summaryOf(r.Description), UnitCount: int(r.UnitCount)})
	}
	return out, nil
}

// CreateOrganization заводит организацию; создатель становится владельцем.
func (s *Service) CreateOrganization(ctx context.Context, user auth.User, in OrgInput) (Organization, error) {
	f, err := validateOrg(in)
	if err != nil {
		return Organization{}, err
	}
	var created dbgen.Organization
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := s.spend(ctx, q, kindOrgCreate, user.ID, s.cfg.Create); err != nil {
			return err
		}
		now := s.now()
		base := slugify(f.name)
		for n := 1; ; n++ {
			slug := slugCandidate(base, n)
			if n > maxSlugTries {
				slug = base + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
			}
			row, err := q.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
				Slug: slug, Name: f.name, Kind: f.kind, City: f.city, Website: f.website, Description: f.description,
				CreatedBy: &user.ID, CreatedAt: now,
			})
			if err == nil {
				created = row
				break
			}
			if !errors.Is(err, errNoRow) {
				return fmt.Errorf("orgs: create organization: %w", err)
			}
		}
		if err := q.AddMember(ctx, dbgen.AddMemberParams{OrgID: created.ID, UserID: user.ID, Role: string(access.RoleOwner), JoinedAt: now}); err != nil {
			return fmt.Errorf("orgs: add owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return Organization{}, err
	}
	return organizationFrom(created), nil
}

// UpdateOrganization меняет данные организации (владелец). Адрес страницы не меняется.
func (s *Service) UpdateOrganization(ctx context.Context, user auth.User, slug string, in OrgInput) (Organization, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.EditOrganization, access.NoUnit)
	if err != nil {
		return Organization{}, err
	}
	f, err := validateOrg(in)
	if err != nil {
		return Organization{}, err
	}
	row, err := s.q.UpdateOrganization(ctx, dbgen.UpdateOrganizationParams{
		ID: org.ID, Name: f.name, Kind: f.kind, City: f.city, Website: f.website, Description: f.description, UpdatedAt: s.now(),
	})
	if err != nil {
		return Organization{}, fmt.Errorf("orgs: update organization: %w", err)
	}
	return organizationFrom(row), nil
}

// MyOrganization — организация человека и его роль в ней.
type MyOrganization struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
	Kind string    `json:"kind"`
	City string    `json:"city"`
	Role string    `json:"role"`
}

// MyOrganizations — организации, где человек сотрудник.
func (s *Service) MyOrganizations(ctx context.Context, user auth.User) ([]MyOrganization, error) {
	rows, err := s.q.ListOrganizationsOfUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("orgs: list my organizations: %w", err)
	}
	out := make([]MyOrganization, 0, len(rows))
	for _, r := range rows {
		out = append(out, MyOrganization{ID: r.ID, Slug: r.Slug, Name: r.Name, Kind: r.Kind, City: r.City, Role: r.Role})
	}
	return out, nil
}
