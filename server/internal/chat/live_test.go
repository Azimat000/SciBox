package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/httpguard"
	"scibox/server/internal/testkit"
)

// Живое обновление чата (срез 39, D-242…D-244, T-100): настоящий сервер потока на настоящей базе, люди подключаются
// по HTTP и читают Server-Sent Events, как это делает сайт.

// spyObserver запоминает, что потоку есть о чём сообщить.
type spyObserver struct {
	mu        sync.Mutex
	opened    int
	closed    map[string]int
	rejected  map[string]int
	dispatch  map[string]int
	delivered map[string]int
	lates     []time.Duration
	catchUp   int
	listening chan struct{}
	once      sync.Once
}

func newSpy() *spyObserver {
	return &spyObserver{closed: map[string]int{}, rejected: map[string]int{}, dispatch: map[string]int{}, delivered: map[string]int{}, listening: make(chan struct{})}
}

func (o *spyObserver) StreamOpened() { o.mu.Lock(); o.opened++; o.mu.Unlock() }
func (o *spyObserver) StreamClosed(reason string, _ time.Duration) {
	o.mu.Lock()
	o.closed[reason]++
	o.mu.Unlock()
}
func (o *spyObserver) StreamRejected(reason string) { o.mu.Lock(); o.rejected[reason]++; o.mu.Unlock() }
func (o *spyObserver) EventDispatched(kind string)  { o.mu.Lock(); o.dispatch[kind]++; o.mu.Unlock() }
func (o *spyObserver) EventDelivered(kind string, late time.Duration) {
	o.mu.Lock()
	o.delivered[kind]++
	o.lates = append(o.lates, late)
	o.mu.Unlock()
}
func (o *spyObserver) ListenerState(up bool) {
	if up {
		o.once.Do(func() { close(o.listening) })
	}
}
func (o *spyObserver) CatchUp(n int) { o.mu.Lock(); o.catchUp += n; o.mu.Unlock() }

func (o *spyObserver) closedCount(reason string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closed[reason]
}

// liveWorld — переписка с настоящим потоком: раздача событий, HTTP-сервер и люди-читатели.
type liveWorld struct {
	*world
	hub *Hub
	obs *spyObserver
	srv *httptest.Server
	cfg HubConfig
}

// liveConfig — быстрые сроки для тестов.
func liveConfig() HubConfig {
	c := DefaultHubConfig()
	c.Ping, c.Recheck, c.Poll, c.UnreadEvery = 150*time.Millisecond, 120*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond
	c.UnreadGap = 20 * time.Millisecond
	return c
}

func newLive(t *testing.T, mutate ...func(*HubConfig)) *liveWorld {
	t.Helper()
	return newLiveOn(t, newWorld(t), mutate...)
}

func newLiveOn(t *testing.T, w *world, mutate ...func(*HubConfig)) *liveWorld {
	t.Helper()
	cfg := liveConfig()
	for _, m := range mutate {
		m(&cfg)
	}
	obs := newSpy()
	hub := NewHub(w.svc, testkit.Pool.Config().ConnConfig, cfg, w.Log, obs)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := hub.Run(ctx); err != nil {
			t.Errorf("hub: %v", err)
		}
	}()
	select {
	case <-obs.listening:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener did not connect")
	}
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(w.svc, w.Log, requireUser).WithHub(hub, nil).Mount(r)
	})
	srv := httptest.NewServer(api.H)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		hub.Close()
		cancel()
		<-done
	})
	return &liveWorld{world: w, hub: hub, obs: obs, srv: srv, cfg: cfg}
}

// sseEvent — событие потока, как его читает сайт.
type sseEvent struct {
	id, name string
	data     map[string]any
	raw      string
}

// reader — открытый поток человека.
type reader struct {
	t      *testing.T
	events chan sseEvent
	cancel context.CancelFunc
	status int
	header http.Header
	done   chan struct{}
}

// stream открывает поток человека; lastID — заголовок Last-Event-ID (пусто — не посылать).
func (l *liveWorld) stream(who testkit.Person, lastID string) *reader {
	l.T.Helper()
	ctx, cancel := context.WithCancel(l.T.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.srv.URL+"/api/chats/events", nil)
	if err != nil {
		l.T.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.Token})
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		l.T.Fatal(err)
	}
	r := &reader{t: l.T, events: make(chan sseEvent, 256), cancel: cancel, status: resp.StatusCode, header: resp.Header, done: make(chan struct{})}
	l.T.Cleanup(cancel)
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		close(r.done)
		close(r.events)
		return r
	}
	go func() {
		defer close(r.done)
		defer close(r.events)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.name != "" {
					r.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, ":"):
				r.events <- sseEvent{name: "#comment", raw: line}
			case strings.HasPrefix(line, "id: "):
				ev.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.raw = strings.TrimPrefix(line, "data: ")
				_ = json.Unmarshal([]byte(ev.raw), &ev.data)
			}
		}
	}()
	return r
}

// until ждёт события с таким именем; события других видов (и комментарии) пропускаются.
func (r *reader) until(name string, within time.Duration) sseEvent {
	r.t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				r.t.Fatalf("the stream ended while waiting for %s", name)
			}
			if ev.name == name {
				return ev
			}
		case <-timer.C:
			r.t.Fatalf("no %s within %v", name, within)
		}
	}
}

