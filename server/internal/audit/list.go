package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
	"scibox/server/internal/privacy"
)

// Размер страницы журнала.
const (
	DefaultPage = 30
	MaxPage     = 100
)

// Person — человек в записи. Имя берётся из аккаунта в момент показа: у стёртого аккаунта это «Аккаунт удалён».
type Person struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Anonymous — имя и номер скрыты: человек приглашён с анонимным профилем (или скрылся от этой
	// организации) и ещё не открыл ей имя откликом или ответом «Интересно» (D-199, D-200). Тогда ID пуст, а Name не заполнен.
	Anonymous bool `json:"anonymous,omitzero"`
}

// Target — над чем сделано действие.
type Target struct {
	Kind  string     `json:"kind"`
	ID    *uuid.UUID `json:"id"`
	Label string     `json:"label"`
}

// Entry — запись журнала в том виде, в каком её видит владелец организации. Адреса здесь нет намеренно:
// он нужен для разбора происшествий (прямой запрос к базе), а не для просмотра руководителем.
type Entry struct {
	ID      int64             `json:"id"`
	At      time.Time         `json:"at"`
	Action  Action            `json:"action"`
	Actor   *Person           `json:"actor"`
	Subject *Person           `json:"subject"`
	Target  Target            `json:"target"`
	Details map[string]string `json:"details"`
}

// Page — страница журнала. Next — номер записи, с которого продолжать (передать как before); nil — больше записей нет.
type Page struct {
	Items []Entry `json:"items"`
	Next  *int64  `json:"next"`
}

// List читает журнал организации от новых записей к старым. before — номер записи, раньше которой читать (0 — с самых
// новых). Права на чтение журнала проверяет вызывающий: пакет их не знает.
func List(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, before int64, limit int) (Page, error) {
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	rows, err := q.ListOrgAudit(ctx, dbgen.ListOrgAuditParams{
		OrgID:   &orgID,
		Before:  pgtype.Int8{Int64: before, Valid: before > 0},
		MaxRows: num.Int32(limit + 1), // одна лишняя запись говорит, есть ли продолжение
	})
	if err != nil {
		return Page{}, fmt.Errorf("audit: list: %w", err)
	}
	page := Page{Items: make([]Entry, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit {
			page.Next = &rows[i-1].ID
			break
		}
		e := Entry{
			ID: r.ID, At: r.At, Action: Action(r.Action), Details: map[string]string{},
			Actor: person(r.ActorID, r.ActorName), Subject: person(r.SubjectID, r.SubjectName),
			Target: Target{Kind: r.TargetKind, ID: r.TargetID, Label: r.TargetLabel},
		}
		if e.Subject != nil && !privacy.ShowsName(r.SubjectConcealed, r.SubjectRevealed) {
			e.Subject = &Person{Anonymous: true}
		}
		if err := json.Unmarshal(r.Details, &e.Details); err != nil {
			return Page{}, fmt.Errorf("audit: decode details of %d: %w", r.ID, err)
		}
		page.Items = append(page.Items, e)
	}
	return page, nil
}

func person(id *uuid.UUID, name *string) *Person {
	if id == nil || name == nil {
		return nil
	}
	return &Person{ID: *id, Name: *name}
}
