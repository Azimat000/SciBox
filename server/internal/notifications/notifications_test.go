package notifications

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/mail"
	"scibox/server/internal/testkit"
)

var bg = testkit.BG

func newSvc() *Service {
	return newService(testkit.Pool, Config{ProductName: "SciBox", PublicURL: "http://localhost:5173/"})
}

func emit(t *testing.T, s *Service, n Notice) {
	t.Helper()
	if err := s.Emit(bg, s.q, n); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

type outboxRow struct {
	To, Subject, Body string
}

func outboxFor(t *testing.T, email string) []outboxRow {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT to_email, subject, body FROM outbox WHERE to_email = $1 ORDER BY id`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.To, &r.Subject, &r.Body); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestEmitCreatesNotificationAndMailToAccountEmail(t *testing.T) {
	w := testkit.NewWorld(t)
	p := w.User("Елена")
	s := newSvc()
	emit(t, s, Notice{UserID: p.ID, Kind: "application_received", Title: "Новый отклик", Body: "Иван откликнулся на вакансию.", Link: "/candidates/abc"})

	list, err := s.List(bg, p.User, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Unread != 1 || len(list.Items) != 1 {
		t.Fatalf("list = %+v", list)
	}
	it := list.Items[0]
	if it.Kind != "application_received" || it.Title != "Новый отклик" || it.Link != "/candidates/abc" || it.Read {
		t.Errorf("item = %+v", it)
	}
	mails := outboxFor(t, p.Email)
	if len(mails) != 1 {
		t.Fatalf("mails = %+v", mails)
	}
	m := mails[0]
	if m.Subject != "Новый отклик" {
		t.Errorf("subject = %q", m.Subject)
	}
	for _, want := range []string{"Иван откликнулся на вакансию.", "Открыть: http://localhost:5173/candidates/abc", "SciBox. Это письмо отправлено автоматически"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, m.Body)
		}
	}
	if strings.Contains(m.Body, "//candidates") {
		t.Errorf("double slash in link:\n%s", m.Body)
	}
}

func TestEmitWithoutBodyAndLink(t *testing.T) {
	w := testkit.NewWorld(t)
	p := w.User("Елена")
	s := newSvc()
	emit(t, s, Notice{UserID: p.ID, Kind: "x", Title: "Коротко"})
	body := outboxFor(t, p.Email)[0].Body
	if strings.Contains(body, "Открыть") || strings.HasPrefix(body, "\n") {
		t.Errorf("body = %q", body)
	}
}

func TestEmitRejectsBrokenNotices(t *testing.T) {
	w := testkit.NewWorld(t)
	p := w.User("Елена")
	s := newSvc()
	ok := Notice{UserID: p.ID, Kind: "k", Title: "t"}
	cases := map[string]func(n *Notice){
		"no user":          func(n *Notice) { n.UserID = uuid.Nil },
		"no kind":          func(n *Notice) { n.Kind = " " },
		"no title":         func(n *Notice) { n.Title = "" },
		"long title":       func(n *Notice) { n.Title = strings.Repeat("я", maxTitle+1) },
		"title line break": func(n *Notice) { n.Title = "a\nBcc: x@y.z" },
		"long link":        func(n *Notice) { n.Link = "/" + strings.Repeat("a", maxLink) },
		"external link":    func(n *Notice) { n.Link = "https://evil.example/x" },
		"protocol-relative": func(n *Notice) {
			n.Link = "//evil.example/x"
		},
		"relative link":   func(n *Notice) { n.Link = "candidates/1" },
		"space in link":   func(n *Notice) { n.Link = "/a b" },
		"newline in link": func(n *Notice) { n.Link = "/a\nb" },
		"backslash link":  func(n *Notice) { n.Link = `/\evil.example` },
	}
	for name, mutate := range cases {
		n := ok
		mutate(&n)
		if err := s.Emit(bg, s.q, n); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if got := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, p.ID); got != 0 {
		t.Errorf("%d notifications were created by broken notices", got)
	}
	if got := len(outboxFor(t, p.Email)); got != 0 {
		t.Errorf("%d mails were created by broken notices", got)
	}
}

func TestEmitForMissingUserCreatesNothing(t *testing.T) {
	// Нет такого аккаунта: уведомление не создаётся (внешний ключ), письмо тоже.
	s := newSvc()
	err := s.Emit(bg, s.q, Notice{UserID: uuid.New(), Kind: "k", Title: "t"})
	if err == nil {
		t.Fatal("notification for a missing user was created")
	}
}

func TestEnqueueValidatesAddressAndSubject(t *testing.T) {
	s := newSvc()
	addr := fmt.Sprintf("ref%d@example.ru", testkit.Seq())
	if err := s.Enqueue(bg, s.q, mail.Message{To: addr, Subject: "Просьба", Body: "текст"}); err != nil {
		t.Fatal(err)
	}
	if got := outboxFor(t, addr); len(got) != 1 || got[0].Subject != "Просьба" {
		t.Errorf("outbox = %+v", got)
	}
	bad := []mail.Message{
		{To: "not an address", Subject: "s"},
		{To: "", Subject: "s"},
		{To: addr, Subject: ""},
		{To: addr, Subject: "a\r\nBcc: x@y.z"},
	}
	for _, m := range bad {
		if err := s.Enqueue(bg, s.q, m); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v", m, err)
		}
	}
	if got := len(outboxFor(t, addr)); got != 1 {
		t.Errorf("outbox grew to %d", got)
	}
}

func TestQueueSendIsDurableAndValidates(t *testing.T) {
	q := NewQueue(testkit.Pool)
	addr := fmt.Sprintf("queue%d@example.ru", testkit.Seq())
	if err := q.Send(bg, mail.Message{To: addr, Subject: "Подтверждение", Body: "ссылка"}); err != nil {
		t.Fatal(err)
	}
	if got := outboxFor(t, addr); len(got) != 1 || got[0].Body != "ссылка" {
		t.Errorf("outbox = %+v", got)
	}
	if err := q.Send(bg, mail.Message{To: "x", Subject: "s"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad address: %v", err)
	}
}

func TestListPagingFilterAndLimits(t *testing.T) {
	w := testkit.NewWorld(t)
	p := w.User("Елена")
	s := newSvc()
	base := time.Now().UTC().Add(-time.Hour)
	for i := range 5 {
		s.now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		emit(t, s, Notice{UserID: p.ID, Kind: "k", Title: fmt.Sprintf("n%d", i)})
	}
	all, _ := s.List(bg, p.User, 2, 0, false)
	if all.Total != 5 || len(all.Items) != 2 || all.Items[0].Title != "n4" || all.Items[1].Title != "n3" {
		t.Errorf("first page = %+v", all)
	}
	next, _ := s.List(bg, p.User, 2, 4, false)
	if len(next.Items) != 1 || next.Items[0].Title != "n0" {
		t.Errorf("last page = %+v", next)
	}
	// Некорректные границы приводятся к допустимым.
	def, _ := s.List(bg, p.User, -5, -5, false)
	if len(def.Items) != 5 {
		t.Errorf("default page has %d items", len(def.Items))
	}
	big, _ := s.List(bg, p.User, 10_000, 0, false)
	if len(big.Items) != 5 {
		t.Errorf("big limit page has %d items", len(big.Items))
	}
	// Прочитанные не попадают в «только непрочитанные».
	if err := s.MarkRead(bg, p.User, all.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	unread, _ := s.List(bg, p.User, 20, 0, true)
	if len(unread.Items) != 4 || unread.Unread != 4 || unread.Total != 5 {
		t.Errorf("unread = %+v", unread)
	}
	for _, it := range unread.Items {
		if it.Read || it.Title == "n4" {
			t.Errorf("read item in unread list: %+v", it)
		}
	}
}

func TestMarkReadOnlyOwn(t *testing.T) {
	w := testkit.NewWorld(t)
	a, b := w.User("Первый"), w.User("Второй")
	s := newSvc()
	emit(t, s, Notice{UserID: a.ID, Kind: "k", Title: "для первого"})
	emit(t, s, Notice{UserID: b.ID, Kind: "k", Title: "для второго"})
	la, _ := s.List(bg, a.User, 20, 0, false)
	id := la.Items[0].ID

	if err := s.MarkRead(bg, b.User, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign notification: %v", err)
	}
	if n, _ := s.UnreadCount(bg, a.User); n != 1 {
		t.Errorf("foreign MarkRead changed the owner's count: %d", n)
	}
	if err := s.MarkRead(bg, a.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown notification: %v", err)
	}
	if err := s.MarkRead(bg, a.User, id); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRead(bg, a.User, id); err != nil {
		t.Errorf("second MarkRead: %v", err)
	}
	if n, _ := s.UnreadCount(bg, a.User); n != 0 {
		t.Errorf("unread = %d", n)
	}
	if n, _ := s.UnreadCount(bg, b.User); n != 1 {
		t.Errorf("another person's count = %d", n)
	}
}

func TestMarkAllReadOnlyOwn(t *testing.T) {
	w := testkit.NewWorld(t)
	a, b := w.User("Первый"), w.User("Второй")
	s := newSvc()
	for range 3 {
		emit(t, s, Notice{UserID: a.ID, Kind: "k", Title: "a"})
		emit(t, s, Notice{UserID: b.ID, Kind: "k", Title: "b"})
	}
	if err := s.MarkAllRead(bg, a.User); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnreadCount(bg, a.User); n != 0 {
		t.Errorf("a unread = %d", n)
	}
	if n, _ := s.UnreadCount(bg, b.User); n != 3 {
		t.Errorf("b unread = %d", n)
	}
}

func TestNewServiceWiresPool(t *testing.T) {
	s := NewService(testkit.Pool, Config{ProductName: "SciBox", PublicURL: "http://x"})
	if s.now().IsZero() {
		t.Error("clock is not set")
	}
}
