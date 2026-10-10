package httpcache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Keyed — короткий кеш ответов по ключу запроса (срез 27, D-179). Нужен поиску: один и тот же запрос (слово, фильтры,
// страница) приходит от многих людей подряд, а отвечать на него дорого (до 25 мс процессора базы у частого слова).
// В кеш кладутся только ответы, одинаковые для всех, кто может задать этот запрос (вызывающий отвечает за ключ).
//
// Устройство:
//   - живёт ttl, потом ответ собирается заново; ответ с ошибкой не запоминается;
//   - одновременные запросы с одним ключом ждут один сбор, а не создают его каждый (нагрузка на базу не умножается
//     числом посетителей в момент, когда срок вышел);
//   - сбор идёт в контексте без отмены самого посетителя (иначе ушедший посетитель оборвал бы сбор для остальных) с
//     собственным сроком loadTimeout;
//   - записей не больше max: при переполнении сначала уходят устаревшие, потом любая готовая.
type Keyed struct {
	ttl         time.Duration
	max         int
	loadTimeout time.Duration
	now         func() time.Time

	// staleFor — сколько ещё после срока ответ можно отдать, пока он обновляется в фоне (0 — нельзя).
	staleFor time.Duration

	mu    sync.Mutex
	items map[string]*keyedItem

	hits, misses, shared, stale atomic.Int64
}

type keyedItem struct {
	doc   Doc
	until time.Time
	err   error
	ready bool
	// refreshing — фоновое обновление просроченного ответа уже идёт.
	refreshing bool
	done       chan struct{} // закрывается, когда сбор закончен (с ответом или ошибкой)
}

// Stats — счётчики кеша для метрик: hits — ответ был готов, misses — пришлось собирать, shared — присоединился к
// чужому сбору, stale — отдан просроченный ответ, пока он обновляется в фоне.
type Stats struct {
	Hits, Misses, Shared, Stale int64
	Entries                     int
}

// NewKeyed создаёт кеш: ответы живут ttl, записей не больше max. Нулевые и отрицательные значения заменяются
// безопасными (секунда, одна запись): кеш «навсегда» и кеш «без предела» оба ошибка.
func NewKeyed(ttl time.Duration, max int) *Keyed { return newKeyed(ttl, max, time.Now) }

// NewKeyedWithClock — то же со своими часами (для тестов пакетов, которые кладут кеш в свои обработчики).
func NewKeyedWithClock(ttl time.Duration, max int, now func() time.Time) *Keyed {
	return newKeyed(ttl, max, now)
}

func newKeyed(ttl time.Duration, max int, now func() time.Time) *Keyed {
	if ttl <= 0 {
		ttl = time.Second
	}
	if max < 1 {
		max = 1
	}
	return &Keyed{ttl: ttl, max: max, loadTimeout: 10 * time.Second, now: now, items: map[string]*keyedItem{}}
}

// WithStale разрешает отдавать просроченный ответ ещё d после срока, пока он обновляется в фоне: тот, кто пришёл в
// момент истечения срока, не ждёт тяжёлый сбор (так живёт главная страница: счёт занимает секунду). Первый ответ и ответ,
// просроченный больше чем на d, по-прежнему собираются в запросе.
func (k *Keyed) WithStale(d time.Duration) *Keyed {
	k.staleFor = max(d, 0)
	return k
}

// Get возвращает готовый ответ по ключу или собирает его через load. Ответ больше maxBody байт возвращается, но не
// запоминается (maxBody ≤ 0 — без предела): один тяжёлый ответ не должен вытеснить сотни лёгких.
func (k *Keyed) Get(ctx context.Context, key string, maxBody int, load func(context.Context) (any, error)) (Doc, error) {
	k.mu.Lock()
	it := k.items[key]
	switch {
	case it != nil && it.ready && k.now().Before(it.until):
		doc := it.doc
		k.mu.Unlock()
		k.hits.Add(1)
		return doc, nil
	case it != nil && it.ready && k.staleFor > 0 && k.now().Before(it.until.Add(k.staleFor)):
		// Просрочен недавно: отдаём прежний ответ, один из запросов обновляет его в фоне.
		doc := it.doc
		refresh := !it.refreshing
		it.refreshing = true
		k.mu.Unlock()
		k.stale.Add(1)
		if refresh {
			go k.refresh(ctx, key, it, maxBody, load)
		}
		return doc, nil
	case it != nil && !it.ready:
		done := it.done
		k.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return Doc{}, ctx.Err()
		}
		// Сбор закончился: читаем его итог. Если он кончился ошибкой, она же и ответ (повторять за всех не нужно).
		k.mu.Lock()
		doc, err := it.doc, it.err
		k.mu.Unlock()
		if err != nil {
			return Doc{}, err
		}
		k.shared.Add(1)
		return doc, nil
	}
	// Записи нет или она устарела: этот запрос собирает ответ.
	it = &keyedItem{done: make(chan struct{})}
	k.items[key] = it
	k.evictLocked(key)
	k.mu.Unlock()
	k.misses.Add(1)

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), k.loadTimeout)
	v, err := load(loadCtx)
	cancel()
	var doc Doc
	if err == nil {
		doc, err = Encode(v)
	}

	k.mu.Lock()
	it.doc, it.err, it.ready = doc, err, true
	it.until = k.now().Add(k.ttl)
	if err != nil || (maxBody > 0 && len(doc.Body) > maxBody) {
		// Не запоминается: следующий запрос соберёт заново. Ждавшие этот сбор получат итог через it.
		if k.items[key] == it {
			delete(k.items, key)
		}
	}
	k.mu.Unlock()
	close(it.done)
	return doc, err
}

// refresh обновляет просроченный ответ в фоне. Неудача оставляет прежний ответ (он доживёт свой запас), следующий запрос
// попробует снова.
func (k *Keyed) refresh(ctx context.Context, key string, it *keyedItem, maxBody int, load func(context.Context) (any, error)) {
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), k.loadTimeout)
	defer cancel()
	v, err := load(loadCtx)
	var doc Doc
	if err == nil {
		doc, err = Encode(v)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	it.refreshing = false
	switch {
	case err != nil:
	case maxBody > 0 && len(doc.Body) > maxBody:
		if k.items[key] == it {
			delete(k.items, key)
		}
	default:
		it.doc, it.until = doc, k.now().Add(k.ttl)
	}
}

// evictLocked освобождает место для новой записи (mu занят). Ключ keep (только что созданный) остаётся.
func (k *Keyed) evictLocked(keep string) {
	if len(k.items) <= k.max {
		return
	}
	now := k.now()
	for key, it := range k.items {
		if key != keep && it.ready && !now.Before(it.until) {
			delete(k.items, key)
		}
	}
	for key, it := range k.items {
		if len(k.items) <= k.max {
			return
		}
		if key != keep && it.ready {
			delete(k.items, key)
		}
	}
}

// Stats возвращает счётчики.
func (k *Keyed) Stats() Stats {
	k.mu.Lock()
	n := len(k.items)
	k.mu.Unlock()
	return Stats{Hits: k.hits.Load(), Misses: k.misses.Load(), Shared: k.shared.Load(), Stale: k.stale.Load(), Entries: n}
}
