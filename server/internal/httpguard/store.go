package httpguard

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"scibox/server/internal/dbgen"
)

// Store считает запросы: Hit засчитывает ещё один запрос вида kind от key в окне, начавшемся в window,
// и возвращает, сколько их стало в этом окне.
type Store interface {
	Hit(ctx context.Context, kind, key string, window time.Time) (int, error)
}

// guestKey — ключ гостя. IPv6 считается по сети /64: обычно она целиком у одного человека или дома,
// и смена адреса внутри неё не обнуляет счёт.
func guestKey(a netip.Addr) string {
	switch {
	case !a.IsValid():
		return "ip:unknown"
	case a.Is6():
		p, _ := a.Prefix(64) // для IPv6 длина 64 всегда допустима
		return "ip6:" + p.String()
	default:
		return "ip:" + a.String()
	}
}

// PostgresStore держит счётчики в таблице rate_counters (D-141): лимит общий для всех копий сервера.
type PostgresStore struct {
	q *dbgen.Queries
}

// NewPostgresStore — счётчики в базе.
func NewPostgresStore(db dbgen.DBTX) *PostgresStore {
	return &PostgresStore{q: dbgen.New(db)}
}

// Hit — см. Store. Одна команда INSERT … ON CONFLICT: одновременные запросы не теряют счёт.
func (s *PostgresStore) Hit(ctx context.Context, kind, key string, window time.Time) (int, error) {
	hits, err := s.q.HitRateCounter(ctx, dbgen.HitRateCounterParams{Kind: kind, Key: key, WindowStart: window})
	return int(hits), err
}

// Cleanup удаляет счётчики, окна которых начались раньше now − KeepCounters.
func (s *PostgresStore) Cleanup(ctx context.Context, now time.Time) error {
	return s.q.DeleteOldRateCounters(ctx, now.Add(-KeepCounters))
}

// MemoryStore держит счётчики в памяти процесса. Годится для одной копии сервера и тестов;
// при нескольких копиях каждая считала бы своё, поэтому сервер в работе берёт PostgresStore.
type MemoryStore struct {
	mu     sync.Mutex
	counts map[memKey]int
	swept  time.Time
}

type memKey struct {
	kind, key string
	window    time.Time
}

// NewMemoryStore — счётчики в памяти.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{counts: map[memKey]int{}}
}

// Hit — см. Store. Раз в окно KeepCounters старые счётчики удаляются, чтобы память не росла.
func (s *MemoryStore) Hit(_ context.Context, kind, key string, window time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if window.Sub(s.swept) >= KeepCounters {
		for k := range s.counts {
			if window.Sub(k.window) >= KeepCounters {
				delete(s.counts, k)
			}
		}
		s.swept = window
	}
	k := memKey{kind: kind, key: key, window: window.UTC()}
	s.counts[k]++
	return s.counts[k], nil
}
