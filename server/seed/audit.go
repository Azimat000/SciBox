package seed

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
)

// seedAudit пишет в журнал демо-организаций историю, которая могла бы быть у живой организации: приглашения и вступление
// сотрудников, назначение руководителей, публикация и закрытие вакансий, просмотры и решения по откликам. Время у записей
// своё (в прошлом), порядок записи хронологический: журнал листается по номеру записи. Метка demo в подробностях нужна, чтобы
// повторный запуск не дублировал историю.
func seedAudit(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	var done int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE details ? 'demo'`).Scan(&done); err != nil {
		return 0, fmt.Errorf("seed audit: %w", err)
	}
	if done > 0 {
		return 0, nil
	}
	orgRows, err := pool.Query(ctx, `SELECT id, created_by FROM organizations ORDER BY name`)
	if err != nil {
		return 0, fmt.Errorf("seed audit: %w", err)
	}
	type org struct{ id, owner uuid.UUID }
	var list []org
	for orgRows.Next() {
		var o org
		var owner *uuid.UUID
		if err := orgRows.Scan(&o.id, &owner); err != nil {
			orgRows.Close()
			return 0, fmt.Errorf("seed audit: %w", err)
		}
		if owner != nil {
			o.owner = *owner
			list = append(list, o)
		}
	}
	orgRows.Close()

	var events []audit.Event
	for _, o := range list {
		evs, err := orgHistory(ctx, pool, o.id, o.owner, now)
		if err != nil {
			return 0, err
		}
		events = append(events, evs...)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("seed audit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	for _, e := range events {
		if e.Details == nil {
			e.Details = map[string]string{}
		}
		e.Details["demo"] = "1"
		if err := audit.Record(ctx, q, e); err != nil {
			return 0, fmt.Errorf("seed audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("seed audit: %w", err)
	}
	return len(events), nil
}

func clamp(t, now time.Time) time.Time {
	if t.After(now) {
		return now.Add(-time.Minute)
	}
	return t
}

// orgHistory собирает события одной организации по тому, что в ней уже есть в базе.
func orgHistory(ctx context.Context, pool *pgxpool.Pool, orgID, owner uuid.UUID, now time.Time) ([]audit.Event, error) {
	var evs []audit.Event
	day := func(n int, hour int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), hour, 7, 0, 0, time.UTC).AddDate(0, 0, -n)
	}

	// Сотрудники: приглашение владельца и вступление.
	members, err := pool.Query(ctx, `SELECT user_id, role FROM org_members WHERE org_id = $1 AND user_id <> $2 ORDER BY joined_at, user_id`, orgID, owner)
	if err != nil {
		return nil, fmt.Errorf("seed audit: members: %w", err)
	}
	type member struct {
		id   uuid.UUID
		role string
	}
	var ms []member
	for members.Next() {
		var m member
		if err := members.Scan(&m.id, &m.role); err != nil {
			members.Close()
			return nil, fmt.Errorf("seed audit: members: %w", err)
		}
		ms = append(ms, m)
	}
	members.Close()
	for i, m := range ms {
		at := day(40-i*3, 10)
		inv := uuid.New()
		evs = append(evs,
			audit.Event{Action: audit.InvitationSent, OrgID: orgID, Actor: owner, TargetKind: "invitation", TargetID: inv, At: at,
				Details: map[string]string{"role": m.role, "email": "***@demo.example.ru"}},
			audit.Event{Action: audit.MemberJoined, OrgID: orgID, Actor: m.id, TargetKind: "invitation", TargetID: inv, At: at.Add(3 * time.Hour),
				Details: map[string]string{"role": m.role}},
		)
	}

	// Руководители подразделений.
	units, err := pool.Query(ctx, `SELECT id, name, head_user_id FROM units WHERE org_id = $1 AND head_user_id IS NOT NULL ORDER BY name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("seed audit: units: %w", err)
	}
	for i := 0; units.Next(); i++ {
		var id, head uuid.UUID
		var name string
		if err := units.Scan(&id, &name, &head); err != nil {
			units.Close()
			return nil, fmt.Errorf("seed audit: units: %w", err)
		}
		evs = append(evs, audit.Event{Action: audit.UnitHeadChanged, OrgID: orgID, Actor: owner, Subject: head, TargetKind: "unit", TargetID: id, TargetLabel: name, At: day(36-i, 12)})
	}
	units.Close()

	// Вакансии: публикация и закрытие (автор — создатель вакансии).
	vacs, err := pool.Query(ctx, `SELECT id, title, status, created_by, published_at, closed_at FROM vacancies
		WHERE org_id = $1 AND status IN ('published', 'closed') ORDER BY created_at, id LIMIT 12`, orgID)
	if err != nil {
		return nil, fmt.Errorf("seed audit: vacancies: %w", err)
	}
	for i := 0; vacs.Next(); i++ {
		var id uuid.UUID
		var title, status string
		var by *uuid.UUID
		var published, closed *time.Time
		if err := vacs.Scan(&id, &title, &status, &by, &published, &closed); err != nil {
			vacs.Close()
			return nil, fmt.Errorf("seed audit: vacancies: %w", err)
		}
		actor := owner
		if by != nil {
			actor = *by
		}
		pubAt := day(30-i%25, 9+i%8)
		if published != nil && published.Before(now) && published.After(now.AddDate(0, 0, -60)) {
			pubAt = *published
		}
		evs = append(evs, audit.Event{Action: audit.VacancyPublished, OrgID: orgID, Actor: actor, TargetKind: "vacancy", TargetID: id, TargetLabel: title,
			At: clamp(pubAt, now), Details: map[string]string{"from": "draft", "to": "published"}})
		if status == "closed" {
			at := pubAt.AddDate(0, 0, 9)
			if closed != nil && closed.After(pubAt) && closed.Before(now) {
				at = *closed
			}
			evs = append(evs, audit.Event{Action: audit.VacancyClosed, OrgID: orgID, Actor: actor, TargetKind: "vacancy", TargetID: id, TargetLabel: title,
				At: clamp(at, now), Details: map[string]string{"from": "published", "to": "closed"}})
		}
	}
	vacs.Close()

	// Отклики: просмотры и решения (решает владелец или его кадровик).
	reviewer := owner
	if len(ms) > 0 && ms[0].role == "hr" {
		reviewer = ms[0].id
	}
	apps, err := pool.Query(ctx, `SELECT a.id, a.user_id, a.status, a.status_changed_at, a.created_at, v.title
		FROM applications a JOIN vacancies v ON v.id = a.vacancy_id
		WHERE v.org_id = $1 AND a.status <> 'withdrawn' ORDER BY a.created_at, a.id LIMIT 20`, orgID)
	if err != nil {
		return nil, fmt.Errorf("seed audit: applications: %w", err)
	}
	for i := 0; apps.Next(); i++ {
		var id, user uuid.UUID
		var status, title string
		var changed, created time.Time
		if err := apps.Scan(&id, &user, &status, &changed, &created, &title); err != nil {
			apps.Close()
			return nil, fmt.Errorf("seed audit: applications: %w", err)
		}
		if status == "sent" {
			continue
		}
		base := clamp(changed, now)
		ev := func(a audit.Action, at time.Time, d map[string]string) audit.Event {
			return audit.Event{Action: a, OrgID: orgID, Actor: reviewer, Subject: user, TargetKind: "application", TargetID: id, TargetLabel: title, At: clamp(at, now), Details: d}
		}
		evs = append(evs, ev(audit.ApplicationViewed, base.Add(-2*time.Hour), nil))
		if i%2 == 0 {
			evs = append(evs, ev(audit.ApplicationFileDownloaded, base.Add(-90*time.Minute), map[string]string{"kind": "cv"}))
		}
		switch status {
		case "accepted":
			evs = append(evs, ev(audit.ApplicationAccepted, base, nil))
		case "rejected":
			evs = append(evs, ev(audit.ApplicationRejected, base, nil))
		}
	}
	apps.Close()
	return evs, nil
}
