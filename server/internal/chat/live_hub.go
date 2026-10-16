package chat

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// Живое обновление чата (срез 39, D-242…D-244). Hub — часть сервера, которая держит открытые потоки этой копии и
// раздаёт им события. События приходят из PostgreSQL (LISTEN/NOTIFY) от любой копии, в том числе от этой: у потока нет
// другого источника, поэтому сообщение, отправленное через соседнюю копию, доходит так же, как через эту.

// HubConfig — пределы и сроки потока. Нулевые поля заменяются значениями DefaultHubConfig.
type HubConfig struct {
	// MaxPerUser — открытых потоков на человека (вкладки браузера); следующий получает отказ, а не вытесняет старый.
	MaxPerUser int
	// MaxPerCopy — открытых потоков на копию сервера.
	MaxPerCopy int
	// QueueSize — сколько событий ждёт отправки одному потоку; переполнение закрывает поток (медленный читатель).
	QueueSize int
	// Ping — как часто в поток уходит строка-комментарий, чтобы соединение и посредники не засыпали.
	Ping time.Duration
	// Recheck — как часто поток проверяет, жива ли сессия и не сузились ли права.
	Recheck time.Duration
	// MaxAge — самое долгое время жизни потока (Caddy обрывает ответ через 5 минут, D-242); к нему добавляется разброс
	// до восьмой части, чтобы потоки, открытые вместе, не переподключались вместе.
	MaxAge time.Duration
	// WriteTimeout — срок одной записи в поток: читатель, который не читает, не держит горутину.
	WriteTimeout time.Duration
	// Poll — страховочный опрос журнала: события новее последнего обработанного.
	Poll time.Duration
	// UnreadEvery — как часто пересчитывается число непрочитанных у людей, которых коснулись события; UnreadGap — не
	// чаще одного пересчёта на человека за этот срок (у руководителя большой организации пересчёт читает тысячи
	// переписок, а сообщения у неё идут часто).
	UnreadEvery, UnreadGap time.Duration
	// Overlap — сколько до известного события берётся из журнала при добре и переподключении: порядок фиксации не
	// равен порядку номеров (D-243).
	Overlap time.Duration
	// ReplayLimit — не больше стольких событий досылается при переподключении, иначе сайт перечитывает списки сам.
	ReplayLimit int
	// Workers — сколько горутин раздаёт события (переписка всегда попадает к одной, порядок внутри неё сохраняется).
	Workers int
}

// DefaultHubConfig — значения в работе.
func DefaultHubConfig() HubConfig {
	return HubConfig{
		MaxPerUser: 5, MaxPerCopy: 3000, QueueSize: 32,
		Ping: 25 * time.Second, Recheck: 30 * time.Second,
		MaxAge: 4 * time.Minute, WriteTimeout: 15 * time.Second,
		Poll: 2 * time.Second, UnreadEvery: time.Second, UnreadGap: 5 * time.Second, Overlap: 15 * time.Second,
		ReplayLimit: 500, Workers: 4,
	}
}

func (c HubConfig) orDefaults() HubConfig {
	d := DefaultHubConfig()
	pos := func(v, fallback int) int {
		if v > 0 {
			return v
		}
		return fallback
	}
	dur := func(v, fallback time.Duration) time.Duration {
		if v > 0 {
			return v
		}
		return fallback
	}
	return HubConfig{
		MaxPerUser: pos(c.MaxPerUser, d.MaxPerUser), MaxPerCopy: pos(c.MaxPerCopy, d.MaxPerCopy), QueueSize: pos(c.QueueSize, d.QueueSize),
		Ping: dur(c.Ping, d.Ping), Recheck: dur(c.Recheck, d.Recheck), MaxAge: dur(c.MaxAge, d.MaxAge),
		WriteTimeout: dur(c.WriteTimeout, d.WriteTimeout),
		Poll:         dur(c.Poll, d.Poll), UnreadEvery: dur(c.UnreadEvery, d.UnreadEvery), UnreadGap: dur(c.UnreadGap, d.UnreadGap),
		Overlap:     dur(c.Overlap, d.Overlap),
		ReplayLimit: pos(c.ReplayLimit, d.ReplayLimit), Workers: pos(c.Workers, d.Workers),
	}
}