// none проверяет, что за срок событий с такими именами не пришло (прочие виды пропускаются).
func (r *reader) none(within time.Duration, names ...string) {
	r.t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return
			}
			for _, n := range names {
				if ev.name == n {
					r.t.Fatalf("unexpected %s: %s", n, ev.raw)
				}
			}
		case <-timer.C:
			return
		}
	}
}

// closed ждёт, пока поток закроется.
func (r *reader) closed(within time.Duration) {
	r.t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-r.events:
			if !ok {
				return
			}
		case <-timer.C:
			r.t.Fatalf("the stream is still open after %v", within)
		}
	}
}

// drain забирает все события, которые уже пришли и придут в ближайшие d.
func (r *reader) drain(d time.Duration) []sseEvent {
	var out []sseEvent
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return out
			}
			if ev.name != "#comment" {
				out = append(out, ev)
			}
		case <-timer.C:
			return out
		}
	}
}

func jstr(m map[string]any, path ...string) string {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprint(cur)
}

// checkContract сверяет событие с contracts/chat/events.json.
func checkContract(t *testing.T, ev sseEvent) {
	t.Helper()
	id := ev.id
	if id == "" {
		id = "0"
	}
	raw, _ := json.Marshal(map[string]any{"id": id, "event": ev.name, "data": json.RawMessage(ev.raw)})
	f := loadContract(t, "events")
	if errs := f.responseErrors(string(raw)); len(errs) > 0 {
		t.Errorf("%s does not match the contract: %v\n%s", ev.name, errs, raw)
	}
}

const wait = 5 * time.Second

// Сообщение доходит до обеих сторон, каждой в её виде; тем, кому переписка не положена, не приходит ничего (T-100).
func TestStreamCarriesMessagesOnlyToParticipants(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	otherSci := l.Applicant("Другой Учёный")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")

	sciS := l.stream(sci, "")
	ownerS := l.stream(l.tm.Owner, "")
	hrS := l.stream(l.tm.HR, "")
	headAS := l.stream(l.tm.HeadA, "")
	headBS := l.stream(l.tm.HeadB, "")
	outS := l.stream(l.tm.Out, "")
	otherS := l.stream(otherSci, "")
	for _, s := range []*reader{sciS, ownerS, hrS, headAS, headBS, outS, otherS} {
		if s.status != 200 || !strings.HasPrefix(s.header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("status %d, content type %q", s.status, s.header.Get("Content-Type"))
		}
		if s.header.Get("X-Accel-Buffering") != "no" || !strings.Contains(s.header.Get("Cache-Control"), "no-cache") {
			t.Errorf("headers: %v", s.header)
		}
		first := s.until("unread.count", wait)
		checkContract(t, first)
		if jstr(first.data, "total") != "0" {
			t.Errorf("first unread.count: %s", first.raw)
		}
	}

	sent := l.send(sci, chat, "Здравствуйте, хочу уточнить условия.")

	mine := sciS.until("message.created", wait)
	checkContract(t, mine)
	if jstr(mine.data, "side") != "scientist" || jstr(mine.data, "message", "author", "is_me") != "true" ||
		jstr(mine.data, "message", "client_id") == "" || jstr(mine.data, "message", "editable_until") == "" {
		t.Errorf("scientist's own message: %s", mine.raw)
	}
	if jstr(mine.data, "message", "text") != "Здравствуйте, хочу уточнить условия." || jstr(mine.data, "chat_id") != chat.String() ||
		mine.id == "" {
		t.Errorf("scientist's own message: id %q %s", mine.id, mine.raw)
	}
	for name, s := range map[string]*reader{"owner": ownerS, "hr": hrS, "headA": headAS} {
		got := s.until("message.created", wait)
		checkContract(t, got)
		if jstr(got.data, "side") != "org" || jstr(got.data, "message", "author", "is_me") != "false" ||
			jstr(got.data, "message", "client_id") != "" || jstr(got.data, "message", "editable_until") != "" ||
			jstr(got.data, "message", "id") != sent.ID.String() {
			t.Errorf("%s: %s", name, got.raw)
		}
	}
	for name, s := range map[string]*reader{"headB": headBS, "stranger staff": outS, "other scientist": otherS} {
		t.Run("nothing for "+name, func(t *testing.T) { s.none(400*time.Millisecond, "message.created", "unread.count", "chat.read") })
	}

	// Число непрочитанных у сотрудников сменилось: событие приходит само.
	for _, s := range []*reader{ownerS, hrS, headAS} {
		u := s.until("unread.count", wait)
		checkContract(t, u)
		if jstr(u.data, "org") != "1" || jstr(u.data, "total") != "1" || jstr(u.data, "scientist") != "0" {
			t.Errorf("unread.count: %s", u.raw)
		}
	}
}

