package chat

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/apierr"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
)

// Коды ошибок потока.
const (
	CodeStreamLimit = "stream_limit" // у человека открыто слишком много потоков (429)
	CodeStreamBusy  = "stream_busy"  // копия занята или останавливается (503)
	messageStreams  = "Открыто слишком много окон с перепиской. Закройте лишние и подождите немного"
	messageBusy     = "Живое обновление сейчас недоступно. Страница попробует снова"
	retryUser       = 30
	retryBusy       = 10
)

// WithHub подключает живой поток к обработчикам. Без него адрес потока отвечает 404 (как отвечал до среза 39: сайт
// такой ответ переживает). onStream вызывается, когда ответ стал потоком: метрики не считают его обычным запросом.
func (h *Handler) WithHub(hub *Hub, onStream func(context.Context)) *Handler {
	h.hub, h.onStream = hub, onStream
	return h
}

// events — GET /api/chats/events: живой поток событий своих переписок (Server-Sent Events, D-242).
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		h.notFound(w, r)
		return
	}
	p, _ := auth.FromContext(r.Context()) // вход проверен requireUser
	st, err := h.hub.open(r.Context(), p.User, p.SessionID)
	switch {
	case err == nil:
	case errors.Is(err, ErrTooManyStreams):
		w.Header().Set("Retry-After", strconv.Itoa(retryUser))
		apierr.WriteError(w, http.StatusTooManyRequests, CodeStreamLimit, messageStreams)
		return
	case errors.Is(err, ErrHubBusy), errors.Is(err, ErrHubClosed):
		w.Header().Set("Retry-After", strconv.Itoa(retryBusy))
		apierr.WriteError(w, http.StatusServiceUnavailable, CodeStreamBusy, messageBusy)
		return
	default:
		h.fail(w, r, err)
		return
	}
	defer h.hub.remove(st)
	if h.onStream != nil {
		h.onStream(r.Context())
	}
	h.hub.serve(w, r, st)
}

// lastEventID читает заголовок Last-Event-ID; 0 — его нет или он негоден.
func lastEventID(r *http.Request) int64 {
	n, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// serve ведёт поток до конца: сначала число непрочитанных и пропущенное, затем живые события.
func (h *Hub) serve(w http.ResponseWriter, r *http.Request, st *stream) {
	ctx := r.Context()
	rc := http.NewResponseController(w)
	// Срок чтения у потока снимается (читать нечего). Проверено тестом `TestStreamOutlivesServerTimeouts`: решает срок записи,
	// WriteTimeout сервера оборвал бы поток через минуту, поэтому каждая запись идёт под своим сроком (D-242).
	_ = rc.SetReadDeadline(time.Time{})
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream; charset=utf-8")
	hd.Set("Cache-Control", "no-cache, no-transform")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	write := func(b []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(h.cfg.WriteTimeout))
		if _, err := w.Write(b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	send := func(f frame) bool {
		if !write(f.wire()) {
			return false
		}
		if !f.at.IsZero() {
			h.obs.EventDelivered(f.name, max(0, h.now().Sub(f.at)))
		}
		return true
	}
	if !write([]byte("retry: 2000\n\n")) {
		return
	}

	// Число непрочитанных всегда первым событием: после переподключения оно же говорит сайту «перечитай списки».
	if counts, err := h.svc.UnreadCount(ctx, st.user); err == nil {
		st.mu.Lock()
		st.lastUnread, st.hasUnread = counts, true
		st.mu.Unlock()
		if !send(primitiveFrame(0, "unread.count", counts, time.Time{})) {
			return
		}
	} else if ctx.Err() == nil {
		h.log.Warn("chat stream: unread count", "err", err)
	}

	sent := map[int64]struct{}{}
	if last := lastEventID(r); last > 0 {
		if !h.replay(ctx, st, last, send, sent) {
			return
		}
	}

	ping := time.NewTicker(h.cfg.Ping)
	defer ping.Stop()
	recheck := time.NewTicker(h.cfg.Recheck + jitter(h.cfg.Recheck/5))
	defer recheck.Stop()
	age := time.NewTimer(h.cfg.MaxAge + jitter(h.cfg.MaxAge/8))
	defer age.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-st.done:
			return
		case f := <-st.out:
			if f.id > 0 {
				if _, dup := sent[f.id]; dup {
					delete(sent, f.id)
					continue
				}
			}
			if !send(f) {
				return
			}
		case <-ping.C:
			if !write([]byte(": ping\n\n")) {
				return
			}
		case <-recheck.C:
			if !h.recheck(ctx, st) {
				return
			}
		case <-age.C:
			st.close(CloseAge)
			return
		}
	}
}

// jitter — случайная добавка до limit (0 — без добавки): потоки, открытые вместе, не переподключаются вместе.
func jitter(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(limit))) //nolint:gosec // G404: разброс срока соединения, не защита
}

// replay досылает потоку события журнала, которые он мог пропустить после события last. Если пропущено больше
// ReplayLimit или last уже вышло из журнала, ничего не досылается: сайт перечитает списки сам по unread.count.
func (h *Hub) replay(ctx context.Context, st *stream, last int64, send func(frame) bool, sent map[int64]struct{}) bool {
	at, err := h.svc.q.GetChatEventTime(ctx, last)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) && ctx.Err() == nil {
			h.log.Warn("chat stream: replay anchor", "err", err)
		}
		return true
	}
	sc := st.currentScope()
	rows, err := h.svc.q.ListChatEventsForReplay(ctx, dbgen.ListChatEventsForReplayParams{
		Since: at.Add(-h.cfg.Overlap), UserID: st.user.ID, Orgs: sc.orgIDs(), Units: sc.unitIDs(), RowLimit: num.Int32(h.cfg.ReplayLimit + 1),
	})
	if err != nil {
		if ctx.Err() == nil {
			h.log.Warn("chat stream: replay", "err", err)
		}
		return true
	}
	if len(rows) > h.cfg.ReplayLimit {
		return true
	}
	for _, r := range rows {
		ev := eventOf(r)
		side, ok := sideFor(ev, st.user.ID, sc)
		if !ok {
			continue
		}
		per, _ := h.build(ctx, ev, []target{{st: st, side: side}})
		for _, f := range per[0] {
			if f.id > 0 {
				sent[f.id] = struct{}{}
			}
			if !send(f) {
				return false
			}
		}
	}
	return true
}

// recheck проверяет сессию и права потока; false — поток закрыт. Ошибка проверки (база недоступна) поток не рвёт.
func (h *Hub) recheck(ctx context.Context, st *stream) bool {
	alive, err := h.svc.q.ChatStreamSessionAlive(ctx, dbgen.ChatStreamSessionAliveParams{SessionID: st.session, UserID: st.user.ID, Now: h.now()})
	if err == nil && !alive {
		st.close(CloseSession)
		return false
	}
	sc, err := h.scopeOf(ctx, st.user.ID)
	if err != nil {
		return true
	}
	if sc.shrunkFrom(st.currentScope()) {
		st.close(CloseRights)
		return false
	}
	h.setScope(st, sc)
	return true
}