// Причины закрытия потока (метки метрик).
const (
	CloseClient   = "client"   // человек ушёл
	CloseSlow     = "slow"     // не успевает читать
	CloseRights   = "rights"   // права сузились
	CloseSession  = "session"  // сессия закончилась, аккаунт удалён
	CloseAge      = "age"      // срок жизни потока
	CloseShutdown = "shutdown" // остановка копии
)

// Причины отказа в потоке (метки метрик).
const (
	RejectUser = "user_limit"
	RejectCopy = "copy_limit"
	RejectShut = "shutdown"
	// RejectStart — раздача ещё не прочитала голову журнала (база недоступна при запуске): поток никому бы ничего не доставил.
	RejectStart = "starting"
)

// Ошибки открытия потока.
var (
	// ErrTooManyStreams — у человека уже открыто MaxPerUser потоков.
	ErrTooManyStreams = errors.New("chat: too many streams for the person")
	// ErrHubBusy — на копии открыто MaxPerCopy потоков.
	ErrHubBusy = errors.New("chat: too many streams on this copy")
	// ErrHubClosed — копия останавливается.
	ErrHubClosed = errors.New("chat: stream hub is closing")
)

// Observer принимает наблюдения за потоком (метрики); любой метод может вызываться из разных горутин.
type Observer interface {
	StreamOpened()
	StreamClosed(reason string, lived time.Duration)
	StreamRejected(reason string)
	// EventDispatched — копия обработала событие журнала (kind — вид); EventDelivered — один получатель получил его, late —
	// сколько прошло от записи события.
	EventDispatched(kind string)
	EventDelivered(kind string, late time.Duration)
	// ListenerState — соединение LISTEN поднято или упало.
	ListenerState(up bool)
	// CatchUp — сколько событий добрано после обрыва или страховочным опросом.
	CatchUp(n int)
}

type nopObserver struct{}

func (nopObserver) StreamOpened()                        {}
func (nopObserver) StreamClosed(string, time.Duration)   {}
func (nopObserver) StreamRejected(string)                {}
func (nopObserver) EventDispatched(string)               {}
func (nopObserver) EventDelivered(string, time.Duration) {}
func (nopObserver) ListenerState(bool)                   {}
func (nopObserver) CatchUp(int)                          {}

// frame — одно событие потока, уже собранное для получателя. id 0 — у события нет номера журнала (число непрочитанных,
// «печатает», состояние): оно не меняет Last-Event-ID на стороне сайта.
type frame struct {
	id   int64
	name string
	data []byte
	// at — когда событие записано в журнал; нулевое — задержку не считаем.
	at time.Time
}

// scope — где человек вправе вести отклики: организации целиком и отдельные подразделения (как в списках).
type scope struct {
	orgs  map[uuid.UUID]struct{}
	units map[uuid.UUID]struct{}
}

func scopeFrom(sc orgs.Scope) scope {
	out := scope{orgs: make(map[uuid.UUID]struct{}, len(sc.WholeOrgs)), units: make(map[uuid.UUID]struct{}, len(sc.Units))}
	for _, id := range sc.WholeOrgs {
		out.orgs[id] = struct{}{}
	}
	for _, id := range sc.Units {
		out.units[id] = struct{}{}
	}
	return out
}

// orgIDs и unitIDs — права в виде списков для запросов (не nil: запрос принимает пустой список).
func (s scope) orgIDs() []uuid.UUID  { return keys(s.orgs) }
func (s scope) unitIDs() []uuid.UUID { return keys(s.units) }