// Правка, удаление, прочтение и строка о событии приходят так же; прочтение не уходит коллегам той же стороны, а
// строка о событии приходит вместе с состоянием переписки.
func TestStreamEditDeleteReadAndSystemLines(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	app := l.application(sci, l.vacancy.ID, "sent")
	chat := l.open(sci, l.vacancy.ID, &app, nil)

	sciS := l.stream(sci, "")
	ownerS := l.stream(l.tm.Owner, "")
	ownerTab2 := l.stream(l.tm.Owner, "")
	hrS := l.stream(l.tm.HR, "")
	for _, s := range []*reader{sciS, ownerS, ownerTab2, hrS} {
		s.until("unread.count", wait)
	}

	// Правка своего сообщения.
	m := l.send(l.tm.Owner, chat, "Добрый день!")
	sciS.until("message.created", wait)
	ownerS.until("message.created", wait)
	if _, err := l.svc.Edit(bg, l.tm.Owner.User, chat, m.Seq, EditInput{Text: "Добрый день, Анна!"}); err != nil {
		t.Fatal(err)
	}
	ed := sciS.until("message.edited", wait)
	checkContract(t, ed)
	if jstr(ed.data, "message", "text") != "Добрый день, Анна!" || jstr(ed.data, "message", "edited_at") == "" {
		t.Errorf("edited: %s", ed.raw)
	}
	own := ownerS.until("message.edited", wait)
	if jstr(own.data, "message", "author", "is_me") != "true" {
		t.Errorf("author's other tab: %s", own.raw)
	}
	if got := hrS.until("message.edited", wait); jstr(got.data, "message", "author", "is_me") != "false" {
		t.Errorf("colleague: %s", got.raw)
	}

	// Повторная правка тем же текстом ничего не публикует.
	before := l.countEvents("message.edited")
	if _, err := l.svc.Edit(bg, l.tm.Owner.User, chat, m.Seq, EditInput{Text: "Добрый день, Анна!"}); err != nil {
		t.Fatal(err)
	}
	if after := l.countEvents("message.edited"); after != before {
		t.Errorf("edit with the same text published an event: %d → %d", before, after)
	}

	// Прочтение: учёный прочитал, у организации галочки; коллега по организации не получает чужую отметку.
	if err := l.svc.MarkRead(bg, sci.User, chat, 99); err != nil {
		t.Fatal(err)
	}
	rd := ownerS.until("chat.read", wait)
	checkContract(t, rd)
	if jstr(rd.data, "reader_side") != "scientist" || jstr(rd.data, "side") != "org" || jstr(rd.data, "up_to") != fmt.Sprint(m.Seq) {
		t.Errorf("chat.read: %s", rd.raw)
	}
	hrS.until("chat.read", wait)
	own = sciS.until("chat.read", wait) // другая вкладка читавшего
	if jstr(own.data, "side") != "scientist" || jstr(own.data, "reader_side") != "scientist" {
		t.Errorf("reader's own tab: %s", own.raw)
	}
	// Повтор прочтения без роста отметки событий не даёт.
	n := l.countEvents("chat.read")
	if err := l.svc.MarkRead(bg, sci.User, chat, 99); err != nil {
		t.Fatal(err)
	}
	if got := l.countEvents("chat.read"); got != n {
		t.Errorf("repeated read published an event: %d → %d", n, got)
	}

	// Организация прочитала: учёный видит «прочитано», второй сотрудник организации не получает.
	l.send(sci, chat, "Спасибо!")
	ownerS.until("message.created", wait)
	hrS.until("message.created", wait)
	if err := l.svc.MarkRead(bg, l.tm.Owner.User, chat, 99); err != nil {
		t.Fatal(err)
	}
	got := sciS.until("chat.read", wait)
	if jstr(got.data, "reader_side") != "org" || jstr(got.data, "side") != "scientist" {
		t.Errorf("chat.read for the scientist: %s", got.raw)
	}
	ownerTab2.until("chat.read", wait)
	hrS.none(400*time.Millisecond, "chat.read")

	// Удаление.
	if err := l.svc.Delete(bg, l.tm.Owner.User, chat, m.Seq); err != nil {
		t.Fatal(err)
	}
	del := sciS.until("message.deleted", wait)
	checkContract(t, del)
	if jstr(del.data, "message", "deleted") != "true" || jstr(del.data, "message", "text") != "" {
		t.Errorf("deleted: %s", del.raw)
	}

	// Строка о событии: решение организации даёт строку и новое состояние переписки.
	l.setApplication(app, "rejected")
	l.system(chat, EventApplicationDecided, map[string]any{"application_id": app, "decision": "rejected", "note": ""})
	line := sciS.until("message.created", wait) // данные строки сверены с messages.json, в events.json вида нет
	if jstr(line.data, "message", "kind") != "system" || jstr(line.data, "message", "event", "type") != EventApplicationDecided {
		t.Errorf("system line: %s", line.raw)
	}
	st := sciS.until("chat.state", wait)
	checkContract(t, st)
	if jstr(st.data, "can_write") != "false" || jstr(st.data, "closed_reason") != "rejected" || jstr(st.data, "status", "value") != "rejected" {
		t.Errorf("chat.state: %s", st.raw)
	}
	if st.id != "" {
		t.Errorf("chat.state has a journal number %q: it must not move Last-Event-ID", st.id)
	}
}

// countEvents — событий этого вида в журнале.
func (l *liveWorld) countEvents(kind string) int {
	l.T.Helper()
	return testkit.Count(l.T, `SELECT count(*) FROM chat_events WHERE kind = $1`, kind)
}

