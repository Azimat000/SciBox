// Package jobs запускает фоновые задачи сервера и воркера и делает их безопасными при нескольких копиях (T-23).
//
// Задачи бывают двух видов:
//   - «на весь кластер» (Job.Cluster): чистка, рассылка по поискам, напоминания. Расписание лежит в базе (`job_runs`):
//     в каждый срок задачу исполняет ровно одна копия, остальные видят «ещё не пора» или «уже занята». Упавшая копия не
//     держит задачу: аренда истекает, и её берёт другая. После сбоя пауза нарастает и общая для всех копий.
//   - «в каждой копии» (письма, проверка файлов): копия гоняет задачу по своему таймеру, а от дублей защищает сама
//     задача (письмо берётся в аренду, файл берётся с отметкой).
//
// Остановка мягкая: по сигналу копия перестаёт брать новую работу (Draining), текущим задачам даётся время дописать, и
// только потом их контекст отменяется. Задача, прерванная остановкой, возвращается другим копиям сразу и сбоем не считается.
package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/logmask"
)

// DB — база для расписания.
type DB = dbgen.DBTX

// Job — одна фоновая задача.
type Job struct {
	// Name — уникальное имя: ключ расписания в базе, подпись в журнале и метриках.
	Name string
	// Every — пауза между проходами после успеха.
	Every time.Duration
	// Cluster — задача идёт раз в Every на весь кластер (см. описание пакета), иначе в каждой копии по своему таймеру.
	Cluster bool
	// Lease — для задач кластера: на сколько задача закрепляется за копией и сколько ей отведено на проход. По умолчанию
	// максимум из Every и 10 минут. Задача, не уложившаяся в срок, получает отмену контекста и считается упавшей.
	Lease time.Duration
	// Run — сама работа. Возвращает ошибку при сбое, уважает ctx и между порциями работы спрашивает Draining(ctx).
	Run func(ctx context.Context) error
}

// Config — настройки исполнителя. Нулевые значения заменяются обычными.
type Config struct {
	// Poll — как часто копия проверяет, не пора ли занять задачу кластера (по умолчанию 10 секунд).
	Poll time.Duration
	// Grace — сколько после сигнала остановки дописывают текущие задачи (по умолчанию 20 секунд).
	Grace time.Duration
	// MaxBackoff — предел паузы после сбоев (по умолчанию 15 минут).
	MaxBackoff time.Duration
	// Holder — имя копии для расписания; по умолчанию «хост-pid-случайное».
	Holder string
	// Now — часы; в тестах подменяются.
	Now func() time.Time
}

// Normalized заменяет нулевые и отрицательные значения обычными: «0» не значит «без паузы» или «без срока».
func (c Config) Normalized() Config {
	if c.Poll <= 0 {
		c.Poll = 10 * time.Second
	}
	if c.Grace <= 0 {
		c.Grace = 20 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 15 * time.Minute
	}
	if c.Holder == "" {
		c.Holder = newHolder()
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

func newHolder() string {
	host, _ := os.Hostname() // без имени узла копия называется «host-pid-…»: случайная часть всё равно различает копии
	if host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), rand.Text()[:6])
}

// Status — состояние задачи в этой копии (для журнала и метрик среза 26).
type Status struct {
	Name    string
	Cluster bool
	// Runs — сколько проходов начала эта копия.
	Runs int64
	// Failures — сколько проходов подряд закончились ошибкой.
	Failures   int
	Running    bool
	LastStart  time.Time
	LastFinish time.Time
	LastOK     time.Time
	LastError  string
}

type drainKey struct{}

// Draining сообщает, что копию останавливают: новую порцию работы не начинать, текущую дописать. Истёкшая отмена
// контекста тоже значит «остановка».
func Draining(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	ch, _ := ctx.Value(drainKey{}).(<-chan struct{})
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Runner исполняет задачи.
type Runner struct {
	q    *dbgen.Queries
	log  *slog.Logger
	cfg  Config
	mu   sync.Mutex
	stat map[string]*Status
}

// New собирает исполнитель.
func New(db DB, logger *slog.Logger, cfg Config) *Runner {
	return &Runner{q: dbgen.New(db), log: logger, cfg: cfg.Normalized(), stat: map[string]*Status{}}
}

// Holder — имя этой копии в расписании.
func (r *Runner) Holder() string { return r.cfg.Holder }

// Snapshot — состояние всех задач этой копии, по имени.
func (r *Runner) Snapshot() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, 0, len(r.stat))
	for _, s := range r.stat {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b Status) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (r *Runner) update(name string, f func(*Status)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(r.stat[name])
}