func keys(m map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// shrunkFrom — в новом наборе нет чего-то, что было в старом.
func (s scope) shrunkFrom(old scope) bool {
	for id := range old.orgs {
		if _, ok := s.orgs[id]; !ok {
			return true
		}
	}
	for id := range old.units {
		if _, ok := s.units[id]; !ok {
			return true
		}
	}
	return false
}

// stream — один открытый поток одного человека.
type stream struct {
	user    auth.User
	session uuid.UUID
	out     chan frame
	done    chan struct{}
	opened  time.Time

	once   sync.Once
	reason atomic.Value // string

	mu         sync.Mutex
	scope      scope
	lastUnread UnreadCount
	hasUnread  bool
}

func newStream(user auth.User, session uuid.UUID, sc scope, queue int, now time.Time) *stream {
	return &stream{user: user, session: session, scope: sc, out: make(chan frame, queue), done: make(chan struct{}), opened: now}
}

// close закрывает поток один раз; reason запоминается по первому закрытию.
func (st *stream) close(reason string) {
	st.once.Do(func() {
		st.reason.Store(reason)
		close(st.done)
	})
}

func (st *stream) closeReason() string {
	r, _ := st.reason.Load().(string)
	return r
}

// push кладёт событие в очередь потока, не ожидая; false — очередь полна.
func (st *stream) push(f frame) bool {
	select {
	case st.out <- f:
		return true
	default:
		return false
	}
}

func (st *stream) currentScope() scope {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.scope
}

// Hub — открытые потоки копии и раздача событий.
type Hub struct {
	svc *Service
	cfg HubConfig
	log *slog.Logger
	obs Observer
	// connCfg — как подключаться к базе для LISTEN (своё соединение, не из пула).
	connCfg *pgx.ConnConfig
	now     func() time.Time

	mu      sync.RWMutex
	closing bool
	all     map[*stream]struct{}
	byUser  map[uuid.UUID]map[*stream]struct{}
	byOrg   map[uuid.UUID]map[*stream]struct{}
	byUnit  map[uuid.UUID]map[*stream]struct{}
	dirty   map[uuid.UUID]struct{}

	seenMu sync.Mutex
	seen   map[int64]time.Time
	high   int64 // наибольший обработанный номер журнала; пишет только диспетчер

	started atomic.Bool
	// waiting — Run уже вызван, но голова журнала ещё не прочитана: события этой копии никому не раздаются, потоки
	// принимать нельзя (иначе человек держал бы поток, который молчит, а сайт не знал бы, что переподключаться).
	waiting atomic.Bool
}

// NewHub собирает раздачу. connCfg — настройки соединения LISTEN (обычно из настроек пула); obs может быть nil.
func NewHub(svc *Service, connCfg *pgx.ConnConfig, cfg HubConfig, logger *slog.Logger, obs Observer) *Hub {
	if obs == nil {
		obs = nopObserver{}
	}
	var cc *pgx.ConnConfig
	if connCfg != nil {
		cc = connCfg.Copy()
		cc.Tracer = nil // трассировщик пула считает запросы; вечное ожидание уведомления запросом не является
	}
	return &Hub{
		svc: svc, cfg: cfg.orDefaults(), log: logger, obs: obs, connCfg: cc, now: func() time.Time { return time.Now().UTC() },
		all: map[*stream]struct{}{}, byUser: map[uuid.UUID]map[*stream]struct{}{}, byOrg: map[uuid.UUID]map[*stream]struct{}{},
		byUnit: map[uuid.UUID]map[*stream]struct{}{}, dirty: map[uuid.UUID]struct{}{}, seen: map[int64]time.Time{},
	}
}

// Config возвращает действующие настройки.
func (h *Hub) Config() HubConfig { return h.cfg }

// Streams — сколько потоков открыто сейчас.
func (h *Hub) Streams() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.all)
}

// scopeOf читает, где человек ведёт отклики: решает пакет access.
func (h *Hub) scopeOf(ctx context.Context, userID uuid.UUID) (scope, error) {
	sc, err := orgs.ScopeOf(ctx, h.svc.q, userID, access.ViewApplications)
	if err != nil {
		return scope{}, err
	}
	return scopeFrom(sc), nil
}

