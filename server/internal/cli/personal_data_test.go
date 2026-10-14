package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
)

// markersOf собирает то, по чему человека можно узнать в базе: почта, имя, идентификаторы профиля, почты рекомендателей и
// адрес входа. Короткие строки отбрасываются: они дали бы случайные совпадения.
func markersOf(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, ip string) []string {
	t.Helper()
	ctx := t.Context()
	var out []string
	add := func(vals ...*string) {
		for _, v := range vals {
			if v != nil && utf8.RuneCountInString(*v) >= 8 {
				out = append(out, *v)
			}
		}
	}
	collect := func(query string, nCols int) {
		rows, err := pool.Query(ctx, query, id)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		defer rows.Close()
		for rows.Next() {
			vals := make([]*string, nCols)
			dest := make([]any, nCols)
			for i := range vals {
				dest[i] = &vals[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			add(vals...)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	// Только то, что принадлежит одному человеку: общие фразы (шаблон письма, названия областей, должности) встречаются
	// и в чужих данных, и остаются там по праву.
	collect(`SELECT email::text, display_name FROM users WHERE id = $1`, 2)
	collect(`SELECT orcid, contact_email::text, spin, scopus_id, wos_id, github_url FROM profiles WHERE user_id = $1`, 6)
	collect(`SELECT contact_email::text FROM applications WHERE user_id = $1`, 1)
	collect(`SELECT r.email::text FROM reference_requests r JOIN applications a ON a.id = r.application_id WHERE a.user_id = $1`, 1)
	if ip != "" {
		out = append(out, ip)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// leftovers ищет маркеры во всех таблицах базы: каждая строка каждой таблицы превращается в текст и сверяется без учёта регистра.
func leftovers(t *testing.T, pool *pgxpool.Pool, markers []string) map[string]int {
	t.Helper()
	ctx := t.Context()
	patterns := make([]string, len(markers))
	escape := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	for i, m := range markers {
		patterns[i] = "%" + escape.Replace(m) + "%"
	}
	rows, err := pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'goose_db_version' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for _, table := range tables {
		var n int
		q := fmt.Sprintf(`SELECT count(*) FROM %s x WHERE row_to_json(x)::text ILIKE ANY ($1::text[])`, pgx.Identifier{table}.Sanitize())
		if err := pool.QueryRow(ctx, q, patterns).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n > 0 {
			found[table] = n
		}
	}
	return found
}

// После удаления аккаунта в базе нет ничего, по чему человека можно узнать, кроме обезличенных откликов и записей
// «кто сделал» (T-11, T-33, T-35, I-27). Проверка идёт на всех демо-людях: у них отклики с рекомендателями, избранное,
// поиски, приглашения, записи журнала, сеансы и письма. Сначала человек входит с известного адреса, чтобы сеанс,
// знакомый браузер, письмо о новом устройстве и запись журнала тоже были на месте.
func TestErasedPersonLeavesNothingToRecogniseThemBy(t *testing.T) {
	w := newPerimeterWorld(t)
	h := w.handler(nil)
	ctx := t.Context()
	rows, err := w.pool.Query(ctx, `SELECT id, email::text FROM users WHERE email_confirmed_at IS NOT NULL AND deleted_at IS NULL ORDER BY email`)
	if err != nil {
		t.Fatal(err)
	}
	type person struct {
		id    uuid.UUID
		email string
	}
	people, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (p person, err error) { return p, r.Scan(&p.id, &p.email) })
	if err != nil || len(people) < 15 {
		t.Fatalf("demo people: %d %v", len(people), err)
	}

	erased, withApplications := 0, 0
	for i, p := range people {
		t.Run(p.email, func(t *testing.T) {
			ip := fmt.Sprintf("198.51.100.%d", 10+i)
			login(t, h, p.email, ip+":4000", map[string]string{"User-Agent": "Mozilla/5.0 (Macintosh) Firefox/130.0"})
			markers := markersOf(t, w.pool, p.id, ip)
			if len(markers) < 2 {
				t.Fatalf("too few markers to look for: %v", markers)
			}
			var apps int
			if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM applications WHERE user_id = $1`, p.id).Scan(&apps); err != nil {
				t.Fatal(err)
			}
			// До стирания метки видны в базе: проверка должна уметь находить.
			if before := leftovers(t, w.pool, markers); len(before) == 0 {
				t.Fatalf("the markers are not found before the erasure, the check is blind: %v", markers)
			}
			err := eraseInTx(ctx, w.pool, p.id)
			if blocked, ok := asBlocked(err); ok {
				t.Skipf("sole owner of %d organizations with staff: erasure is refused by design", len(blocked.Orgs))
			}
			if err != nil {
				t.Fatal(err)
			}
			erased++
			if apps > 0 {
				withApplications++
			}
			if after := leftovers(t, w.pool, markers); len(after) > 0 {
				t.Errorf("after the erasure the person is still recognisable in %v (markers: %q)", after, markers)
			}
		})
	}
	if erased < 8 || withApplications < 3 {
		t.Errorf("only %d people erased, %d of them with applications: the test lost its coverage", erased, withApplications)
	}
}

func asBlocked(err error) (*erasure.BlockedError, bool) {
	if err == nil {
		return nil, false
	}
	return errorsAsBlocked(err)
}

func eraseInTx(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	u, err := q.LockUserRow(ctx, id)
	if err != nil {
		return err
	}
	if err := erasure.Erase(ctx, q, u, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func errorsAsBlocked(err error) (*erasure.BlockedError, bool) {
	return errors.AsType[*erasure.BlockedError](err)
}

// Выгрузка через всю сборку сервера: роутер кладёт адрес человека в журнал, периметр не режет долгую передачу,
// заголовки защиты стоят, а пароль и лимит работают.
func TestExportThroughTheWholeServer(t *testing.T) {
	w := newPerimeterWorld(t)
	h := w.handler(nil)
	const email, remote = "dmitry.korolev@demo.example.ru", "198.51.100.200"
	cookie := login(t, h, email, remote+":5000", nil)
	rec := send(h, req{path: "/api/account/export", cookie: cookie, remote: remote + ":5000"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %q %.200s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the archive must not be cached or sniffed: %v", rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["data.json"] || !names["README.txt"] {
		t.Errorf("archive entries: %v", names)
	}
	files := 0
	for name := range names {
		if strings.HasPrefix(name, "files/") {
			files++
		}
	}
	if files == 0 {
		t.Error("the demo applicant has applications with files: they must be in the archive")
	}
	var ip *string
	if err := w.pool.QueryRow(t.Context(), `SELECT a.ip FROM audit_log a JOIN users u ON u.id = a.actor_id WHERE u.email = $1 AND a.action = 'account.data_exported'`, email).Scan(&ip); err != nil || ip == nil || *ip != remote {
		t.Errorf("the journal must record the address of the request: %v %v", ip, err)
	}
	// Сеанс из ссылки не свежий: окно пароля, а не архив.
	if rec := send(h, req{path: "/api/account/export", cookie: staleSession(t, w, h, email), remote: remote + ":5000"}); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "reauth_required") {
		t.Errorf("stale session: %d %s", rec.Code, rec.Body)
	}
}

// staleSession — сеанс без недавнего ввода пароля: вход по паролю ставит отметку «свежий», поэтому она убирается вручную.
func staleSession(t *testing.T, w *perimeterWorld, h http.Handler, email string) *http.Cookie {
	t.Helper()
	c := login(t, h, email, "198.51.100.201:5000", nil)
	if _, err := w.pool.Exec(t.Context(), `UPDATE sessions SET reauth_at = NULL WHERE user_id = (SELECT id FROM users WHERE email = $1)`, email); err != nil {
		t.Fatal(err)
	}
	return c
}
