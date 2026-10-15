// Команда chatload — нагрузка живого потока чата (срез 39, D-242…D-244): открывает тысячи потоков на копиях сервера,
// пишет сообщения с заданной скоростью и измеряет время отправки и доставки собеседнику. Нужна база с переписками
// (`make load-data`) и запущенные копии сервера; запускает её scripts/load-test.
//
// Люди берутся из базы: для каждой пары «учёный, владелец организации» одной открытой переписки заводятся сеансы с
// известными токенами (user_agent = 'chatload', убираются при выходе). Каждый человек открывает один поток; сообщения
// пишутся то от учёного, то от владельца, текст несёт метку времени, по ней у получателя считается доставка.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionAgent = "chatload"

type pair struct {
	chat       uuid.UUID
	scientist  uuid.UUID
	staff      uuid.UUID
	sciToken   string
	staffToken string
}

type result struct {
	Streams       int     `json:"streams_target"`
	Opened        int64   `json:"streams_opened"`
	OpenFailures  int64   `json:"stream_open_failures"`
	Reconnects    int64   `json:"stream_reconnects"`
	Sent          int64   `json:"messages_sent"`
	SendFailures  int64   `json:"send_failures"`
	Throttled     int64   `json:"send_throttled"`
	Delivered     int64   `json:"messages_delivered"`
	DeliveryRatio float64 `json:"delivery_ratio"`
	Events        int64   `json:"events_received"`
	SendP50       float64 `json:"send_p50_ms"`
	SendP95       float64 `json:"send_p95_ms"`
	SendP99       float64 `json:"send_p99_ms"`
	DelivP50      float64 `json:"delivery_p50_ms"`
	DelivP95      float64 `json:"delivery_p95_ms"`
	DelivP99      float64 `json:"delivery_p99_ms"`
	DelivMax      float64 `json:"delivery_max_ms"`
	RateWanted    float64 `json:"rate_wanted_per_s"`
	RateAchieved  float64 `json:"rate_achieved_per_s"`
	Seconds       float64 `json:"seconds"`
}

func main() {
	dbURL := flag.String("db", "postgres://scibox:scibox@localhost:5433/scibox_load?sslmode=disable", "база с переписками")
	basesFlag := flag.String("bases", "http://127.0.0.1:8091,http://127.0.0.1:8092", "копии сервера через запятую")
	streams := flag.Int("streams", 5000, "сколько потоков держать открытыми")
	tabs := flag.Int("tabs", 1, "сколько потоков (вкладок) открывает каждый человек, не больше предела сервера на человека")
	rate := flag.Float64("rate", 50, "сообщений в секунду")
	duration := flag.Duration("duration", time.Minute, "сколько писать")
	ramp := flag.Int("ramp", 250, "сколько потоков открывать в секунду")
	out := flag.String("out", "", "куда записать итог JSON")
	flag.Parse()

	os.Exit(execute(*dbURL, strings.Split(*basesFlag, ","), *streams, *tabs, *rate, *duration, *ramp, *out))
}

// execute запускает нагрузку и возвращает код выхода (отдельно от main, чтобы defer успели отработать).
func execute(dbURL string, bases []string, streams, tabs int, rate float64, duration time.Duration, ramp int, out string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, dbURL, bases, streams, tabs, rate, duration, ramp, out); err != nil {
		fmt.Fprintln(os.Stderr, "chatload:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, dbURL string, bases []string, streams, tabs int, rate float64, duration time.Duration, ramp int, outPath string) error {
	tabs = max(tabs, 1)
	wantPairs := (streams + 2*tabs - 1) / (2 * tabs)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	pairs, err := pickPairs(ctx, pool, wantPairs)
	if err != nil {
		return err
	}
	if len(pairs) < wantPairs {
		fmt.Fprintf(os.Stderr, "в базе нашлось только %d подходящих пар вместо %d\n", len(pairs), wantPairs)
	}
	if err := openSessions(ctx, pool, pairs); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, `DELETE FROM sessions WHERE user_agent = $1`, sessionAgent); err != nil {
			fmt.Fprintln(os.Stderr, "не удалось убрать сеансы:", err)
		}
	}()

	tr := &http.Transport{MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second, DisableCompression: true}
	client := &http.Client{Transport: tr}
	st := &stats{}
	var wg sync.WaitGroup
	streamCtx, cancelStreams := context.WithCancel(ctx)
	defer cancelStreams()

	// Потоки: два на пару, копии по кругу, открываются с заданной скоростью.
	tick := time.NewTicker(time.Second / time.Duration(max(ramp, 1)))
	defer tick.Stop()
	n := 0
	for _, p := range pairs {
		for _, token := range []string{p.sciToken, p.staffToken} {
			for range tabs {
				<-tick.C
				base := bases[n%len(bases)]
				n++
				wg.Go(func() { st.follow(streamCtx, client, base, token) })
			}
		}
	}
	fmt.Printf("потоков запрошено: %d, ждём подключения…\n", n)
	if !waitOpen(ctx, st, int64(n), 60*time.Second) {
		fmt.Printf("открылось %d из %d потоков\n", st.opened.Load(), n)
	}

	// Сообщения.
	started := time.Now()
	sendCtx, cancelSend := context.WithTimeout(ctx, duration)
	defer cancelSend()
	jobs := make(chan struct{}, 4096)
	var senders sync.WaitGroup
	for range 64 {
		senders.Go(func() {
			for range jobs {
				p := pairs[rand.IntN(len(pairs))]             //nolint:gosec // G404: случайный выбор для нагрузки, не защита
				st.send(sendCtx, client, bases[rand.IntN(len(bases))], p) //nolint:gosec // G404: то же
			}
		})
	}
	interval := time.Duration(float64(time.Second) / rate)
	sched := time.NewTicker(max(interval, time.Microsecond))