// open регистрирует поток человека: проверяет пределы, читает его права. Закрыть поток надо через remove.
func (h *Hub) open(ctx context.Context, user auth.User, session uuid.UUID) (*stream, error) {
	// Пределы проверяются до работы с базой: переполненная копия или человек с кучей вкладок не должны ещё и нагружать её.
	if err := h.admit(user.ID); err != nil {
		return nil, err
	}
	sc, err := h.scopeOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	st := newStream(user, session, sc, h.cfg.QueueSize, h.now())
	h.mu.Lock()
	defer h.mu.Unlock()
	// Между проверкой и регистрацией мог открыться ещё поток: проверка повторяется под замком.
	if err := h.admitLocked(user.ID); err != nil {
		return nil, err
	}
	h.all[st] = struct{}{}
	index(h.byUser, user.ID, st)
	h.indexScope(st, sc)
	h.obs.StreamOpened()
	return st, nil
}

// admit проверяет, можно ли человеку ещё один поток.
func (h *Hub) admit(user uuid.UUID) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.admitLocked(user)
}

// admitLocked — admit при уже взятом замке; отказ записывается в метрики.
func (h *Hub) admitLocked(user uuid.UUID) error {
	switch {
	case h.closing:
		h.obs.StreamRejected(RejectShut)
		return ErrHubClosed
	case h.waiting.Load():
		h.obs.StreamRejected(RejectStart)
		return ErrHubBusy
	case len(h.all) >= h.cfg.MaxPerCopy:
		h.obs.StreamRejected(RejectCopy)
		return ErrHubBusy
	case len(h.byUser[user]) >= h.cfg.MaxPerUser:
		h.obs.StreamRejected(RejectUser)
		return ErrTooManyStreams
	}
	return nil
}

func index(m map[uuid.UUID]map[*stream]struct{}, key uuid.UUID, st *stream) {
	set := m[key]
	if set == nil {
		set = map[*stream]struct{}{}
		m[key] = set
	}
	set[st] = struct{}{}
}

func unindex(m map[uuid.UUID]map[*stream]struct{}, key uuid.UUID, st *stream) {
	if set := m[key]; set != nil {
		delete(set, st)
		if len(set) == 0 {
			delete(m, key)
		}
	}
}

// indexScope заносит поток в указатели организаций и подразделений (под замком h.mu).
func (h *Hub) indexScope(st *stream, sc scope) {
	for id := range sc.orgs {
		index(h.byOrg, id, st)
	}
	for id := range sc.units {
		index(h.byUnit, id, st)
	}
}

func (h *Hub) unindexScope(st *stream, sc scope) {
	for id := range sc.orgs {
		unindex(h.byOrg, id, st)
	}
	for id := range sc.units {
		unindex(h.byUnit, id, st)
	}
}

// setScope заменяет права потока.
func (h *Hub) setScope(st *stream, sc scope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.all[st]; !ok {
		return
	}
	st.mu.Lock()
	old := st.scope
	st.scope = sc
	st.mu.Unlock()
	h.unindexScope(st, old)
	h.indexScope(st, sc)
}

// remove снимает поток с раздачи.
func (h *Hub) remove(st *stream) {
	h.mu.Lock()
	_, ok := h.all[st]
	if ok {
		delete(h.all, st)
		unindex(h.byUser, st.user.ID, st)
		h.unindexScope(st, st.currentScope())
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	reason := st.closeReason()
	if reason == "" {
		reason = CloseClient
	}
	h.obs.StreamClosed(reason, h.now().Sub(st.opened))
}

// Close закрывает все потоки и не принимает новые: вызывается перед остановкой HTTP-сервера, иначе тот ждал бы все
// потоки до срока остановки.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closing = true
	all := make([]*stream, 0, len(h.all))
	for st := range h.all {
		all = append(all, st)
	}
	h.mu.Unlock()
	for _, st := range all {
		st.close(CloseShutdown)
	}
}

// target — получатель события: поток и его сторона в переписке.
type target struct {
	st   *stream
	side Side
}

