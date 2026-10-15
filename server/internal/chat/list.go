package chat

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// Пределы списка.
const (
	defaultList = 30
	maxList     = 50
)

// cursor — ключ листания: время последнего сообщения и номер переписки (оба определяют место однозначно).
func encodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixMicro(), 10) + ":" + id.String()))
}

func decodeCursor(s string) (time.Time, uuid.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	micros, rest, ok := strings.Cut(string(raw), ":")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	n, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return time.UnixMicro(n).UTC(), id, true
}

// escapeLike — знаки, которые в LIKE значат «любое», становятся обычными буквами.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// branch — одна ветка отбора переписок стороны организации: организации целиком или подразделения. Запросы к базе идут
// по ветке отдельно (у каждой свой индекс), ответы сливаются; у стороны учёного ветка одна и пустая.
type branch struct{ whole, units []uuid.UUID }

// branches раскладывает права человека по веткам; у человека без прав на отклики веток нет.
func (s *Service) branches(ctx context.Context, user auth.User, side Side) ([]branch, error) {
	if side == SideScientist {
		return []branch{{}}, nil
	}
	sc, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ViewApplications)
	if err != nil {
		return nil, err
	}
	var out []branch
	if len(sc.WholeOrgs) > 0 {
		out = append(out, branch{whole: sc.WholeOrgs})
	}
	if len(sc.Units) > 0 {
		out = append(out, branch{units: sc.Units})
	}
	return out, nil
}

// counts складывает числа вкладок по веткам.
func (s *Service) counts(ctx context.Context, user auth.User, side Side, bs []branch) (Counts, error) {
	var out Counts
	for _, b := range bs {
		c, err := s.q.CountChatBoxes(ctx, dbgen.CountChatBoxesParams{Side: string(side), ViewerID: user.ID, WholeOrgs: b.whole, Units: b.units})
		if err != nil {
			return Counts{}, fmt.Errorf("chat: count boxes: %w", err)
		}
		out.Unread += int(c.Unread)
		out.Archive += int(c.Archive)
	}
	return out, nil
}

// List возвращает страницу списка переписок одной стороны: новые сверху, листание по ключу.
func (s *Service) List(ctx context.Context, user auth.User, q ListQuery) (List, error) {
	side := Side(q.Side)
	if !side.Valid() {
		return List{}, &BadParamError{Param: "side"}
	}
	box := q.Box
	if box == "" {
		box = BoxAll
	}
	if box != BoxAll && box != BoxUnread && box != BoxArchive {
		return List{}, &BadParamError{Param: "box"}
	}
	if utf8.RuneCountInString(q.Search) > maxSearch {
		return List{}, &BadParamError{Param: "q"}
	}
	if q.VacancyID != nil && side != SideOrg {
		return List{}, &BadParamError{Param: "vacancy"}
	}
	limit := q.Limit
	if limit == 0 {
		limit = defaultList
	}
	if limit < 1 || limit > maxList {
		return List{}, &BadParamError{Param: "limit"}
	}
	var curAt *time.Time
	var curID *uuid.UUID
	if q.Cursor != "" {
		at, id, ok := decodeCursor(q.Cursor)
		if !ok {
			return List{}, &BadParamError{Param: "cursor"}
		}
		curAt, curID = &at, &id
	}
	bs, err := s.branches(ctx, user, side)
	if err != nil {
		return List{}, err
	}
	out := List{Items: []ListItem{}}
	if out.Counts, err = s.counts(ctx, user, side, bs); err != nil {
		return List{}, err
	}
	// Каждая ветка отдаёт свою первую страницу с запасом в одну строку; из слияния берутся первые limit+1.
	var rows []dbgen.ListChatsRow
	for _, b := range bs {
		got, err := s.q.ListChats(ctx, dbgen.ListChatsParams{
			Side: string(side), ViewerID: user.ID, WholeOrgs: b.whole, Units: b.units, Box: box,
			VacancyID: q.VacancyID, Search: escapeLike(strings.TrimSpace(q.Search)), CursorAt: curAt, CursorID: curID,
			RowLimit: int32(limit + 1),
		})
		if err != nil {
			return List{}, fmt.Errorf("chat: list: %w", err)
		}
		rows = append(rows, got...)
	}
	if len(bs) > 1 {
		slices.SortFunc(rows, func(a, b dbgen.ListChatsRow) int {
			if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
				return c
			}
			return strings.Compare(b.ID.String(), a.ID.String())
		})
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	for _, r := range rows {
		out.Items = append(out.Items, listItem(side, user.ID, r))
	}
	if more {
		last := rows[len(rows)-1]
		c := encodeCursor(last.UpdatedAt, last.ID)
		out.NextCursor = &c
	}
	return out, nil
}

