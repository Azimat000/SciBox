// Package resilience защищает сервер от внешних сервисов (T-24): ограничитель числа одновременных обращений и
// «предохранитель», который после серии сбоев на время перестаёт обращаться к сервису. Зависший Crossref или почтовый
// сервер без этого держит запросы и горутины, а упавший получает новые обращения каждую секунду.
package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrOpen — предохранитель открыт: сервис недавно отказывал, обращений к нему сейчас нет.
	ErrOpen = errors.New("resilience: circuit breaker is open")
	// ErrBusy — слишком много одновременных обращений и место не освободилось за срок ожидания.
	ErrBusy = errors.New("resilience: too many concurrent calls")
)

// Rejected сообщает, что обращение отклонено нами, а не отвергнуто самим сервисом.
func Rejected(err error) bool { return errors.Is(err, ErrOpen) || errors.Is(err, ErrBusy) }

// Outcome — итог одного обращения для предохранителя.
type Outcome int

const (
	// Success — сервис ответил как должен.
	Success Outcome = iota
	// Failure — сбой самого сервиса (нет связи, срок, 5xx): считается в серии.
	Failure
	// Trip — сервис просит перестать (429): предохранитель открывается сразу.
	Trip
	// Neutral — итог не говорит о здоровье сервиса (ответ «нет такого», обрыв самим человеком).
	Neutral
)

// State — состояние предохранителя.
type State int

const (
	// Closed — обращения идут.
	Closed State = iota
	// Open — обращений нет до конца паузы.
	Open
	// HalfOpen — пауза прошла, идёт одно пробное обращение.
	HalfOpen
)

// String — название состояния для журнала.
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	}
	return "unknown"
}

// BreakerConfig — настройки предохранителя. Нулевые значения заменяются обычными.
type BreakerConfig struct {
	// Threshold — сколько сбоев подряд открывают предохранитель (по умолчанию 5).
	Threshold int
	// Cooldown — пауза в открытом состоянии (по умолчанию 30 секунд). Неудачная проба удваивает её до MaxCooldown.
	Cooldown time.Duration
	// MaxCooldown — предел паузы (по умолчанию 5 минут).
	MaxCooldown time.Duration
	// Now — часы; в тестах подменяются.
	Now func() time.Time
}

// Normalized подставляет обычные значения вместо нулевых и отрицательных: «0» никогда не значит «без паузы».
func (c BreakerConfig) Normalized() BreakerConfig {
	if c.Threshold < 1 {
		c.Threshold = 5
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 30 * time.Second
	}
	if c.MaxCooldown < c.Cooldown {
		c.MaxCooldown = max(5*time.Minute, c.Cooldown)
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Breaker — предохранитель. Безопасен для одновременного использования.
type Breaker struct {
	cfg BreakerConfig

	mu         sync.Mutex
	state      State
	generation uint64 // меняется при каждом переходе: запоздавший итог прошлого состояния ничего не решает
	failures   int
	openedAt   time.Time
	cooldown   time.Duration
	trips      int64
}

// NewBreaker собирает предохранитель.
func NewBreaker(cfg BreakerConfig) *Breaker {
	cfg = cfg.Normalized()
	return &Breaker{cfg: cfg, cooldown: cfg.Cooldown}
}

// Allow спрашивает разрешение на обращение. При отказе возвращает ErrOpen. При согласии возвращает done, который
// обязательно нужно вызвать с итогом (повторные вызовы игнорируются).
func (b *Breaker) Allow() (done func(Outcome), err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Open:
		if b.cfg.Now().Sub(b.openedAt) < b.cooldown {
			return nil, ErrOpen
		}
		b.transition(HalfOpen) // пауза прошла: пропускаем одну пробу
	case HalfOpen:
		return nil, ErrOpen // проба уже идёт
	}
	gen := b.generation
	var once sync.Once
	return func(o Outcome) { once.Do(func() { b.finish(gen, o) }) }, nil
}

func (b *Breaker) finish(gen uint64, o Outcome) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.generation {
		return // состояние сменилось, пока обращение шло
	}
	switch b.state {
	case Closed:
		switch o {
		case Success:
			b.failures = 0
		case Failure:
			if b.failures++; b.failures >= b.cfg.Threshold {
				b.open(b.cfg.Cooldown)
			}
		case Trip:
			b.open(b.cfg.Cooldown)
		case Neutral:
		}
	case HalfOpen:
		switch o {
		case Success:
			b.cooldown = b.cfg.Cooldown
			b.failures = 0
			b.transition(Closed)
		case Failure, Trip:
			b.open(min(b.cooldown*2, b.cfg.MaxCooldown))
		case Neutral:
			// Проба ничего не показала: пробуем ещё раз со следующим обращением.
			b.openedAt = time.Time{}
			b.transition(Open)
		}
	case Open:
	}
}