// Анонимный учёный остаётся анонимным и в потоке: организация не получает его имя ни в сообщении, ни в «печатает» (T-97).
func TestStreamKeepsAnonymityOfTheScientist(t *testing.T) {
	const name = "Кирилл Заречный"
	l := newLive(t)
	sci := l.Applicant(name)
	l.privacy(sci, "anon")
	offer := l.offer(sci, l.vacancy.ID, "pending")
	chat := l.open(sci, l.vacancy.ID, nil, &offer)

	ownerS := l.stream(l.tm.Owner, "")
	sciS := l.stream(sci, "")
	ownerS.until("unread.count", wait)
	sciS.until("unread.count", wait)

	l.send(sci, chat, "Здравствуйте, расскажите подробнее.")
	m := ownerS.until("message.created", wait)
	checkContract(t, m)
	if strings.Contains(m.raw, name) || jstr(m.data, "message", "author", "anonymous") != "true" || jstr(m.data, "message", "author", "name") != "" {
		t.Errorf("the name leaks to the organization: %s", m.raw)
	}
	own := sciS.until("message.created", wait)
	if !strings.Contains(own.raw, name) || jstr(own.data, "message", "author", "anonymous") != "false" {
		t.Errorf("the scientist sees his own name: %s", own.raw)
	}

	if err := l.svc.Typing(bg, sci.User, chat); err != nil {
		t.Fatal(err)
	}
	ty := ownerS.until("chat.typing", wait)
	checkContract(t, ty)
	if strings.Contains(ty.raw, name) || jstr(ty.data, "author", "anonymous") != "true" {
		t.Errorf("typing leaks the name: %s", ty.raw)
	}
}

// «Печатает» доходит только до собеседника, не чаще раза в три секунды, и не пишется в базу.
func TestStreamTyping(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	sciS := l.stream(sci, "")
	ownerS := l.stream(l.tm.Owner, "")
	hrS := l.stream(l.tm.HR, "")
	headBS := l.stream(l.tm.HeadB, "")
	for _, s := range []*reader{sciS, ownerS, hrS, headBS} {
		s.until("unread.count", wait)
	}
	events := testkit.Count(t, `SELECT count(*) FROM chat_events`)

	if err := l.svc.Typing(bg, l.tm.Owner.User, chat); err != nil {
		t.Fatal(err)
	}
	ty := sciS.until("chat.typing", wait)
	checkContract(t, ty)
	if jstr(ty.data, "side") != "scientist" || jstr(ty.data, "author", "side") != "org" || jstr(ty.data, "author", "name") != l.tm.Owner.Name ||
		jstr(ty.data, "author", "anonymous") != "false" || ty.id != "" {
		t.Errorf("typing: id %q %s", ty.id, ty.raw)
	}
	// Коллеге по организации и другому подразделению «печатает» не показывается.
	hrS.none(300*time.Millisecond, "chat.typing")
	headBS.none(100*time.Millisecond, "chat.typing")
	ownerS.none(100*time.Millisecond, "chat.typing")

	// Второй вызов в пределах трёх секунд молча пропускается, через три — проходит.
	if err := l.svc.Typing(bg, l.tm.Owner.User, chat); err != nil {
		t.Fatal(err)
	}
	sciS.none(300*time.Millisecond, "chat.typing")
	l.clock.Advance(TypingEvery + time.Second)
	if err := l.svc.Typing(bg, l.tm.Owner.User, chat); err != nil {
		t.Fatal(err)
	}
	sciS.until("chat.typing", wait)

	if got := testkit.Count(t, `SELECT count(*) FROM chat_events`); got != events {
		t.Errorf("typing wrote %d journal rows", got-events)
	}
	// Закрытая переписка «печатает» не принимает.
	l.setApplication(l.applicationOf(chat), "rejected")
	if err := l.svc.Typing(bg, sci.User, chat); !errors.Is(err, ErrClosed) {
		t.Errorf("typing in a closed chat: %v", err)
	}
}

