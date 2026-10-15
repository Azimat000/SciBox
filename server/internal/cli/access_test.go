package cli

// Таблица доступа (срез 21, T-15, T-16): для каждого адреса настоящего роутера записано, кому что отвечает сервер.
// Тест обходит все адреса (chi.Walk) и падает, если у адреса нет строки в таблице или в таблице есть лишний адрес;
// затем по каждой строке отправляет настоящие запросы от каждого вида людей и сверяет ответ.
//
// Виды людей (actor):
//   - guest: не вошёл;
//   - stranger: вошёл, ни к чему отношения не имеет;
//   - subject: тот, о ком объект (автор отклика, хозяин профиля, приглашённый, хозяин поиска или сеанса);
//   - owner, hr: владелец и кадровик организации;
//   - headA: руководитель подразделения, которому принадлежит объект;
//   - headB: руководитель другого подразделения той же организации;
//   - foreign: владелец другой организации.
//
// Строки трёх видов: public (адрес для всех), self (нужен вход, человек видит только своё) и object (объект
// с правами: набор тех, кому можно, и ответ всем остальным). Если остальным отвечают «нет такого» (404),
// ответ обязан совпадать с ответом на выдуманный номер: так нельзя узнать, что чужой объект существует (T-16).
// Отказ 403 допустим только там, где сам объект публичен, и в строке записано почему.
// Отказ ничего не меняет в базе (I-10): отпечаток таблиц до и после отказов совпадает.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/config"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/testdb"
	"scibox/server/internal/testkit"
)

type actor int

const (
	guest actor = iota
	stranger
	subject
	owner
	hr
	headA
	headB
	foreign
	actorCount
)

var actorNames = [actorCount]string{"guest", "stranger", "subject", "owner", "hr", "headA", "headB", "foreign"}

func (a actor) String() string { return actorNames[a] }

type set uint16

func of(as ...actor) set {
	var s set
	for _, a := range as {
		s |= 1 << a
	}
	return s
}

func (s set) has(a actor) bool { return s&(1<<a) != 0 }

var (
	signedIn = of(stranger, subject, owner, hr, headA, headB, foreign)
	everyone = of(guest, stranger, subject, owner, hr, headA, headB, foreign)
	// staffA — те, кто ведёт вакансии и видит отклики подразделения A.
	staffA = of(owner, hr, headA)
)

type kind int

const (
	public kind = iota + 1 // адрес для всех: гость и вошедший доходят до обработчика
	self                   // нужен вход, человек работает со своими данными: гость получает 401, вошедший — успех
	object                 // объект с правами: allow получают успех, остальные — deny
)

// call — один запрос. params подставляются в адрес вместо {имя}.
type call struct {
	params map[string]string
	query  string
	body   any
	form   *formBody
	// as — от чьего имени идёт запрос в строках self (иначе от подставного человека).
	as *testkit.Person
}

type formBody struct {
	data  any
	files [][2]string // имя, содержимое
}

type kase struct {
	route string // "GET /api/vacancies/{id}/" ровно так, как отдаёт chi.Walk
	name  string // вариант строки (если у адреса их несколько)
	kind  kind
	// ok — какие статусы считаются успехом (по умолчанию 200, 201, 202, 204).
	ok []int
	// object:
	allow set
	deny  int    // что получают остальные вошедшие: 403 (объект публичен) или 404 (его «нет»)
	why   string // при deny == 403: почему существование объекта не секрет
	pub   bool   // адрес не требует входа: гость — такой же человек, как остальные, а не 401
	twin  []string
	// only — если задано, проверяются только эти люди (остальных отклоняют раньше, по другой причине).
	only set
	// twinCall — как выдумать объект, если он не в адресе, а в теле запроса (иначе подставляются параметры twin).
	twinCall func(call) call
	// perActor — у каждого человека свой объект (запрос зависит от того, кто его шлёт).
	perActor bool
	make     func(f *fx, who actor) call
}

// build делает запрос; строка без make — адрес без параметров и тела.
func (c kase) build(f *fx, who actor) call {
	if c.make == nil {
		return call{}
	}
	return c.make(f, who)
}

// ---- мир ----

type accessWorld struct {
	t       *testing.T
	dbURL   string
	pool    *pgxpool.Pool
	h       http.Handler
	k       *testkit.World
	tm      *testkit.Team
	foreign *testkit.Team
	fixer   testkit.Person // второй владелец организации: делает заготовки, не тратя пределы настоящих людей
	remote  atomic.Int64
}