// Validate проверяет описание задач: у каждой есть уникальное имя, срок и работа. Run делает то же, но запуск сервера
// проверяет заранее, чтобы ошибка описания остановила сервер до старта, а не спряталась в фоновой горутине.
func Validate(list []Job) error {
	seen := map[string]bool{}
	for _, j := range list {
		switch {
		case strings.TrimSpace(j.Name) == "":
			return errors.New("jobs: a job has no name")
		case seen[j.Name]:
			return fmt.Errorf("jobs: duplicate job name %q", j.Name)
		case j.Every <= 0:
			return fmt.Errorf("jobs: job %q has no interval", j.Name)
		case j.Run == nil:
			return fmt.Errorf("jobs: job %q has no Run", j.Name)
		}
		seen[j.Name] = true
	}
	return nil
}

// Run исполняет задачи, пока не отменён ctx, и возвращается, когда все закончили. Отмена ctx — сигнал остановки: задачи
// видят Draining, и им даётся Config.Grace, после чего их контекст отменяется. Ошибка возвращается только для
// неверного описания задач; сбои самих задач в журнале и в расписании.
func (r *Runner) Run(ctx context.Context, list ...Job) error {
	if err := Validate(list); err != nil {
		return err
	}
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()
	drain := make(chan struct{})
	var drainRO <-chan struct{} = drain
	runCtx = context.WithValue(runCtx, drainKey{}, drainRO)
	// Сигнал остановки: задачи видят Draining; через Grace контекст отменяется (таймер без остановки: после выхода
	// из Run он только повторно отменит уже отменённый контекст).
	stopWatch := context.AfterFunc(ctx, func() {
		close(drain)
		time.AfterFunc(r.cfg.Grace, cancelRun)
	})
	defer stopWatch()

	var wg sync.WaitGroup
	for _, j := range list {
		r.mu.Lock()
		r.stat[j.Name] = &Status{Name: j.Name, Cluster: j.Cluster}
		r.mu.Unlock()
		wg.Go(func() { r.loop(runCtx, drain, j) })
	}
	wg.Wait()
	return nil
}

