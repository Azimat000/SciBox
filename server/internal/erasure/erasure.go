// Package erasure — что происходит с данными человека, когда он удаляет аккаунт (срез 20, T-11, D-142, D-143).
//
// Критичная зона (docs/TESTING.md): ошибка здесь оставляет персональные данные после удаления или стирает чужие.
// Пакет ничего не решает о сроках и письмах (это auth): он проверяет организации и стирает данные внутри
// транзакции, которую ему передали.
package erasure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Org — организация, где человек единственный владелец.
type Org struct {
	ID   uuid.UUID
	Slug string
	Name string
}

// Plan — что будет с организациями человека.
// Blocking: он единственный владелец, а другие сотрудники есть — сначала нужно назначить владельца.
// Orphaned: он там единственный сотрудник — организация останется без владельца, опубликованные вакансии закроются.
type Plan struct {
	Blocking []Org
	Orphaned []Org
}

// Blocked — удалять аккаунт пока нельзя.
func (p Plan) Blocked() bool { return len(p.Blocking) > 0 }

// BlockedError — удаление невозможно: в этих организациях человек единственный владелец, а сотрудники есть.
type BlockedError struct{ Orgs []Org }

func (e *BlockedError) Error() string {
	names := make([]string, len(e.Orgs))
	for i, o := range e.Orgs {
		names[i] = o.Slug
	}
	return "erasure: sole owner of " + strings.Join(names, ", ")
}

// Check смотрит организации, где человек единственный владелец.
func Check(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) (Plan, error) {
	rows, err := q.SoleOwnedOrganizations(ctx, userID)
	if err != nil {
		return Plan{}, fmt.Errorf("erasure: sole owned organizations: %w", err)
	}
	plan := Plan{Blocking: []Org{}, Orphaned: []Org{}}
	for _, r := range rows {
		o := Org{ID: r.ID, Slug: r.Slug, Name: r.Name}
		if r.OtherMembers > 0 {
			plan.Blocking = append(plan.Blocking, o)
		} else {
			plan.Orphaned = append(plan.Orphaned, o)
		}
	}
	return plan, nil
}

// Erase стирает персональные данные человека и обезличивает его отклики. Вызывается в транзакции, где строка
// аккаунта уже заблокирована (dbgen.LockUserRow). user — строка аккаунта до стирания: её почта нужна,
// чтобы найти письма и приглашения на этот адрес.
//
// Что остаётся: строка аккаунта без почты, имени и пароля (на неё ссылаются обезличенные отклики и записи
// «кто создал вакансию, пригласил, принял решение») и отклики без письма, контактов, профиля и файлов.
// Если человек единственный владелец организации с другими сотрудниками, ничего не меняется: *BlockedError.
func Erase(ctx context.Context, q *dbgen.Queries, user dbgen.User, now time.Time) error {
	// Организации человека блокируются до проверки: приём приглашения идёт по той же строке, поэтому сотрудник не успеет
	// войти между проверкой «других сотрудников нет» и стиранием членства (T-34).
	if _, err := q.LockOrganizationsOwnedBy(ctx, user.ID); err != nil {
		return fmt.Errorf("erasure: lock organizations: %w", err)
	}
	plan, err := Check(ctx, q, user.ID)
	if err != nil {
		return err
	}
	if plan.Blocked() {
		return &BlockedError{Orgs: plan.Blocking}
	}
	for _, o := range plan.Orphaned {
		if err := q.CloseOrphanVacancies(ctx, dbgen.CloseOrphanVacanciesParams{OrgID: o.ID, Now: now}); err != nil {
			return fmt.Errorf("erasure: close vacancies of %s: %w", o.Slug, err)
		}
	}
	id := user.ID
	steps := []struct {
		name string
		run  func() error
	}{
		// Уведомления организаций об откликах ищутся по ссылке на отклик: до обезличивания.
		{"candidate notifications", func() error { return q.EraseCandidateNotifications(ctx, id) }},
		{"offer answer notifications", func() error { return q.EraseOfferAnswerNotifications(ctx, id) }},
		// Объекты файлов уходят в очередь на удаление из хранилища в этой же транзакции (T-21): сами строки стираются
		// следом, а фоновая задача filework удалит объекты, как только хранилище ответит.
		{"application file blobs", func() error {
			return q.QueueApplicationFileBlobs(ctx, dbgen.QueueApplicationFileBlobsParams{UserID: id, Now: now})
		}},
		{"application files", func() error { return q.EraseApplicationFiles(ctx, id) }},
		{"referee mail history", func() error { return q.EraseRefereeMailHistory(ctx, id) }},
		{"reference requests", func() error { return q.EraseReferenceRequests(ctx, id) }},
		{"application invitations", func() error { return q.EraseApplicationInvitations(ctx, id) }},
		{"anonymize applications", func() error {
			return q.AnonymizeApplications(ctx, dbgen.AnonymizeApplicationsParams{UserID: id, Now: now})
		}},
		{"offers", func() error { return q.EraseOffersTo(ctx, id) }},
		{"profile", func() error { return q.EraseProfile(ctx, id) }},
		{"favorites", func() error { return q.EraseFavorites(ctx, id) }},
		{"saved searches", func() error { return q.EraseSavedSearches(ctx, id) }},
		{"deadline reminders", func() error { return q.EraseDeadlineReminders(ctx, id) }},
		{"notification settings", func() error { return q.EraseNotificationSettings(ctx, id) }},
		{"notifications", func() error { return q.EraseNotifications(ctx, id) }},
		{"unit heads", func() error { return q.EraseUnitHeads(ctx, dbgen.EraseUnitHeadsParams{UserID: &id, Now: now}) }},
		{"memberships", func() error { return q.EraseMemberships(ctx, id) }},
		{"org invitations", func() error { return q.EraseOrgInvitationsTo(ctx, user.Email) }},
		{"sessions", func() error { return q.EraseSessions(ctx, id) }},
		{"auth tokens", func() error { return q.EraseAuthTokens(ctx, id) }},
		{"known devices", func() error { return q.EraseKnownDevices(ctx, id) }},
		{"outbox", func() error { return q.EraseOutboxTo(ctx, user.Email) }},
		// Записи журнала остаются (это история организации), адрес человека из них стирается.
		{"audit addresses", func() error { _, err := q.ScrubAuditIP(ctx, &id); return err }},
		{"rate events", func() error {
			return q.EraseRateEvents(ctx, dbgen.EraseRateEventsParams{Email: user.Email, UserKey: id.String()})
		}},
		{"account", func() error { return q.TombstoneUser(ctx, dbgen.TombstoneUserParams{ID: id, Now: now}) }},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			return fmt.Errorf("erasure: %s: %w", s.name, err)
		}
	}
	return nil
}
