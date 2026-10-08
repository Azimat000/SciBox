// Package testaudit — помощники тестов журнала действий: прочитать записи и «сломать» журнал, чтобы проверить,
// что действие без записи не проходит. В боевом коде не используется (как testkit); зависит только от базы,
// поэтому годится и для пакетов, чьи собственные тесты не могут подключить testkit.
package testaudit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Row — запись журнала, как она лежит в таблице (с адресом).
type Row struct {
	ID          int64
	Action      string
	Org         *uuid.UUID
	Actor       *uuid.UUID
	Subject     *uuid.UUID
	TargetKind  string
	TargetID    *uuid.UUID
	TargetLabel string
	Details     map[string]string
	IP          *string
}

// Rows читает записи с условием where (SQL над таблицей audit_log), от старых к новым.
func Rows(t testing.TB, pool *pgxpool.Pool, where string, args ...any) []Row {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, action, org_id, actor_id, subject_id, target_kind, target_id, target_label, details, ip
		FROM audit_log WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var details []byte
		if err := rows.Scan(&r.ID, &r.Action, &r.Org, &r.Actor, &r.Subject, &r.TargetKind, &r.TargetID, &r.TargetLabel, &details, &r.IP); err != nil {
			t.Fatalf("audit scan: %v", err)
		}
		if err := json.Unmarshal(details, &r.Details); err != nil {
			t.Fatalf("audit details: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	return out
}

// Org — записи журнала организации.
func Org(t testing.TB, pool *pgxpool.Pool, org uuid.UUID) []Row {
	t.Helper()
	return Rows(t, pool, `org_id = $1`, org)
}

// Account — записи об аккаунте (без организации), где человек — тот, кто сделал.
func Account(t testing.TB, pool *pgxpool.Pool, user uuid.UUID) []Row {
	t.Helper()
	return Rows(t, pool, `org_id IS NULL AND actor_id = $1`, user)
}

// Actions — коды действий в порядке записей.
func Actions(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Action
	}
	return out
}

// Break заставляет журнал отвечать ошибкой на каждую новую запись до конца теста. Тест, который его использует,
// не должен идти параллельно с другими тестами пакета.
func Break(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE FUNCTION audit_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'the journal is down'; END $$`,
		`CREATE TRIGGER audit_test_fail BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION audit_test_fail()`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("break audit: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{`DROP TRIGGER audit_test_fail ON audit_log`, `DROP FUNCTION audit_test_fail()`} {
			if _, err := pool.Exec(ctx, q); err != nil {
				t.Errorf("repair audit: %v", err)
			}
		}
	})
}