func (b *Breaker) open(cooldown time.Duration) {
	b.cooldown = cooldown
	b.openedAt = b.cfg.Now()
	b.trips++
	b.transition(Open)
}

func (b *Breaker) transition(s State) {
	b.state = s
	b.generation++
}

// State — текущее состояние (для журнала и метрик).
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Trips — сколько раз предохранитель открывался с запуска.
func (b *Breaker) Trips() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.trips
}

// RetryAfter — сколько ждать до следующей пробы; 0, если обращения идут.
func (b *Breaker) RetryAfter() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != Open {
		return 0
	}
	return max(b.cooldown-b.cfg.Now().Sub(b.openedAt), 0)
}

// Limiter ограничивает число одновременных обращений. Ожидание места ограничено сроком: очередь из горутин,
// ждущих зависший сервис, — та же беда, что и сам зависший сервис.
type Limiter struct {
	slots    chan struct{}
	maxWait  time.Duration
	rejected atomic.Int64
}

// NewLimiter собирает ограничитель: не больше maxConcurrent одновременных обращений, ожидание места не дольше maxWait
// (0 — не ждать вовсе). maxConcurrent меньше 1 заменяется единицей.
func NewLimiter(maxConcurrent int, maxWait time.Duration) *Limiter {
	return &Limiter{slots: make(chan struct{}, max(maxConcurrent, 1)), maxWait: max(maxWait, 0)}
}

// Acquire занимает место. release обязателен. Возвращает ErrBusy, если место не освободилось за срок, и ошибку
// контекста, если обращение отменили раньше.
func (l *Limiter) Acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel := func() { <-l.slots }
	select {
	case l.slots <- struct{}{}:
		return rel, nil
	default:
	}
	if l.maxWait == 0 {
		l.rejected.Add(1)
		return nil, ErrBusy
	}
	timer := time.NewTimer(l.maxWait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return rel, nil
	case <-timer.C:
		l.rejected.Add(1)
		return nil, ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InFlight — сколько обращений идёт сейчас.
func (l *Limiter) InFlight() int { return len(l.slots) }

// Capacity — сколько обращений может идти одновременно.
func (l *Limiter) Capacity() int { return cap(l.slots) }

// Rejected — сколько обращений отклонено из-за нехватки места с запуска.
func (l *Limiter) Rejected() int64 { return l.rejected.Load() }

// Gate — ограничитель и предохранитель одного внешнего сервиса. Нулевой или nil Gate ничего не ограничивает.
type Gate struct {
	Limiter *Limiter
	Breaker *Breaker
}

// Do выполняет fn под защитой. classify превращает итог fn в Outcome (nil — по умолчанию: ошибка значит сбой, но
// отмена самим вызывающим сбоем не считается). Отклонённое обращение возвращает ErrOpen или ErrBusy, fn не вызывается.
func (g *Gate) Do(ctx context.Context, fn func(context.Context) error, classify func(error) Outcome) error {
	if g == nil {
		return fn(ctx)
	}
	done := func(Outcome) {}
	if g.Breaker != nil {
		d, err := g.Breaker.Allow()
		if err != nil {
			return err
		}
		done = d
	}
	if g.Limiter != nil {
		release, err := g.Limiter.Acquire(ctx)
		if err != nil {
			done(Neutral) // место не нашлось: это не сбой сервиса, а наша очередь
			return err
		}
		defer release()
	}
	err := fn(ctx)
	switch {
	case classify != nil:
		done(classify(err))
	case err == nil:
		done(Success)
	case ctx.Err() != nil:
		done(Neutral)
	default:
		done(Failure)
	}
	return err
}