// recipients выбирает потоки, которым событие положено по правам: учёному переписки (все его потоки) и сотрудникам,
// у которых в правах организация или подразделение вакансии. Человек, который сам учёный этой переписки, участвует как
// учёный, даже если работает в той же организации.
func (h *Hub) recipients(ev event) []target {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]target, 0, len(h.byUser[ev.UserID])+len(h.byOrg[ev.OrgID]))
	for st := range h.byUser[ev.UserID] {
		out = append(out, target{st: st, side: SideScientist})
	}
	for st := range h.byOrg[ev.OrgID] {
		if st.user.ID != ev.UserID {
			out = append(out, target{st: st, side: SideOrg})
		}
	}
	if ev.UnitID != nil {
		for st := range h.byUnit[*ev.UnitID] {
			// Права по подразделению и по всей организации у одного человека не пересекаются (orgs.ScopeOf кладёт
			// подразделение, только если организация целиком ему не положена), поэтому дублей нет.
			if st.user.ID != ev.UserID {
				out = append(out, target{st: st, side: SideOrg})
			}
		}
	}
	return out
}

// interested — есть ли у копии хоть один поток, которому событие может быть нужно.
func (h *Hub) interested(ev event) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.byUser[ev.UserID]) > 0 || len(h.byOrg[ev.OrgID]) > 0 {
		return true
	}
	return ev.UnitID != nil && len(h.byUnit[*ev.UnitID]) > 0
}

// markDirty отмечает людей, у которых после события надо пересчитать число непрочитанных.
func (h *Hub) markDirty(ids ...uuid.UUID) {
	h.mu.Lock()
	for _, id := range ids {
		h.dirty[id] = struct{}{}
	}
	h.mu.Unlock()
}

// ---- раздача ----

// note — то, что слушатель передаёт диспетчеру.
type note struct {
	event  *event
	typing *typingNote
	// resync — соединение LISTEN поднято (в первый раз или после обрыва): надо добрать журнал.
	resync bool
}

// work — задание рабочей горутине раздачи.
type work struct {
	event  *event
	typing *typingNote
}

// Run раздаёт события, пока ctx не отменён. Вызывается один раз на копию, в `serve`.
func (h *Hub) Run(ctx context.Context) error {
	if !h.started.CompareAndSwap(false, true) {
		return errors.New("chat: hub already running")
	}
	// Голова журнала читается, пока база не ответит: сбой при запуске (база занята, перезапускается, срок запроса) не должен
	// навсегда оставлять копию с потоками, которые ничего не получают (находка итогового аудита, T-120).
	h.waiting.Store(true)
	head, ok := h.readHead(ctx)
	if !ok {
		return nil
	}
	h.high = head
	h.waiting.Store(false)
	notes := make(chan note, 256)
	shards := make([]chan work, h.cfg.Workers)
	var wg sync.WaitGroup
	for i := range shards {
		shards[i] = make(chan work, 128)
		wg.Go(func() { h.worker(ctx, shards[i]) })
	}
	wg.Go(func() { h.listen(ctx, notes) })
	wg.Go(func() { h.flushUnread(ctx) })
	h.dispatch(ctx, notes, shards)
	wg.Wait()
	return nil
}