func newAccessWorld(t *testing.T) *accessWorld {
	t.Helper()
	dbURL := testdb.Create(t, true)
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	prev := testkit.Pool
	testkit.Pool = pool
	t.Cleanup(func() { testkit.Pool = prev })

	w := &accessWorld{t: t, pool: pool, dbURL: dbURL, k: testkit.NewWorld(t)}
	w.h = w.app(nil)
	w.tm, w.foreign = w.k.Team(), w.k.Team()
	w.fixer = w.k.User("Заготовщик")
	if err := dbgen.New(pool).AddMember(context.Background(), dbgen.AddMemberParams{
		OrgID: w.tm.OrgID, UserID: w.fixer.ID, Role: string(access.RoleOwner), JoinedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

// app собирает сервер на базе мира; extra дополняет окружение (доверенные прокси и т. п.).
func (w *accessWorld) app(extra map[string]string) http.Handler {
	w.t.Helper()
	env := map[string]string{
		"DATABASE_URL": w.dbURL, "SCIBOX_PRODUCT_CONFIG": productConfig,
		"SCIBOX_CROSSREF_URL": "http://127.0.0.1:1", // Crossref в тестах недоступен
		// Живой поток чата в таблице доступа завершается сам: запрос ответил 200 и закрылся (срез 39).
		"SCIBOX_CHAT_STREAM_MAX_AGE": "100ms",
	}
	maps.Copy(env, extra)
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		w.t.Fatal(err)
	}
	return newApp(cfg, w.pool, w.k.Vault, discardLogger(), okHealth{}, nil, time.Now).handler
}

// fx — заготовки одного запроса: свои объекты и свой «субъект», чтобы запросы не мешали друг другу.
type fx struct {
	t    *testing.T
	w    *accessWorld
	subj *testkit.Person
	user *testkit.Person
}

func (w *accessWorld) fx(t *testing.T) *fx { return &fx{t: t, w: w} }

// Subject — человек с заполненным профилем (новый для каждой заготовки).
func (f *fx) Subject() testkit.Person {
	if f.subj == nil {
		p := f.w.k.Applicant("Субъект")
		f.subj = &p
	}
	return *f.subj
}

// User — новый человек, который работает со своими данными; свежий пароль, чтобы опасные действия проходили.
func (f *fx) User() testkit.Person {
	if f.user == nil {
		p := f.w.k.User("Временный")
		pr, err := f.w.k.Auth.Authenticate(context.Background(), p.Token)
		if err != nil {
			f.t.Fatal(err)
		}
		if err := f.w.k.Auth.Reauth(context.Background(), pr, testkit.Password); err != nil {
			f.t.Fatal(err)
		}
		f.user = &p
	}
	return *f.user
}

func (f *fx) person(a actor) *testkit.Person {
	w := f.w
	switch a {
	case stranger:
		return &w.tm.Out
	case subject:
		p := f.Subject()
		return &p
	case owner:
		return &w.tm.Owner
	case hr:
		return &w.tm.HR
	case headA:
		return &w.tm.HeadA
	case headB:
		return &w.tm.HeadB
	case foreign:
		return &w.foreign.Owner
	}
	return nil
}

// ---- запросы ----

type reply struct {
	Code int
	Raw  string
}

func (r reply) String() string { return fmt.Sprintf("%d %.300s", r.Code, r.Raw) }

func (w *accessWorld) path(c kase, cl call) string {
	_, pattern, _ := strings.Cut(c.route, " ")
	pattern = strings.TrimSuffix(pattern, "/")
	for name, v := range cl.params {
		pattern = strings.ReplaceAll(pattern, "{"+name+"}", v)
	}
	if strings.Contains(pattern, "{") {
		w.t.Fatalf("%s: parameters left in the path: %s (have %v)", c.route, pattern, slices.Sorted(maps.Keys(cl.params)))
	}
	return pattern + cl.query
}

func (w *accessWorld) send(c kase, cl call, who *testkit.Person) reply {
	w.t.Helper()
	method, _, _ := strings.Cut(c.route, " ")
	r := req{method: method, path: w.path(c, cl), remote: fmt.Sprintf("198.51.%d.%d:1", w.remote.Add(1)>>8&255, w.remote.Load()&255)}
	switch {
	case cl.form != nil:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		raw, _ := json.Marshal(cl.form.data)
		fw, _ := mw.CreateFormField("data")
		_, _ = fw.Write(raw)
		for _, f := range cl.form.files {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f[0]))
			h.Set("Content-Type", "application/pdf")
			pw, _ := mw.CreatePart(h)
			_, _ = pw.Write([]byte(f[1]))
		}
		_ = mw.Close()
		r.body, r.length, r.contentType = &buf, int64(buf.Len()), mw.FormDataContentType()
	case cl.body != nil:
		raw, _ := json.Marshal(cl.body)
		r.body, r.length, r.contentType = bytes.NewReader(raw), int64(len(raw)), "application/json"
	case method != http.MethodGet:
		// Запросы, меняющие данные, принимают только JSON, даже пустой.
		r.body, r.length, r.contentType = strings.NewReader("{}"), 2, "application/json"
	}
	if who != nil {
		r.cookie = &http.Cookie{Name: auth.CookieName, Value: who.Token}
	}
	rec := send(w.h, r)
	return reply{rec.Code, rec.Body.String()}
}

