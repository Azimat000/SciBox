package chat

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Мелочи живого потока, не требующие настоящего соединения: разбор нагрузки уведомлений, ворота «печатает», вид события
// на проводе, права получателей.

func TestParseEvent(t *testing.T) {
	id, org := uuid.New(), uuid.New()
	good := `{"id":7,"kind":"message.created","chat_id":"` + id.String() + `","user_id":"` + uuid.NewString() + `","org_id":"` + org.String() +
		`","unit_id":null,"seq":3,"actor_id":null,"side":null,"up_to":0,"created_at":"2026-10-15T10:30:00.123456+00:00"}`
	ev, err := parseEvent(good)
	if err != nil || ev.ID != 7 || ev.Seq != 3 || ev.OrgID != org || ev.UnitID != nil || ev.CreatedAt.Year() != 2026 {
		t.Fatalf("event %+v err %v", ev, err)
	}
	for name, bad := range map[string]string{"not json": "{", "no id": `{"kind":"x","chat_id":"` + id.String() + `"}`, "no kind": `{"id":1,"chat_id":"` + id.String() + `"}`, "no chat": `{"id":1,"kind":"x"}`} {
		if _, err := parseEvent(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseTyping(t *testing.T) {
	n := typingNote{ChatID: uuid.New(), UserID: uuid.New(), Side: SideOrg, Scientist: uuid.New(), OrgID: uuid.New(), Name: "Елена", Anonymous: false}
	got, err := parseTyping(n.payload())
	if err != nil || got != n {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	if len(n.payload()) > 600 {
		t.Errorf("typing notification is too long: %d", len(n.payload()))
	}
	for name, bad := range map[string]string{
		"not json":   "{",
		"no chat":    `{"u":"` + uuid.NewString() + `","s":"org","x":"` + uuid.NewString() + `","o":"` + uuid.NewString() + `"}`,
		"bad side":   `{"c":"` + uuid.NewString() + `","u":"` + uuid.NewString() + `","s":"admin","x":"` + uuid.NewString() + `","o":"` + uuid.NewString() + `"}`,
		"no org":     `{"c":"` + uuid.NewString() + `","u":"` + uuid.NewString() + `","s":"org","x":"` + uuid.NewString() + `"}`,
		"no typist":  `{"c":"` + uuid.NewString() + `","s":"org","x":"` + uuid.NewString() + `","o":"` + uuid.NewString() + `"}`,
		"no subject": `{"c":"` + uuid.NewString() + `","u":"` + uuid.NewString() + `","s":"org","o":"` + uuid.NewString() + `"}`,
	} {
		if _, err := parseTyping(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTypingGate(t *testing.T) {
	g := newTypingGate()
	chat, user := uuid.New(), uuid.New()
	now := time.Now()
	if !g.allow(chat, user, now) || g.allow(chat, user, now.Add(TypingEvery-time.Millisecond)) {
		t.Fatal("the second call within the period must be refused")
	}
	if !g.allow(chat, user, now.Add(TypingEvery)) {
		t.Error("after the period the call goes through")
	}
	if !g.allow(chat, uuid.New(), now) || !g.allow(uuid.New(), user, now) {
		t.Error("another person or another chat is not limited")
	}
	// Записи копятся не бесконечно: старые выбрасываются, когда их слишком много.
	for range typingGateSweepAt {
		g.allow(uuid.New(), uuid.New(), now)
	}
	g.allow(uuid.New(), uuid.New(), now.Add(time.Hour))
	if len(g.last) > 2 {
		t.Errorf("old entries were not dropped: %d", len(g.last))
	}
}

func TestFrameOnTheWire(t *testing.T) {
	f, err := newFrame(1234567890123, "message.created", map[string]int{"a": 1}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(f.wire()), "id: 1234567890123\nevent: message.created\ndata: {\"a\":1}\n\n"; got != want {
		t.Errorf("wire %q, want %q", got, want)
	}
	u, _ := newFrame(0, "unread.count", UnreadCount{Total: 1, Org: 1}, time.Time{})
	if got := string(u.wire()); strings.Contains(got, "id:") || !strings.HasPrefix(got, "event: unread.count\n") {
		t.Errorf("a frame without a number must not carry id: %q", got)
	}
	if got := string(appendInt(nil, 0)); got != "0" {
		t.Errorf("appendInt(0) = %q", got)
	}
	if _, err := newFrame(0, "x", math.NaN(), time.Time{}); err == nil {
		t.Error("a value JSON cannot hold must be an error")
	}
}

func TestHubConfigDefaults(t *testing.T) {
	d := DefaultHubConfig()
	if got := (HubConfig{}).orDefaults(); got != d {
		t.Errorf("zero config: %+v, want %+v", got, d)
	}
	got := HubConfig{MaxPerUser: 1, MaxPerCopy: 2, Ping: time.Second}.orDefaults()
	if got.MaxPerUser != 1 || got.MaxPerCopy != 2 || got.Ping != time.Second || got.Recheck != d.Recheck || got.QueueSize != d.QueueSize {
		t.Errorf("partial config: %+v", got)
	}
}

func TestScopeShrinks(t *testing.T) {
	org1, org2, unit1, unit2 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mk := func(orgs, units []uuid.UUID) scope {
		s := scope{orgs: map[uuid.UUID]struct{}{}, units: map[uuid.UUID]struct{}{}}
		for _, id := range orgs {
			s.orgs[id] = struct{}{}
		}
		for _, id := range units {
			s.units[id] = struct{}{}
		}
		return s
	}
	old := mk([]uuid.UUID{org1}, []uuid.UUID{unit1})
	for name, tc := range map[string]struct {
		now  scope
		want bool
	}{
		"same":         {mk([]uuid.UUID{org1}, []uuid.UUID{unit1}), false},
		"grown":        {mk([]uuid.UUID{org1, org2}, []uuid.UUID{unit1, unit2}), false},
		"org lost":     {mk(nil, []uuid.UUID{unit1}), true},
		"unit lost":    {mk([]uuid.UUID{org1}, nil), true},
		"all lost":     {mk(nil, nil), true},
		"org replaced": {mk([]uuid.UUID{org2}, []uuid.UUID{unit1}), true},
	} {
		if got := tc.now.shrunkFrom(old); got != tc.want {
			t.Errorf("%s: shrunk = %v, want %v", name, got, tc.want)
		}
	}
}

// Кем человек приходится переписке события: учёный главнее сотрудника той же организации.
func TestSideFor(t *testing.T) {
	sci, staff, other, org, unit := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ev := event{UserID: sci, OrgID: org, UnitID: &unit}
	whole := scope{orgs: map[uuid.UUID]struct{}{org: {}}}
	units := scope{units: map[uuid.UUID]struct{}{unit: {}}}
	foreign := scope{orgs: map[uuid.UUID]struct{}{uuid.New(): {}}, units: map[uuid.UUID]struct{}{uuid.New(): {}}}
	noUnit := event{UserID: sci, OrgID: org}
	for name, tc := range map[string]struct {
		ev    event
		user  uuid.UUID
		sc    scope
		side  Side
		found bool
	}{
		"the scientist":                    {ev, sci, scope{}, SideScientist, true},
		"the scientist works there too":    {ev, sci, whole, SideScientist, true},
		"staff of the whole organization":  {ev, staff, whole, SideOrg, true},
		"head of the unit":                 {ev, staff, units, SideOrg, true},
		"staff of another organization":    {ev, other, foreign, "", false},
		"nobody":                           {ev, other, scope{}, "", false},
		"head of a unit on a no-unit chat": {noUnit, staff, units, "", false},
	} {
		side, ok := sideFor(tc.ev, tc.user, tc.sc)
		if side != tc.side || ok != tc.found {
			t.Errorf("%s: %q %v, want %q %v", name, side, ok, tc.side, tc.found)
		}
	}
}

func TestLastEventID(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "abc": 0, "-5": 0, "0": 0, "42": 42, "9007199254740993": 9007199254740993} {
		r := httpGet(in)
		if got := lastEventID(r); got != want {
			t.Errorf("%q → %d, want %d", in, got, want)
		}
	}
	if jitter(0) != 0 || jitter(-time.Second) != 0 {
		t.Error("no jitter without a limit")
	}
	if j := jitter(time.Second); j < 0 || j >= time.Second {
		t.Errorf("jitter %v", j)
	}
}

// Раздача без наблюдателя и без настроек соединения живёт: события из журнала до неё доходят опросом, а о неудачном
// подключении LISTEN сказано в журнале и повторено.
func TestHubWithoutObserverOrConnection(t *testing.T) {
	w := newWorld(t)
	h := NewHub(w.svc, nil, HubConfig{Poll: 50 * time.Millisecond}, w.Log, nil)
	// Наблюдатель по умолчанию принимает всё.
	h.obs.StreamOpened()
	h.obs.StreamClosed(CloseClient, time.Second)
	h.obs.StreamRejected(RejectUser)
	h.obs.EventDispatched("x")
	h.obs.EventDelivered("x", time.Second)
	h.obs.ListenerState(false)
	h.obs.CatchUp(1)
	if h.Config().MaxPerUser != DefaultHubConfig().MaxPerUser {
		t.Errorf("config %+v", h.Config())
	}

	ctx, cancel := context.WithTimeout(bg, 400*time.Millisecond)
	defer cancel()
	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run without a connection: %v", err)
	}
	if err := h.Run(ctx); err == nil {
		t.Error("a second Run must be refused")
	}
}

func TestSeenSetIsPruned(t *testing.T) {
	w := newWorld(t)
	h := NewHub(w.svc, nil, HubConfig{}, w.Log, nil)
	now := time.Now()
	if h.seenBefore(1, now.Add(-seenKeep-time.Minute)) {
		t.Fatal("the first sight is not a repeat")
	}
	if !h.seenBefore(1, now) {
		t.Error("the second sight is a repeat")
	}
	h.seenBefore(2, now)
	h.pruneSeen(now)
	if h.seenBefore(2, now) != true || h.seenBefore(1, now) != false {
		t.Error("the old number must be forgotten, the fresh one kept")
	}
}

// Вид события для получателя собирается из того, что лежит в базе; если переписки или сообщения уже нет, события нет.
func TestEventViewOfMissingThings(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	m := w.send(sci, chat, "Привет")
	ev := event{ID: 1, Kind: evCreated, ChatID: chat, UserID: sci.ID, OrgID: w.tm.OrgID, Seq: m.Seq}
	if _, ok, err := w.svc.eventView(bg, ev); err != nil || !ok {
		t.Fatalf("existing: ok=%v err=%v", ok, err)
	}
	ev.Seq = 999
	if _, ok, err := w.svc.eventView(bg, ev); err != nil || ok {
		t.Errorf("missing message: ok=%v err=%v", ok, err)
	}
	ev.ChatID, ev.Seq = uuid.New(), m.Seq
	if _, ok, err := w.svc.eventView(bg, ev); err != nil || ok {
		t.Errorf("missing chat: ok=%v err=%v", ok, err)
	}
	// Строка о событии с негодными данными в событие не превращается, ошибка доходит до вызывающего.
	broken := eventView{msg: dbgen.ChatMessage{Kind: KindSystem, Seq: 1, EventType: EventChatBlocked, EventData: []byte(`{`)}}
	if _, err := w.svc.framesFor(event{Kind: evCreated, ChatID: chat}, broken, SideScientist, sci.ID); err == nil {
		t.Error("invalid event data must fail the frame")
	}
}

func httpGet(lastID string) *http.Request {
	r, _ := http.NewRequestWithContext(bg, http.MethodGet, "http://example.test/api/chats/events", nil)
	if lastID != "" {
		r.Header.Set("Last-Event-ID", lastID)
	}
	return r
}