// readHead читает наибольший номер журнала с повторами до успеха; false — остановка раньше, чем база ответила.
func (h *Hub) readHead(ctx context.Context) (int64, bool) {
	backoff := time.Second
	for {
		head, err := h.svc.q.ChatEventHead(ctx)
		if err == nil {
			return head, true
		}
		if ctx.Err() != nil {
			return 0, false
		}
		h.log.Warn("chat hub cannot read the journal head, retrying", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return 0, false
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

// listen держит соединение LISTEN: при обрыве подключается заново и просит диспетчера добрать журнал.
func (h *Hub) listen(ctx context.Context, notes chan<- note) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listenOnce(ctx, notes)
		h.obs.ListenerState(false)
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("chat listener lost", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

func (h *Hub) listenOnce(ctx context.Context, notes chan<- note) error {
	if h.connCfg == nil {
		return errors.New("no connection settings")
	}
	conn, err := pgx.ConnectConfig(ctx, h.connCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	for _, ch := range []string{channelEvents, channelTyping} {
		if _, err := conn.Exec(ctx, "LISTEN "+ch); err != nil {
			return fmt.Errorf("listen %s: %w", ch, err)
		}
	}
	h.obs.ListenerState(true)
	if !h.send(ctx, notes, note{resync: true}) {
		return ctx.Err()
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var out note
		switch n.Channel {
		case channelEvents:
			ev, err := parseEvent(n.Payload)
			if err != nil {
				h.log.Warn("chat event skipped", "err", err)
				continue
			}
			out.event = &ev
		case channelTyping:
			t, err := parseTyping(n.Payload)
			if err != nil {
				h.log.Warn("chat typing skipped", "err", err)
				continue
			}
			out.typing = &t
		default:
			continue
		}
		if !h.send(ctx, notes, out) {
			return ctx.Err()
		}
	}
}

func (h *Hub) send(ctx context.Context, notes chan<- note, n note) bool {
	select {
	case notes <- n:
		return true
	case <-ctx.Done():
		return false
	}
}

// seenBefore запоминает номер и сообщает, видели ли его.
func (h *Hub) seenBefore(id int64, now time.Time) bool {
	h.seenMu.Lock()
	defer h.seenMu.Unlock()
	if _, ok := h.seen[id]; ok {
		return true
	}
	h.seen[id] = now
	return false
}

// seenKeep — сколько хранятся номера обработанных событий: дольше окна добора они не нужны.
const seenKeep = 5 * time.Minute

func (h *Hub) pruneSeen(now time.Time) {
	h.seenMu.Lock()
	defer h.seenMu.Unlock()
	for id, at := range h.seen {
		if now.Sub(at) > seenKeep {
			delete(h.seen, id)
		}
	}
}

// dispatch принимает уведомления и раздаёт рабочим. Он единственный, кто ведёт seen и high.
func (h *Hub) dispatch(ctx context.Context, notes <-chan note, shards []chan work) {
	poll := time.NewTicker(h.cfg.Poll)
	defer poll.Stop()
	prune := time.NewTicker(time.Minute)
	defer prune.Stop()
	defer func() {
		for _, ch := range shards {
			close(ch)
		}
	}()
	// lastOK — когда копия последний раз точно видела журнал: от этого момента (с запасом на порядок фиксации) добирается
	// всё после обрыва соединения LISTEN.
	lastOK := h.now()
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-notes:
			switch {
			case n.resync:
				if h.catchUp(ctx, shards, lastOK.Add(-h.cfg.Overlap)) {
					lastOK = h.now()
				}
			case n.typing != nil:
				h.route(ctx, shards, work{typing: n.typing}, n.typing.ChatID)
			case n.event != nil:
				h.accept(ctx, shards, *n.event)
			}
		case <-poll.C:
			if h.pollNew(ctx, shards) {
				lastOK = h.now()
			}
		case <-prune.C:
			h.pruneSeen(h.now())
		}
	}
}

// accept отдаёт событие рабочему, если оно новое и нужно этой копии.
func (h *Hub) accept(ctx context.Context, shards []chan work, ev event) {
	if h.seenBefore(ev.ID, h.now()) {
		return
	}
	if ev.ID > h.high {
		h.high = ev.ID
	}
	if !h.interested(ev) {
		return
	}
	h.route(ctx, shards, work{event: &ev}, ev.ChatID)
}

func (h *Hub) route(ctx context.Context, shards []chan work, w work, chat uuid.UUID) {
	f := fnv.New32a()
	_, _ = f.Write(chat[:])
	select {
	case shards[int(f.Sum32())%len(shards)] <- w:
	case <-ctx.Done():
	}
}

// batchLimit — сколько событий читается за одно обращение к журналу при добре.
const batchLimit = 5000

// catchUp берёт из журнала события с момента since и раздаёт те, которых копия ещё не видела. false — не удалось.
func (h *Hub) catchUp(ctx context.Context, shards []chan work, since time.Time) bool {
	rows, err := h.svc.q.ListChatEventsSince(ctx, dbgen.ListChatEventsSinceParams{Since: since, RowLimit: batchLimit})
	if err != nil {
		if ctx.Err() == nil {
			h.log.Warn("chat catch-up failed", "err", err)
		}
		return false
	}
	n := 0
	for _, r := range rows {
		if h.seenBefore(r.ID, h.now()) {
			continue
		}
		n++
		ev := eventOf(r)
		if ev.ID > h.high {
			h.high = ev.ID
		}
		if h.interested(ev) {
			h.route(ctx, shards, work{event: &ev}, ev.ChatID)
		}
	}
	if n > 0 {
		h.obs.CatchUp(n)
	}
	return true
}

// pollNew — страховочный опрос: события новее последнего обработанного, если уведомление почему-то не пришло. false —
// не удалось прочитать журнал.
func (h *Hub) pollNew(ctx context.Context, shards []chan work) bool {
	rows, err := h.svc.q.ListChatEventsAfter(ctx, dbgen.ListChatEventsAfterParams{After: h.high, RowLimit: batchLimit})
	if err != nil {
		if ctx.Err() == nil {
			h.log.Warn("chat poll failed", "err", err)
		}
		return false
	}
	n := 0
	for _, r := range rows {
		if h.seenBefore(r.ID, h.now()) {
			continue
		}
		n++
		ev := eventOf(r)
		if ev.ID > h.high {
			h.high = ev.ID
		}
		if h.interested(ev) {
			h.route(ctx, shards, work{event: &ev}, ev.ChatID)
		}
	}
	if n > 0 {
		h.obs.CatchUp(n)
	}
	return true
}

func (h *Hub) worker(ctx context.Context, in <-chan work) {
	for w := range in {
		switch {
		case w.event != nil:
			h.deliver(ctx, *w.event)
		case w.typing != nil:
			h.deliverTyping(ctx, *w.typing)
		}
	}
}

// flushUnread пересчитывает число непрочитанных у людей, которых коснулись события, и шлёт его потокам, если оно
// изменилось. Пачки событий одного человека схлопываются в один пересчёт за UnreadEvery.
func (h *Hub) flushUnread(ctx context.Context) {
	tick := time.NewTicker(h.cfg.UnreadEvery)
	defer tick.Stop()
	last := map[uuid.UUID]time.Time{} // когда человеку считали в последний раз
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := h.now()
		if len(last) > unreadMemory {
			for id, at := range last {
				if now.Sub(at) >= h.cfg.UnreadGap {
					delete(last, id)
				}
			}
		}
		h.mu.Lock()
		batch := make([]uuid.UUID, 0, min(len(h.dirty), unreadPerTick))
		for id := range h.dirty {
			if at, ok := last[id]; ok && now.Sub(at) < h.cfg.UnreadGap {
				continue // считали недавно: остаётся в очереди до следующего такта
			}
			batch = append(batch, id)
			delete(h.dirty, id)
			last[id] = now
			if len(batch) >= unreadPerTick {
				break
			}
		}
		h.mu.Unlock()
		for _, id := range batch {
			h.refreshUnread(ctx, id)
		}
	}
}

// unreadPerTick — сколько людей пересчитывается за один такт; остальные ждут следующего. unreadMemory — сколько пометок
// «считали недавно» копится, прежде чем устаревшие выбрасываются.
const (
	unreadPerTick = 200
	unreadMemory  = 4096
)

// refreshUnread считает число непрочитанных человека и отправляет его его потокам, если оно изменилось.
func (h *Hub) refreshUnread(ctx context.Context, userID uuid.UUID) {
	h.mu.RLock()
	streams := make([]*stream, 0, len(h.byUser[userID]))
	for st := range h.byUser[userID] {
		streams = append(streams, st)
	}
	h.mu.RUnlock()
	if len(streams) == 0 {
		return
	}
	counts, err := h.svc.UnreadCount(ctx, streams[0].user)
	if err != nil {
		if ctx.Err() == nil {
			h.log.Warn("chat unread count failed", "err", err)
		}
		return
	}
	for _, st := range streams {
		st.mu.Lock()
		same := st.hasUnread && st.lastUnread == counts
		st.lastUnread, st.hasUnread = counts, true
		st.mu.Unlock()
		if same {
			continue
		}
		h.offer(st, primitiveFrame(0, "unread.count", counts, time.Time{}))
	}
}

// offer кладёт событие в очередь потока; переполненную очередь закрывает: сайт переподключится и добёрет журнал.
func (h *Hub) offer(st *stream, f frame) {
	if !st.push(f) {
		st.close(CloseSlow)
	}
}