loop:
	for {
		select {
		case <-sendCtx.Done():
			break loop
		case <-sched.C:
			select {
			case jobs <- struct{}{}:
			default: // отправители не успевают: сообщение пропущено, это видно по фактической скорости
			}
		}
	}
	sched.Stop()
	close(jobs)
	senders.Wait()
	elapsed := time.Since(started)
	// Доставка последних сообщений.
	time.Sleep(3 * time.Second)
	cancelStreams()
	wg.Wait()

	res := st.summary(streams, rate, elapsed)
	printResult(res)
	if outPath != "" {
		raw, _ := json.MarshalIndent(res, "", " ")
		if err := os.WriteFile(outPath, append(raw, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// pickPairs выбирает открытые переписки, у которых учёный и владелец организации не повторяются (по человеку один поток).
func pickPairs(ctx context.Context, pool *pgxpool.Pool, want int) ([]pair, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.user_id, m.user_id
		FROM chats c
		JOIN applications a ON a.id = c.application_id AND a.status IN ('sent', 'viewed', 'invited')
		JOIN org_members m ON m.org_id = c.org_id AND m.role = 'owner'
		WHERE c.user_id <> m.user_id
		  AND NOT EXISTS (SELECT 1 FROM org_members x WHERE x.user_id = c.user_id)
		  AND NOT EXISTS (SELECT 1 FROM chat_blocks b WHERE b.user_id = c.user_id AND b.org_id = c.org_id)
		ORDER BY random()
		LIMIT $1`, want*6)
	if err != nil {
		return nil, fmt.Errorf("pairs: %w", err)
	}
	defer rows.Close()
	usedSci, usedStaff := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	var out []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.chat, &p.scientist, &p.staff); err != nil {
			return nil, err
		}
		if usedSci[p.scientist] || usedStaff[p.staff] || usedSci[p.staff] || usedStaff[p.scientist] {
			continue
		}
		usedSci[p.scientist], usedStaff[p.staff] = true, true
		p.sciToken = "cl-s-" + strconv.Itoa(len(out))
		p.staffToken = "cl-o-" + strconv.Itoa(len(out))
		out = append(out, p)
		if len(out) == want {
			break
		}
	}
	return out, rows.Err()
}

func openSessions(ctx context.Context, pool *pgxpool.Pool, pairs []pair) error {
	if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE user_agent = $1`, sessionAgent); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	now := time.Now().UTC()
	add := func(user uuid.UUID, token string) {
		sum := sha256.Sum256([]byte(token))
		batch.Queue(`INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip)
			VALUES ($1, $2, $3, $3, $4, $5, '10.9.9.9')`, user, sum[:], now, now.Add(6*time.Hour), sessionAgent)
	}
	for _, p := range pairs {
		add(p.scientist, p.sciToken)
		add(p.staff, p.staffToken)
	}
	res := pool.SendBatch(ctx, batch)
	defer func() { _ = res.Close() }()
	for range batch.Len() {
		if _, err := res.Exec(); err != nil {
			return fmt.Errorf("sessions: %w", err)
		}
	}
	return nil
}

func waitOpen(ctx context.Context, st *stats, want int64, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if st.opened.Load() >= want {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return st.opened.Load() >= want
}

type stats struct {
	opened, openFail, reconnects, sent, sendFail, throttled, delivered, events atomic.Int64

	mu       sync.Mutex
	sendMs   []float64
	delivMs  []float64
	delivMax float64
}

// follow держит поток открытым, как сайт: после обрыва переподключается с Last-Event-ID.
func (s *stats) follow(ctx context.Context, c *http.Client, base, token string) {
	lastID := ""
	first := true
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/chats/events", nil)
		if err != nil {
			return
		}
		req.Header.Set("Cookie", "scibox_session="+token)
		req.Header.Set("Accept", "text/event-stream")
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		resp, err := c.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				_ = resp.Body.Close()
			}
			if ctx.Err() != nil {
				return
			}
			s.openFail.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second + time.Duration(rand.IntN(1000))*time.Millisecond): //nolint:gosec // G404: разброс паузы, не защита
			}
			continue
		}
		if first {
			first = false
			s.opened.Add(1)
		} else {
			s.reconnects.Add(1)
		}
		lastID = s.read(resp.Body, lastID)
		_ = resp.Body.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// read читает события до конца потока и возвращает последний номер.