// applicationOf — отклик переписки.
func (l *liveWorld) applicationOf(chat uuid.UUID) uuid.UUID {
	l.T.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT application_id FROM chats WHERE id = $1`, chat).Scan(&id); err != nil {
		l.T.Fatal(err)
	}
	return id
}

// Сообщение, отправленное через одну копию сервера, доходит до потока, открытого на другой.
func TestStreamBetweenTwoCopies(t *testing.T) {
	w := newWorld(t)
	a := newLiveOn(t, w)
	// Вторая копия: свой сервис, своя раздача, свой HTTP-сервер, то же хранилище.
	svcB := NewService(testkit.Pool)
	svcB.now = w.clock.Now
	w2 := *w
	w2.svc = svcB
	b := newLiveOn(t, &w2)

	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	ownerOnB := b.stream(w.tm.Owner, "")
	sciOnA := a.stream(sci, "")
	ownerOnB.until("unread.count", wait)
	sciOnA.until("unread.count", wait)

	// Пишет учёный через копию A: организация на копии B видит.
	started := time.Now()
	w.send(sci, chat, "Через первую копию")
	got := ownerOnB.until("message.created", wait)
	if jstr(got.data, "message", "text") != "Через первую копию" {
		t.Errorf("copy B: %s", got.raw)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("delivery took %v, want under 2s", took)
	}
	// Отвечает организация через копию B: учёный на копии A видит, и его «печатает» тоже.
	if _, _, err := svcB.Send(bg, w.tm.Owner.User, chat, SendInput{Text: "Через вторую копию", ClientID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	sciOnA.until("message.created", wait)
	if err := svcB.Typing(bg, w.tm.Owner.User, chat); err != nil {
		t.Fatal(err)
	}
	sciOnA.until("chat.typing", wait)
	if a.obs.dispatch["message.created"] == 0 || b.obs.dispatch["message.created"] == 0 {
		t.Errorf("both copies must see the event: A %v, B %v", a.obs.dispatch, b.obs.dispatch)
	}
}

// Переподключение с Last-Event-ID досылает пропущенное; повтор события не ломает ленту (склейка по seq на сайте).
func TestStreamReplayAfterReconnect(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")

	first := l.stream(l.tm.Owner, "")
	first.until("unread.count", wait)
	l.send(sci, chat, "Первое")
	seen := first.until("message.created", wait)
	first.cancel()
	first.closed(wait)

	// Пока вкладка отключена, приходят ещё два сообщения, правка и прочтение.
	l.send(sci, chat, "Второе")
	third := l.send(sci, chat, "Третье")
	if _, err := l.svc.Edit(bg, sci.User, chat, third.Seq, EditInput{Text: "Третье, исправлено"}); err != nil {
		t.Fatal(err)
	}

	again := l.stream(l.tm.Owner, seen.id)
	checkContract(t, again.until("unread.count", wait))
	var texts []string
	var edited bool
	deadline := time.After(wait)
	for len(texts) < 2 || !edited {
		select {
		case ev := <-again.events:
			// Досланное сообщение несёт то, что с ним стало к этому моменту (например, правку), поэтому по форме
			// message.created у него бывает edited_at: контракт сверяется на живых событиях и числе непрочитанного.
			switch ev.name {
			case "message.created":
				texts = append(texts, jstr(ev.data, "message", "text"))
			case "message.edited":
				edited = true
			}
		case <-deadline:
			t.Fatalf("replay is incomplete: %v edited=%v", texts, edited)
		}
	}
	// Досланное сообщение несёт текст, каким он стал (правка уже внесена); «Первое» может прийти повторно из запаса окна.
	if !contains(texts, "Второе") || !contains(texts, "Третье, исправлено") {
		t.Errorf("replayed: %v", texts)
	}
	// Живое событие после переподключения приходит один раз.
	l.send(sci, chat, "Четвёртое")
	live := again.until("message.created", wait)
	if jstr(live.data, "message", "text") != "Четвёртое" {
		t.Errorf("after the replay: %s", live.raw)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// Переподключение показывает только то, что человеку положено: чужие события журнала не досылаются.
func TestStreamReplayRespectsRights(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chatA := l.chatOf(sci, l.vacancy.ID, "sent")
	sciB := l.Applicant("Борис Орлов")
	chatB := l.chatOf(sciB, l.other.ID, "sent")

	anchor := l.stream(l.tm.HeadB, "")
	anchor.until("unread.count", wait)
	l.send(sciB, chatB, "Для подразделения Б")
	seen := anchor.until("message.created", wait)
	anchor.cancel()
	anchor.closed(wait)

	l.send(sci, chatA, "Для подразделения А")
	l.send(sciB, chatB, "Ещё для Б")

	again := l.stream(l.tm.HeadB, seen.id)
	got := again.drain(700 * time.Millisecond)
	for _, ev := range got {
		if strings.Contains(ev.raw, "подразделения А") || jstr(ev.data, "chat_id") == chatA.String() {
			t.Errorf("replay leaked another chat: %s", ev.raw)
		}
	}
	found := false
	for _, ev := range got {
		if ev.name == "message.created" && jstr(ev.data, "message", "text") == "Ещё для Б" {
			found = true
		}
	}
	if !found {
		t.Errorf("own chat was not replayed: %d events", len(got))
	}
}

// Предел потоков: шестая вкладка человека получает 429, копия при переполнении 503; закрытый поток освобождает место.
func TestStreamLimits(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.MaxPerUser, c.MaxPerCopy = 2, 3 })
	sci := l.Applicant("Анна Ильина")
	p1 := l.stream(sci, "")
	p2 := l.stream(sci, "")
	p1.until("unread.count", wait)
	p2.until("unread.count", wait)
	third := l.stream(sci, "")
	if third.status != http.StatusTooManyRequests {
		t.Fatalf("third stream of one person: %d", third.status)
	}
	if third.header.Get("Retry-After") == "" {
		t.Error("no Retry-After on the limit")
	}
	// Освободили место: поток открывается.
	p1.cancel()
	p1.closed(wait)
	waitFor(t, func() bool { return l.hub.Streams() == 1 })
	again := l.stream(sci, "")
	if again.status != 200 {
		t.Fatalf("after closing one: %d", again.status)
	}
	again.until("unread.count", wait)

	// Копия: потоков уже два, третий чужой помещается, четвёртый нет.
	other := l.stream(l.tm.Owner, "")
	if other.status != 200 {
		t.Fatalf("copy has room: %d", other.status)
	}
	other.until("unread.count", wait)
	full := l.stream(l.tm.HR, "")
	if full.status != http.StatusServiceUnavailable {
		t.Fatalf("copy is full: %d", full.status)
	}
	l.obs.mu.Lock()
	user, cp := l.obs.rejected[RejectUser], l.obs.rejected[RejectCopy]
	l.obs.mu.Unlock()
	if user != 1 || cp != 1 {
		t.Errorf("rejections: user %d, copy %d", user, cp)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition did not come true in time")
}

// Конец сессии и потеря прав рвут поток; расширение прав его не рвёт (D-244, T-100).
func TestStreamBreaksWhenSessionOrRightsAreLost(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")

	hrS := l.stream(l.tm.HR, "")
	headS := l.stream(l.tm.HeadA, "")
	sciS := l.stream(sci, "")
	for _, s := range []*reader{hrS, headS, sciS} {
		s.until("unread.count", wait)
	}

	// Кадровика убрали из организации: события переписки ему больше не приходят, поток закрывается.
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, l.tm.OrgID, l.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	hrS.closed(wait)
	if l.obs.closedCount(CloseRights) != 1 {
		t.Errorf("closed by rights: %d", l.obs.closedCount(CloseRights))
	}
	l.send(sci, chat, "Привет")
	headS.until("message.created", wait) // у руководителя права на месте
	// Сеанс руководителя удалён (выход, смена пароля): поток закрывается.
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM sessions WHERE user_id = $1`, l.tm.HeadA.ID); err != nil {
		t.Fatal(err)
	}
	headS.closed(wait)
	if l.obs.closedCount(CloseSession) != 1 {
		t.Errorf("closed by session: %d", l.obs.closedCount(CloseSession))
	}
	// Учёный остался: его поток жив.
	l.send(l.tm.Owner, chat, "Здравствуйте")
	sciS.until("message.created", wait)
}