func listItem(side Side, viewer uuid.UUID, r dbgen.ListChatsRow) ListItem {
	st := StateOf(r.ApplicationStatus, r.OfferStatus)
	seen := side == SideScientist || nameSeen(r.Visibility, r.HiddenFrom, r.Revealed)
	item := ListItem{
		ID: r.ID, Side: side,
		Vacancy:      VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, OrgName: r.OrgName, OrgSlug: r.OrgSlug, UnitName: deref(r.UnitName)},
		Counterpart:  counterpartOf(side, r.OrgName, r.ScientistName, r.ProfileID, r.Visibility, r.HiddenFrom, r.Revealed),
		Status:       Status{Source: st.Source, Value: st.Value},
		NeedsAnswer:  needsAnswer(side, st, r.HasPendingInvitation, r.OfferStatus),
		Unread:       int(r.Unread),
		Archived:     r.Archived,
		CanWrite:     !st.Closed,
		ClosedReason: reasonPtr(st),
		UpdatedAt:    r.UpdatedAt.UTC(),
	}
	if r.LastKind == nil {
		return item
	}
	// Время и вид последнего сообщения приходят из одной строки: есть вид, есть и время.
	last := &LastMessage{Seq: int(r.LastSeq), Kind: *r.LastKind, Deleted: r.LastDeletedAt != nil, CreatedAt: r.LastCreatedAt.UTC()}
	if *r.LastKind == KindSystem {
		last.EventType = r.LastEventType
		item.LastMessage = last
		return item
	}
	staff := map[uuid.UUID]authorInfo{}
	if r.LastAuthorID != nil {
		staff[*r.LastAuthorID] = authorInfo{name: deref(r.LastAuthorName), member: r.LastAuthorMember}
	}
	last.Author = authorOf(viewer, Side(deref(r.LastAuthorSide)), r.LastAuthorID, r.ScientistName, seen, staff)
	if !last.Deleted && r.LastBody != nil {
		last.Text = *r.LastBody
	}
	item.LastMessage = last
	return item
}

// UnreadCount считает переписки с непрочитанным вне архива у каждой стороны, где человек участвует.
func (s *Service) UnreadCount(ctx context.Context, user auth.User) (UnreadCount, error) {
	var out UnreadCount
	for _, side := range []Side{SideScientist, SideOrg} {
		bs, err := s.branches(ctx, user, side)
		if err != nil {
			return UnreadCount{}, err
		}
		c, err := s.counts(ctx, user, side, bs)
		if err != nil {
			return UnreadCount{}, err
		}
		if side == SideScientist {
			out.Scientist = c.Unread
		} else {
			out.Org = c.Unread
		}
	}
	out.Total = out.Scientist + out.Org
	return out, nil
}

// Vacancies — вакансии, по которым у организации есть переписки, для отбора в списке. Только те, отклики которых человек
// вправе вести сейчас.
func (s *Service) Vacancies(ctx context.Context, user auth.User) ([]VacancyOption, error) {
	bs, err := s.branches(ctx, user, SideOrg)
	if err != nil {
		return nil, err
	}
	out := []VacancyOption{}
	for _, b := range bs {
		rows, err := s.q.ListChatVacancies(ctx, dbgen.ListChatVacanciesParams{ViewerID: user.ID, WholeOrgs: b.whole, Units: b.units})
		if err != nil {
			return nil, fmt.Errorf("chat: list vacancies: %w", err)
		}
		for _, r := range rows {
			out = append(out, VacancyOption{ID: r.ID, Title: r.Title, OrgName: r.OrgName, UnitName: r.UnitName, Chats: int(r.Chats)})
		}
	}
	if len(bs) > 1 {
		slices.SortFunc(out, func(a, b VacancyOption) int {
			if c := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
				return c
			}
			return strings.Compare(a.ID.String(), b.ID.String())
		})
	}
	return out, nil
}