func (s *stats) read(body io.Reader, lastID string) string {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var name, id, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if name != "" {
				s.events.Add(1)
				if id != "" {
					lastID = id
				}
				if name == "message.created" {
					s.delivery(data)
				}
			}
			name, id, data = "", "", ""
		case strings.HasPrefix(line, "id: "):
			id = line[4:]
		case strings.HasPrefix(line, "event: "):
			name = line[7:]
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		}
	}
	return lastID
}

// delivery считает доставку собеседнику: сообщение нагрузки несёт время отправки, свою копию (is_me) не считаем.
func (s *stats) delivery(data string) {
	var ev struct {
		Message struct {
			Text   string `json:"text"`
			Author *struct {
				IsMe bool `json:"is_me"`
			} `json:"author"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil || ev.Message.Author == nil || ev.Message.Author.IsMe {
		return
	}
	rest, ok := strings.CutPrefix(ev.Message.Text, "load:")
	if !ok {
		return
	}
	ns, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return
	}
	ms := float64(time.Now().UnixNano()-ns) / 1e6
	s.delivered.Add(1)
	s.mu.Lock()
	s.delivMs = append(s.delivMs, ms)
	s.delivMax = max(s.delivMax, ms)
	s.mu.Unlock()
}

func (s *stats) send(ctx context.Context, c *http.Client, base string, p pair) {
	token := p.sciToken
	if rand.IntN(2) == 0 { //nolint:gosec // G404: случайный выбор стороны для нагрузки, не защита
		token = p.staffToken
	}
	t0 := time.Now()
	body, _ := json.Marshal(map[string]string{"text": "load:" + strconv.FormatInt(t0.UnixNano(), 10), "client_id": uuid.NewString()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/chats/"+p.chat.String()+"/messages", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Cookie", "scibox_session="+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.sendFail.Add(1)
		}
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	took := float64(time.Since(t0)) / 1e6
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		s.sent.Add(1)
		s.mu.Lock()
		s.sendMs = append(s.sendMs, took)
		s.mu.Unlock()
	case http.StatusTooManyRequests:
		s.throttled.Add(1)
	default:
		s.sendFail.Add(1)
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

func (s *stats) summary(streams int, rate float64, elapsed time.Duration) result {
	s.mu.Lock()
	defer s.mu.Unlock()
	slices.Sort(s.sendMs)
	slices.Sort(s.delivMs)
	r := result{
		Streams: streams, Opened: s.opened.Load(), OpenFailures: s.openFail.Load(), Reconnects: s.reconnects.Load(),
		Sent: s.sent.Load(), SendFailures: s.sendFail.Load(), Throttled: s.throttled.Load(), Delivered: s.delivered.Load(), Events: s.events.Load(),
		SendP50: percentile(s.sendMs, 0.50), SendP95: percentile(s.sendMs, 0.95), SendP99: percentile(s.sendMs, 0.99),
		DelivP50: percentile(s.delivMs, 0.50), DelivP95: percentile(s.delivMs, 0.95), DelivP99: percentile(s.delivMs, 0.99), DelivMax: s.delivMax,
		RateWanted: rate, Seconds: elapsed.Seconds(),
	}
	if elapsed > 0 {
		r.RateAchieved = float64(r.Sent) / elapsed.Seconds()
	}
	if r.Sent > 0 {
		r.DeliveryRatio = float64(r.Delivered) / float64(r.Sent)
	}
	return r
}

func printResult(r result) {
	fmt.Printf("\nпотоков открыто: %d из %d (сбоев подключения %d, переподключений %d)\n", r.Opened, r.Streams, r.OpenFailures, r.Reconnects)
	fmt.Printf("сообщений: отправлено %d за %.0f с (%.1f в секунду при цели %.0f), отказов %d, 429: %d\n", r.Sent, r.Seconds, r.RateAchieved, r.RateWanted, r.SendFailures, r.Throttled)
	fmt.Printf("отправка, мс: p50 %.1f  p95 %.1f  p99 %.1f\n", r.SendP50, r.SendP95, r.SendP99)
	fmt.Printf("доставка собеседнику, мс: p50 %.1f  p95 %.1f  p99 %.1f  максимум %.1f; доставлено %d из %d (%.1f%%)\n",
		r.DelivP50, r.DelivP95, r.DelivP99, r.DelivMax, r.Delivered, r.Sent, r.DeliveryRatio*100)
	fmt.Printf("событий получено всего: %d\n", r.Events)
}