// Новый сотрудник получает события после обновления прав без переподключения.
func TestStreamGainsRights(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	newbie := l.User("Новый кадровик")
	s := l.stream(newbie, "")
	s.until("unread.count", wait)

	l.send(sci, chat, "До вступления")
	s.none(300*time.Millisecond, "message.created")
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'hr', now())`, l.tm.OrgID, newbie.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		l.send(sci, chat, "После вступления")
		select {
		case ev := <-s.events:
			return ev.name == "message.created" || ev.name == "unread.count"
		case <-time.After(250 * time.Millisecond):
			return false
		}
	})
}

// Медленный читатель закрывается, а не копит память; остальные не страдают.
func TestSlowReaderIsClosed(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.QueueSize = 2 })
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	fast := l.stream(l.tm.Owner, "")
	fast.until("unread.count", wait)

	// Поток, у которого очередь не разбирается: регистрируется прямо в раздаче и не читается.
	st, err := l.hub.open(bg, l.tm.HR.User, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		l.send(sci, chat, "Сообщение")
	}
	select {
	case <-st.done:
	case <-time.After(wait):
		t.Fatal("the slow stream was not closed")
	}
	if st.closeReason() != CloseSlow {
		t.Errorf("reason %q", st.closeReason())
	}
	l.hub.remove(st)
	if l.obs.closedCount(CloseSlow) != 1 {
		t.Errorf("closed as slow: %d", l.obs.closedCount(CloseSlow))
	}
	for range 5 {
		fast.until("message.created", wait)
	}
}

// Мягкая остановка: Close закрывает все потоки и не принимает новые.
func TestHubCloseEndsStreams(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	s := l.stream(sci, "")
	s.until("unread.count", wait)
	l.hub.Close()
	s.closed(wait)
	if l.obs.closedCount(CloseShutdown) != 1 {
		t.Errorf("closed by shutdown: %d", l.obs.closedCount(CloseShutdown))
	}
	late := l.stream(sci, "")
	if late.status != http.StatusServiceUnavailable {
		t.Errorf("after Close: %d", late.status)
	}
}

// Срок жизни потока ограничен: сервер сам завершает его, сайт переподключается (D-242).
func TestStreamEndsAtMaxAge(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.MaxAge = 300 * time.Millisecond })
	sci := l.Applicant("Анна Ильина")
	s := l.stream(sci, "")
	s.until("unread.count", wait)
	s.closed(wait)
	waitFor(t, func() bool { return l.obs.closedCount(CloseAge) == 1 })
}

// Пока ничего не происходит, в поток уходят комментарии «: ping».
func TestStreamPings(t *testing.T) {
	l := newLive(t)
	s := l.stream(l.Applicant("Анна Ильина"), "")
	s.until("#comment", wait)
}

// Адрес без входа отвечает 401; поток выключен (нет раздачи) — 404, как до среза 39.
func TestStreamNeedsLoginAndHub(t *testing.T) {
	l := newLive(t)
	req, _ := http.NewRequestWithContext(bg, http.MethodGet, l.srv.URL+"/api/chats/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("guest: %d", resp.StatusCode)
	}

	a := newAPI(t)
	if r := a.do(&a.tm.Owner, "GET", "/api/chats/events", nil); r.Code != http.StatusNotFound {
		t.Errorf("without a hub: %d %s", r.Code, r.Raw)
	}
}

// Архив: у вкладок того, кто убрал переписку, меняется число непрочитанных.
func TestStreamArchiveRecountsUnread(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	ownerS := l.stream(l.tm.Owner, "")
	ownerS.until("unread.count", wait)
	l.send(sci, chat, "Привет")
	u := ownerS.until("unread.count", wait)
	if jstr(u.data, "org") != "1" {
		t.Fatalf("unread: %s", u.raw)
	}
	if err := l.svc.Archive(bg, l.tm.Owner.User, chat, true); err != nil {
		t.Fatal(err)
	}
	u = ownerS.until("unread.count", wait)
	if jstr(u.data, "org") != "0" || jstr(u.data, "total") != "0" {
		t.Errorf("after archiving: %s", u.raw)
	}
}

// Задержка доставки измеряется и укладывается в цель (≤ 2 с на этой машине с запасом).
func TestStreamDeliveryIsFast(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)
	for range 10 {
		l.send(sci, chat, "Сообщение")
		owner.until("message.created", wait)
	}
	l.obs.mu.Lock()
	defer l.obs.mu.Unlock()
	if len(l.obs.lates) < 10 {
		t.Fatalf("deliveries observed: %d", len(l.obs.lates))
	}
	for _, d := range l.obs.lates {
		if d > 2*time.Second {
			t.Errorf("delivery took %v", d)
		}
	}
}

// События откатившейся транзакции не уходят: ни строки журнала, ни уведомления.
func TestRolledBackEventIsNotAnnounced(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)
	before := testkit.Count(t, `SELECT count(*) FROM chat_events`)

	boom := errors.New("boom")
	err := l.svc.inTx(bg, func(q *dbgen.Queries) error {
		if _, err := AppendSystem(bg, q, chat, EventApplicationViewed, map[string]any{"application_id": uuid.New()}, l.clock.Now()); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	owner.none(400*time.Millisecond, "message.created")
	if after := testkit.Count(t, `SELECT count(*) FROM chat_events`); after != before {
		t.Errorf("rolled back event left %d journal rows", after-before)
	}
}

// publishWithoutNotice кладёт в журнал строку о сообщении так, как это сделала бы потерянная транзакция: без NOTIFY.
func (l *liveWorld) publishWithoutNotice(chat uuid.UUID) (seq int, id int64) {
	l.T.Helper()
	seq, err := appendSystem(bg, l.q(), chat, EventApplicationViewed, map[string]any{"application_id": uuid.New()}, l.clock.Now(), false)
	if err != nil {
		l.T.Fatal(err)
	}
	err = testkit.Pool.QueryRow(bg, `INSERT INTO chat_events (kind, chat_id, user_id, org_id, unit_id, seq, created_at)
		SELECT 'message.created', c.id, c.user_id, c.org_id, v.unit_id, $2, now() FROM chats c JOIN vacancies v ON v.id = c.vacancy_id
		WHERE c.id = $1 RETURNING id`, chat, seq).Scan(&id)
	if err != nil {
		l.T.Fatal(err)
	}
	return seq, id
}

// Если уведомление не пришло, событие всё равно доходит: страховочный опрос журнала.
func TestStreamFindsEventsWhenTheNoticeIsLost(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)
	seq, _ := l.publishWithoutNotice(chat)
	got := owner.until("message.created", wait)
	if jstr(got.data, "message", "seq") != fmt.Sprint(seq) {
		t.Errorf("event: %s", got.raw)
	}
	l.obs.mu.Lock()
	defer l.obs.mu.Unlock()
	if l.obs.catchUp < 1 {
		t.Errorf("catch-up events: %d", l.obs.catchUp)
	}
}

// Соединение LISTEN оборвалось: копия подключается заново и добирает события, пропущенные за время обрыва.
func TestStreamRecoversAfterTheListenerIsLost(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.Poll = time.Hour }) // опрос не помогает: работает именно добор
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)

	// Обрыв: бэкенд соединения LISTEN убит.
	var killed int
	err := testkit.Pool.QueryRow(bg, `SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE datname = current_database() AND query LIKE 'LISTEN chat_typing%' AND pid <> pg_backend_pid()) t`).Scan(&killed)
	if err != nil || killed != 1 {
		t.Fatalf("killed %d listeners: %v", killed, err)
	}
	seq, _ := l.publishWithoutNotice(chat)
	got := owner.until("message.created", wait)
	if jstr(got.data, "message", "seq") != fmt.Sprint(seq) {
		t.Errorf("event after the outage: %s", got.raw)
	}
	// И обычная доставка после подключения работает.
	waitFor(t, func() bool {
		l.send(sci, chat, "После обрыва")
		select {
		case ev := <-owner.events:
			return ev.name == "message.created"
		case <-time.After(300 * time.Millisecond):
			return false
		}
	})
}

// Last-Event-ID, которого нет в журнале, и слишком длинная очередь пропущенного не ломают подключение: сайт получает число
// непрочитанного и перечитывает списки сам.
func TestStreamReplayEdges(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.ReplayLimit = 2 })
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")

	unknown := l.stream(l.tm.Owner, "99999999999")
	checkContract(t, unknown.until("unread.count", wait))
	unknown.none(300*time.Millisecond, "message.created")
	unknown.cancel()

	garbage := l.stream(l.tm.Owner, "not-a-number")
	garbage.until("unread.count", wait)
	garbage.cancel()

	l.send(sci, chat, "Раз")
	var firstID string
	probe := l.stream(l.tm.Owner, "")
	probe.until("unread.count", wait)
	l.send(sci, chat, "Два")
	firstID = probe.until("message.created", wait).id
	l.send(sci, chat, "Три")
	l.send(sci, chat, "Четыре")
	probe.until("message.created", wait)
	probe.until("message.created", wait)
	probe.cancel()

	// За окном журнала с запасом больше двух событий (лимит): ничего не досылается.
	again := l.stream(l.tm.Owner, firstID)
	again.until("unread.count", wait)
	again.none(400*time.Millisecond, "message.created")
}

// Событие о переписке, которой уже нет (удалён аккаунт), молча пропускается и поток живёт дальше.
func TestStreamSurvivesEventsOfVanishedChats(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.Poll = 50 * time.Millisecond })
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	other := l.Applicant("Борис Орлов")
	chat2 := l.chatOf(other, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)

	_, _ = l.publishWithoutNotice(chat)
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM chats WHERE id = $1`, chat); err != nil {
		t.Fatal(err)
	}
	// Событие с несуществующим сообщением в живой переписке.
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO chat_events (kind, chat_id, user_id, org_id, seq, created_at)
		SELECT 'message.created', c.id, c.user_id, c.org_id, 77, now() FROM chats c WHERE c.id = $1`, chat2); err != nil {
		t.Fatal(err)
	}
	owner.none(400*time.Millisecond, "message.created")
	l.send(other, chat2, "Живое")
	owner.until("message.created", wait)
}

// Сбой базы при открытии потока — ошибка сервера, а не тишина.
func TestStreamOpenFailureIsAnError(t *testing.T) {
	w := newWorld(t)
	db, _ := testkit.Faulty(testkit.Pool, 1)
	svc := svcOn(w, db)
	hub := NewHub(svc, nil, HubConfig{}, w.Log, nil)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(svc, w.Log, requireUser).WithHub(hub, nil).Mount(r)
	})
	r := api.Do(&w.tm.Owner, "GET", "/api/chats/events", nil)
	if r.Code != http.StatusInternalServerError {
		t.Errorf("status %d: %s", r.Code, r.Raw)
	}
}

// Поток нельзя открыть с чужого сайта (T-100): запрос, который браузер пометил как межсайтовый, отклоняется до раздачи.
func TestStreamRefusesCrossSiteRequests(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	for site, want := range map[string]int{"cross-site": http.StatusForbidden, "same-origin": http.StatusOK, "none": http.StatusOK} {
		ctx, cancel := context.WithCancel(l.T.Context())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, l.srv.URL+"/api/chats/events", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sci.Token})
		req.Header.Set("Sec-Fetch-Site", site)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Errorf("Sec-Fetch-Site %s: %d, want %d", site, resp.StatusCode, want)
		}
		cancel()
		_ = resp.Body.Close()
	}
	waitFor(t, func() bool { return l.hub.Streams() == 0 })
}

// Число непрочитанных пересчитывается не чаще раза за UnreadGap на человека: пачка сообщений даёт одно событие сразу и одно
// с итоговым числом после паузы (у руководителя большой организации пересчёт дорогой).
func TestStreamCoalescesUnreadRecounts(t *testing.T) {
	l := newLive(t, func(c *HubConfig) { c.UnreadGap = 600 * time.Millisecond })
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)

	for i := range 6 {
		l.send(sci, chat, fmt.Sprintf("Сообщение %d", i))
	}
	var counts []string
	deadline := time.After(2500 * time.Millisecond)
collect:
	for {
		select {
		case ev := <-owner.events:
			if ev.name == "unread.count" {
				counts = append(counts, jstr(ev.data, "org"))
			}
		case <-deadline:
			break collect
		}
	}
	if len(counts) == 0 || len(counts) > 2 {
		t.Errorf("recounts: %v, want one or two (the burst is coalesced)", counts)
	}
	// Число непрочитанных переписок, а не сообщений: переписка одна.
	if len(counts) > 0 && counts[len(counts)-1] != "1" {
		t.Errorf("last unread count: %v", counts)
	}
}

// Сроки чтения и записи сервера (30 и 60 секунд в работе) не обрывают поток: он снимает срок чтения и ведёт свой срок записи
// (D-242). Здесь сроки сервера в сто раз короче.
func TestStreamOutlivesServerTimeouts(t *testing.T) {
	w := newWorld(t)
	cfg := liveConfig()
	obs := newSpy()
	hub := NewHub(w.svc, testkit.Pool.Config().ConnConfig, cfg, w.Log, obs)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = hub.Run(ctx) }()
	<-obs.listening
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(w.svc, w.Log, requireUser).WithHub(hub, nil).Mount(r)
	})
	srv := httptest.NewUnstartedServer(nil)
	srv.Config = httpguard.NewServer(api.H, httpguard.Timeouts{Header: time.Second, Read: 300 * time.Millisecond, Write: 400 * time.Millisecond, Idle: time.Second})
	srv.Start()
	t.Cleanup(func() { hub.Close(); cancel(); <-done; srv.Close() })

	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	l := &liveWorld{world: w, hub: hub, obs: obs, srv: srv, cfg: cfg}
	owner := l.stream(w.tm.Owner, "")
	owner.until("unread.count", wait)
	time.Sleep(1500 * time.Millisecond) // втрое дольше срока чтения и записи
	w.send(sci, chat, "Через полторы секунды")
	got := owner.until("message.created", wait)
	if jstr(got.data, "message", "text") != "Через полторы секунды" {
		t.Errorf("after the timeouts: %s", got.raw)
	}
}