// unknown — тот же запрос, но объекты, о которых нельзя узнавать, заменены выдуманными.
func (c kase) unknown(cl call) call {
	if c.twinCall != nil {
		return c.twinCall(cl)
	}
	out := cl
	out.params = maps.Clone(cl.params)
	for _, name := range c.twin {
		if _, err := uuid.Parse(out.params[name]); err == nil {
			out.params[name] = uuid.NewString()
		} else {
			out.params[name] = "no-such-" + uuid.NewString()[:8]
		}
	}
	return out
}

// fingerprint — сжатый снимок таблиц, которые меняют разобранные здесь действия.
func (w *accessWorld) fingerprint() string {
	w.t.Helper()
	var fp string
	err := w.pool.QueryRow(context.Background(), `
		SELECT concat_ws('|',
			(SELECT count(*) FROM audit_log), (SELECT count(*) FROM outbox), (SELECT count(*) FROM notifications),
			(SELECT count(*) FROM notifications WHERE read_at IS NOT NULL),
			(SELECT md5(coalesce(string_agg(id::text || status || decision_note || coalesce(decided_by::text, ''), ',' ORDER BY id), '')) FROM applications),
			(SELECT md5(coalesce(string_agg(id::text || status || coalesce(answer_at::text, ''), ',' ORDER BY id), '')) FROM application_invitations),
			(SELECT md5(coalesce(string_agg(id::text || status || title || coalesce(unit_id::text, ''), ',' ORDER BY id), '')) FROM vacancies),
			(SELECT md5(coalesce(string_agg(org_id::text || user_id::text || role, ',' ORDER BY org_id, user_id), '')) FROM org_members),
			(SELECT md5(coalesce(string_agg(id::text || coalesce(accepted_at::text, '') || coalesce(revoked_at::text, ''), ',' ORDER BY id), '')) FROM org_invitations),
			(SELECT md5(coalesce(string_agg(id::text || name || coalesce(head_user_id::text, ''), ',' ORDER BY id), '')) FROM units),
			(SELECT md5(coalesce(string_agg(id::text || name || city, ',' ORDER BY id), '')) FROM organizations),
			(SELECT md5(coalesce(string_agg(id::text || status, ',' ORDER BY id), '')) FROM vacancy_offers),
			(SELECT md5(coalesce(string_agg(id::text || status, ',' ORDER BY id), '')) FROM reference_requests),
			(SELECT md5(coalesce(string_agg(id::text || last_seq::text || scientist_read::text || org_read::text, ',' ORDER BY id), '')) FROM chats),
			(SELECT md5(coalesce(string_agg(chat_id::text || seq::text || body || coalesce(deleted_at::text, '') || coalesce(edited_at::text, ''), ',' ORDER BY chat_id, seq), '')) FROM chat_messages),
			(SELECT md5(coalesce(string_agg(chat_id::text || user_id::text || read_seq::text || archived::text, ',' ORDER BY chat_id, user_id), '')) FROM chat_participants),
			(SELECT md5(coalesce(string_agg(id::text || title || body, ',' ORDER BY id), '')) FROM chat_templates),
			(SELECT md5(coalesce(string_agg(user_id::text || org_id::text, ',' ORDER BY user_id, org_id), '')) FROM chat_blocks),
			(SELECT count(*) FROM chat_complaints),
			(SELECT count(*) FROM favorites), (SELECT count(*) FROM saved_searches), (SELECT count(*) FROM profile_items),
			(SELECT count(*) FROM sessions), (SELECT count(*) FROM users WHERE deletion_due_at IS NOT NULL),
			(SELECT md5(coalesce(string_agg(id::text || visibility || headline, ',' ORDER BY id), '')) FROM profiles))`).Scan(&fp)
	if err != nil {
		w.t.Fatal(err)
	}
	return fp
}