// loop гоняет одну задачу, пока не начнётся остановка. Задача сама проверяет Draining перед началом работы: проход, который
// попал на остановку, ничего не начинает.
func (r *Runner) loop(ctx context.Context, drain <-chan struct{}, j Job) {
	failures := 0 // подряд упавших проходов или неудачных попыток занять задачу, считает только эта копия
	for {
		var wait time.Duration
		if j.Cluster {
			wait = r.clusterPass(ctx, j, &failures)
		} else {
			wait = r.localPass(ctx, j, &failures)
		}
		timer := time.NewTimer(jitter(wait))
		select {
		case <-drain:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// localPass — один проход задачи «в каждой копии»; возвращает, сколько ждать до следующего.
func (r *Runner) localPass(ctx context.Context, j Job, failures *int) time.Duration {
	if err := r.exec(ctx, j, 0, *failures); err != nil {
		*failures++
		return Backoff(*failures, j.Every, r.cfg.MaxBackoff)
	}
	*failures = 0
	return j.Every
}

// clusterPass — одна попытка занять и исполнить задачу кластера; возвращает, сколько ждать до следующей.
func (r *Runner) clusterPass(ctx context.Context, j Job, local *int) time.Duration {
	now := r.cfg.Now()
	lease := j.leaseOrDefault()
	shared, err := r.q.ClaimJob(ctx, dbgen.ClaimJobParams{Name: j.Name, Now: now, LeasedUntil: now.Add(lease), Holder: r.cfg.Holder})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		*local = 0
		return r.cfg.Poll // не пора или задачу держит другая копия
	case err != nil:
		if ctx.Err() == nil {
			*local++
			r.log.Error("job schedule is unavailable", "job", j.Name, "failures", *local, "err", err)
		}
		return Backoff(*local, r.cfg.Poll, r.cfg.MaxBackoff)
	}
	*local = 0
	runErr := r.exec(ctx, j, lease, int(shared))
	// Запись итога не должна зависеть от отмены контекста задачи: копию могут останавливать как раз сейчас.
	fin, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	done := r.cfg.Now()
	var rows int64
	var finErr error
	switch {
	case runErr == nil:
		rows, finErr = r.q.FinishJobOK(fin, dbgen.FinishJobOKParams{NextRunAt: done.Add(j.Every), Now: done, Name: j.Name, Holder: r.cfg.Holder})
	case ctx.Err() != nil:
		// Остановка оборвала задачу: сбоем это не считается, другая копия возьмёт её сразу.
		rows, finErr = r.q.ReleaseJob(fin, dbgen.ReleaseJobParams{Now: done, Name: j.Name, Holder: r.cfg.Holder})
	default:
		next := done.Add(Backoff(int(shared)+1, j.Every, r.cfg.MaxBackoff))
		rows, finErr = r.q.FinishJobFailed(fin, dbgen.FinishJobFailedParams{NextRunAt: next, Now: done, LastError: shorten(logmask.Mask(runErr.Error())), Name: j.Name, Holder: r.cfg.Holder})
	}
	switch {
	case finErr != nil:
		r.log.Error("job result was not recorded", "job", j.Name, "err", finErr)
	case rows == 0:
		r.log.Warn("job lease was lost before it finished: another copy took it over", "job", j.Name)
	}
	return r.cfg.Poll
}

func (j Job) leaseOrDefault() time.Duration {
	if j.Lease > 0 {
		return j.Lease
	}
	return max(j.Every, 10*time.Minute)
}

// exec вызывает задачу: с перехватом паники, учётом в Status и записью в журнал. timeout 0 — без срока. prior — сколько
// проходов подряд упало до этого (у задач кластера по общему расписанию, у остальных по этой копии).
func (r *Runner) exec(ctx context.Context, j Job, timeout time.Duration, prior int) (err error) {
	start := r.cfg.Now()
	r.update(j.Name, func(s *Status) { s.Runs++; s.Running = true; s.LastStart = start })
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("jobs: %s panicked: %v", j.Name, p)
			r.log.Error("job panicked", "job", j.Name, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
		}
		r.finished(j, err, prior)
	}()
	return j.Run(runCtx)
}

// finished обновляет состояние и пишет в журнал: первые сбои предупреждением, серия — ошибкой, выздоровление — сообщением.
func (r *Runner) finished(j Job, err error, prior int) {
	at := r.cfg.Now()
	failures := prior + 1
	r.update(j.Name, func(s *Status) {
		s.Running = false
		s.LastFinish = at
		if err == nil {
			s.Failures, s.LastOK, s.LastError = 0, at, ""
			return
		}
		s.Failures = failures
		s.LastError = shorten(logmask.Mask(err.Error()))
	})
	switch {
	case err == nil && prior > 0:
		r.log.Info("job recovered", "job", j.Name, "failed_before", prior)
	case err != nil && errors.Is(err, context.Canceled):
		r.log.Info("job interrupted by shutdown", "job", j.Name)
	case err != nil && failures >= 3:
		r.log.Error("job keeps failing", "job", j.Name, "failures", failures, "err", err)
	case err != nil:
		r.log.Warn("job failed", "job", j.Name, "failures", failures, "err", err)
	}
}

// Backoff — пауза после failures неудач подряд: начинается с короткой (не длиннее 30 секунд и обычного срока) и
// удваивается до предела, который не ниже минуты и не выше maxBackoff; обычный срок задачи тоже не превышается, если он
// короче предела.
func Backoff(failures int, every, maxBackoff time.Duration) time.Duration {
	if failures < 1 {
		return every
	}
	ceiling := min(max(every, time.Minute), max(maxBackoff, time.Minute))
	d := min(every, 30*time.Second)
	for i := 1; i < failures && d < ceiling; i++ {
		d *= 2
	}
	return min(d, ceiling)
}

// jitter разбрасывает паузу на ±10%, чтобы копии не просыпались одновременно.
func jitter(d time.Duration) time.Duration {
	span := int64(d) / 5
	if span <= 0 {
		return d
	}
	//nolint:gosec // G404: разброс пауз между копиями, к безопасности отношения не имеет
	return d - time.Duration(span/2) + time.Duration(mathrand.Int64N(span+1))
}

func shorten(s string) string {
	const limit = 500
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}