// who — от чьего имени идёт запрос: у строки с заготовкой на каждого человека заготовка может подставить своего.
func (w *accessWorld) who(f *fx, c kase, cl call, a actor) *testkit.Person {
	if c.perActor && cl.as != nil {
		return cl.as
	}
	return f.person(a)
}

func successful(c kase, r reply) bool {
	if len(c.ok) > 0 {
		return slices.Contains(c.ok, r.Code)
	}
	return r.Code >= 200 && r.Code < 300
}

// ---- проверка ----

func TestAccessTable(t *testing.T) {
	w := newAccessWorld(t)
	cases := accessCases()

	t.Run("every route has a row and every row has a route", func(t *testing.T) {
		routes := map[string]bool{}
		if err := chi.Walk(w.h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			routes[method+" "+route] = true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(routes) < 100 {
			t.Fatalf("only %d routes walked: is the router fully built?", len(routes))
		}
		listed := map[string]bool{}
		for _, c := range cases {
			listed[c.route] = true
			if !routes[c.route] {
				t.Errorf("the table lists %q, but the router has no such route", c.route)
			}
		}
		for _, route := range slices.Sorted(maps.Keys(routes)) {
			if !listed[route] {
				t.Errorf("route %q has no row in the access table: write who may call it and what everybody else gets", route)
			}
		}
	})

	for _, c := range cases {
		t.Run(strings.TrimSpace(c.route+" "+c.name), func(t *testing.T) {
			switch c.kind {
			case public:
				w.checkPublic(t, c)
			case self:
				w.checkSelf(t, c)
			case object:
				w.checkObject(t, c)
			default:
				t.Fatal("the row has no kind")
			}
		})
	}
}

func (w *accessWorld) checkPublic(t *testing.T, c kase) {
	w.t = t
	for _, a := range []actor{guest, stranger} {
		f := w.fx(t)
		// Вошедший здесь новый человек: вход и выход закрывают сеанс того, чьё это имя, а общие люди нужны остальным строкам.
		var who *testkit.Person
		if a != guest {
			u := f.User()
			who = &u
		}
		r := w.send(c, c.build(f, a), who)
		if !successful(c, r) {
			t.Errorf("%s: %s", a, r)
		}
	}
}

func (w *accessWorld) checkSelf(t *testing.T, c kase) {
	w.t = t
	f := w.fx(t)
	cl := c.build(f, stranger)
	if r := w.send(c, cl, nil); r.Code != http.StatusUnauthorized || !strings.Contains(r.Raw, `"unauthorized"`) {
		t.Errorf("guest: %s", r)
	}
	who := cl.as
	if who == nil {
		u := f.User()
		who = &u
	}
	if r := w.send(c, cl, who); !successful(c, r) {
		t.Errorf("signed in: %s", r)
	}
}

func (w *accessWorld) checkObject(t *testing.T, c kase) {
	w.t = t
	if c.deny != http.StatusForbidden && c.deny != http.StatusNotFound && c.deny != http.StatusBadRequest {
		t.Fatalf("a row must say what the others get (403, 404 or 400), have %d", c.deny)
	}
	if c.deny == http.StatusForbidden && c.why == "" {
		t.Fatal("403 is allowed only for a public object: say why in the row (why)")
	}
	if c.deny != http.StatusForbidden && c.why != "" {
		t.Fatal("why is only for 403 rows")
	}
	if !c.pub && c.allow.has(guest) {
		t.Fatal("an address that needs sign-in cannot allow a guest")
	}
	var denied, allowed []actor
	for a := guest; a < actorCount; a++ {
		if c.only != 0 && !c.only.has(a) {
			continue
		}
		if c.allow.has(a) {
			allowed = append(allowed, a)
		} else {
			denied = append(denied, a)
		}
	}

	// Отказы: на одной заготовке (или на своей у каждого), ничего не меняют.
	var shared *fx
	var sharedCall call
	for _, a := range denied {
		f, cl := shared, sharedCall
		if shared == nil || c.perActor {
			f = w.fx(t)
			cl = c.build(f, a)
			if !c.perActor {
				shared, sharedCall = f, cl
			}
		}
		who := w.who(f, c, cl, a) // человека заводят до снимка: заведение меняет базу
		before := w.fingerprint()
		r := w.send(c, cl, who)
		want := c.deny
		if a == guest && !c.pub {
			want = http.StatusUnauthorized
		}
		if r.Code != want {
			t.Errorf("%s: got %s, want %d", a, r, want)
			continue
		}
		if after := w.fingerprint(); after != before {
			t.Errorf("%s: a refused request changed the database", a)
		}
		if want == http.StatusNotFound || want == http.StatusBadRequest {
			if len(c.twin) == 0 && c.twinCall == nil {
				t.Fatalf("a row that answers %d must name the parameters of the hidden object (twin)", want)
			}
			if twin := w.send(c, c.unknown(cl), who); twin != r {
				t.Errorf("%s: the answer for an existing object differs from the answer for a made-up one (T-16):\n  existing: %s\n  made up:  %s", a, r, twin)
			}
		}
	}

	// Успехи: у каждого своя заготовка, потому что действие меняет объект.
	for _, a := range allowed {
		f := w.fx(t)
		cl := c.build(f, a)
		if r := w.send(c, cl, w.who(f, c, cl, a)); !successful(c, r) {
			t.Errorf("%s: got %s, want success", a, r)
		}
	}
}

// Адрес в журнале — настоящий адрес человека, а не прокси (T-06, T-17): за доверенным прокси он берётся из заголовка.
func TestJournalGetsTheAddressOfThePerson(t *testing.T) {
	w := newAccessWorld(t)
	h := w.app(map[string]string{"SCIBOX_TRUSTED_PROXIES": "127.0.0.1"})
	user := w.k.User("Входящий")
	proxy := func(ip string) map[string]string { return map[string]string{"X-Forwarded-For": ip} }
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, testkit.Password)
	rec := send(h, req{method: http.MethodPost, path: "/api/auth/login", body: strings.NewReader(body), length: int64(len(body)),
		contentType: "application/json", remote: "127.0.0.1:5000", header: proxy("203.0.113.55")})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	inviteBody := `{"email":"colleague@example.ru","role":"hr"}`
	rec = send(h, req{method: http.MethodPost, path: "/api/organizations/" + w.tm.Slug + "/invitations", body: strings.NewReader(inviteBody),
		length: int64(len(inviteBody)), contentType: "application/json", cookie: &http.Cookie{Name: auth.CookieName, Value: w.tm.Owner.Token},
		remote: "127.0.0.1:5001", header: proxy("203.0.113.77")})
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body)
	}
	// Без доверенного прокси заголовок игнорируется: в журнале адрес соединения.
	rec = send(w.h, req{method: http.MethodPost, path: "/api/auth/login", body: strings.NewReader(body), length: int64(len(body)),
		contentType: "application/json", remote: "192.0.2.9:5000", header: proxy("203.0.113.99")})
	if rec.Code != http.StatusOK {
		t.Fatalf("login without a proxy: %d %s", rec.Code, rec.Body)
	}
	got := map[string]string{}
	rows, err := w.pool.Query(context.Background(), `SELECT action || '/' || actor_id::text, coalesce(ip, '-') FROM audit_log WHERE action IN ('account.login', 'invitation.sent') ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, ip string
		if err := rows.Scan(&k, &ip); err != nil {
			t.Fatal(err)
		}
		got[k] += ip + ";"
	}
	if got["invitation.sent/"+w.tm.Owner.ID.String()] != "203.0.113.77;" {
		t.Errorf("invitation address = %q", got["invitation.sent/"+w.tm.Owner.ID.String()])
	}
	if ips := got["account.login/"+user.ID.String()]; !strings.Contains(ips, "203.0.113.55;") || !strings.Contains(ips, "192.0.2.9;") || strings.Contains(ips, "203.0.113.99") {
		t.Errorf("login addresses = %q (want the forwarded address behind the proxy and the connection address without it)", ips)
	}
}
