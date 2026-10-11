// Package dataexport — выгрузка своих данных (срез 22, T-20, D-153): право человека на доступ к своим персональным
// данным по 152-ФЗ. Архив ZIP: data.json (всё, что про человека лежит в базе) и папка files/ (его резюме и
// приложенные к откликам файлы).
//
// Критичная зона (docs/TESTING.md): ошибка здесь отдаёт чужие данные или отдаёт данные чужому. Поэтому:
//   - архив выдаётся только со свежим паролем (D-144): украденный сеанс не вынесет всё молча;
//   - каждая выгрузка записывается в личный журнал аккаунта до выдачи (нет записи в журнале — нет архива, I-33) и
//     считается в лимите (3 в сутки);
//   - каждый запрос выбирает строки по номеру человека; данные других людей не входят (имена сотрудников, письма
//     рекомендателей о нём — их соискатель не видит, D-082);
//   - данные читаются одной транзакцией «только чтение», чтобы архив был согласованным снимком.
package dataexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
)

// kindExport — вид счётчика частоты (таблица rate_events).
const kindExport = "data_export"

// Format — версия устройства data.json. Меняется, если сменились имена разделов.
const Format = 1

// Limit — не больше Max выгрузок за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	ProductName string
	// Export — сколько раз в окно можно скачать архив.
	Export Limit
}

// DefaultConfig — обычные настройки: три архива в сутки.
func DefaultConfig(productName string) Config {
	return Config{ProductName: productName, Export: Limit{Max: 3, Window: 24 * time.Hour}}
}

// DB — то, что сервису нужно от базы. Этому интерфейсу отвечает *pgxpool.Pool; тесты подставляют «ломающуюся» базу.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — выгрузка данных.
type Service struct {
	db    DB
	q     *dbgen.Queries
	vault *files.Vault
	cfg   Config
	now   func() time.Time
}

// NewService собирает сервис. vault — склад, откуда берётся содержимое файлов.
func NewService(db DB, vault *files.Vault, cfg Config) *Service {
	return newService(db, vault, cfg, time.Now)
}

// NewServiceWithClock — то же с чужими часами (для тестов лимита).
func NewServiceWithClock(db DB, vault *files.Vault, cfg Config, now func() time.Time) *Service {
	return newService(db, vault, cfg, now)
}

func newService(db DB, vault *files.Vault, cfg Config, now func() time.Time) *Service {
	if cfg.Export.Max <= 0 || cfg.Export.Window <= 0 {
		cfg.Export = DefaultConfig(cfg.ProductName).Export
	}
	return &Service{db: db, q: dbgen.New(db), vault: vault, cfg: cfg, now: now}
}

// FileEntry — файл, который попадёт в архив.
type FileEntry struct {
	ID            uuid.UUID `json:"id"`
	ApplicationID uuid.UUID `json:"application_id"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Size          int32     `json:"size"`
	// Scan — проверка на вирусы: в архив попадают только проверенные (clean); остальные только перечислены.
	Scan string `json:"scan"`
	// Path — где файл лежит в архиве; пусто у файла, которого в архиве нет.
	Path string `json:"path,omitempty"`
}

// Archive — собранные данные, готовые к записи в ZIP.
type Archive struct {
	user uuid.UUID
	at   time.Time
	// JSON — содержимое data.json.
	JSON []byte
	// Readme — содержимое README.txt.
	Readme []byte
	// Files — файлы для папки files/.
	Files []FileEntry
}

// FileName — имя скачиваемого архива.
func (a *Archive) FileName() string {
	return "scibox-data-" + a.at.UTC().Format("2006-01-02") + ".zip"
}

// section — раздел data.json: ключ и запрос, который его собирает.
type section struct {
	key string
	// optional: у человека может не быть такой строки (профиля, настроек); тогда в архиве null.
	optional bool
	load     func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error)
}

// sections — всё, что входит в data.json, в порядке показа. Новая таблица с данными человека добавляется сюда
// (и в карту ПДн docs/SECURITY.md).
func sections() []section {
	return []section{
		{"account", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportAccount(ctx, u.ID)
		}},
		{"profile", true, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportProfile(ctx, u.ID)
		}},
		{"profile_items", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportProfileItems(ctx, u.ID)
		}},
		{"profile_specialties", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportProfileSpecialties(ctx, u.ID)
		}},
		{"profile_hidden_orgs", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportHiddenOrgs(ctx, u.ID)
		}},
		{"profile_reveals", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportReveals(ctx, u.ID)
		}},
		{"applications", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportApplications(ctx, u.ID)
		}},
		{"offers", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportOffers(ctx, u.ID)
		}},
		{"favorites", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportFavorites(ctx, u.ID)
		}},
		{"saved_searches", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportSavedSearches(ctx, u.ID)
		}},
		{"deadline_reminders", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportDeadlineReminders(ctx, u.ID)
		}},
		{"notification_settings", true, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportNotificationSettings(ctx, u.ID)
		}},
		{"notifications", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportNotifications(ctx, u.ID)
		}},
		{"organizations", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportMemberships(ctx, u.ID)
		}},
		{"organization_invitations", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportOrgInvitations(ctx, u.Email)
		}},
		{"sessions", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportSessions(ctx, u.ID)
		}},
		{"known_browsers", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportKnownDevices(ctx, u.ID)
		}},
		{"waiting_mail", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportWaitingMail(ctx, u.Email)
		}},
		{"journal", false, func(ctx context.Context, q *dbgen.Queries, u auth.User) ([]byte, error) {
			return q.ExportAudit(ctx, &u.ID)
		}},
	}
}

// Sections — ключи data.json по порядку (для тестов и документации).
func Sections() []string {
	var keys []string
	for _, s := range sections() {
		keys = append(keys, s.key)
	}
	return append(keys, "files")
}

// Prepare проверяет право на выгрузку, записывает её в журнал и собирает архив. Человеку ничего не пишет:
// вызывающий отдаёт результат через Write только после успешного Prepare.
func (s *Service) Prepare(ctx context.Context, p auth.Principal) (*Archive, error) {
	if !p.Fresh {
		return nil, auth.ErrReauthRequired
	}
	if err := s.spend(ctx, p.User.ID); err != nil {
		return nil, err
	}
	at := s.now()
	doc, entries, err := s.collect(ctx, p.User, at)
	if err != nil {
		return nil, err
	}
	return &Archive{user: p.User.ID, at: at, JSON: doc, Readme: readme(s.cfg.ProductName, at), Files: entries}, nil
}

// spend считает попытку и записывает выгрузку в личный журнал одной транзакцией: журнал не принял — попытки тоже нет.
func (s *Service) spend(ctx context.Context, user uuid.UUID) (err error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dataexport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	now, lim := s.now(), s.cfg.Export
	// Замок на счётчик: параллельные запросы не проходят проверку с одним и тем же нулём (срез 29, CON-2).
	if err := q.LockRateKey(ctx, kindExport+":"+user.String()); err != nil {
		return fmt.Errorf("dataexport: lock rate key: %w", err)
	}
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindExport, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("dataexport: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindExport, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("dataexport: record rate event: %w", err)
	}
	if err := audit.Record(ctx, q, audit.Event{Action: audit.AccountDataExported, Actor: user, At: now}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dataexport: commit: %w", err)
	}
	return nil
}

// collect читает все разделы одним согласованным снимком и собирает data.json.
func (s *Service) collect(ctx context.Context, user auth.User, at time.Time) ([]byte, []FileEntry, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("dataexport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		return nil, nil, fmt.Errorf("dataexport: snapshot: %w", err)
	}
	q := s.q.WithTx(tx)

	var out bytes.Buffer
	out.WriteString(`{"format":` + strconv.Itoa(Format) + `,"exported_at":`)
	stamp, _ := json.Marshal(at.UTC())
	out.Write(stamp)
	for _, sec := range sections() {
		raw, err := sec.load(ctx, q, user)
		switch {
		case errors.Is(err, pgx.ErrNoRows) && sec.optional:
			raw = []byte("null")
		case err != nil:
			return nil, nil, fmt.Errorf("dataexport: %s: %w", sec.key, err)
		}
		key, _ := json.Marshal(sec.key)
		out.WriteByte(',')
		out.Write(key)
		out.WriteByte(':')
		out.Write(raw)
	}

	rows, err := q.ExportFiles(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("dataexport: files: %w", err)
	}
	entries := make([]FileEntry, 0, len(rows))
	perApplication := map[uuid.UUID]int{}
	for _, r := range rows {
		if !exportable(r.Kind) {
			continue
		}
		e := FileEntry{ID: r.ID, ApplicationID: r.ApplicationID, Kind: r.Kind, Name: r.Name, Size: r.Size, Scan: r.ScanStatus}
		if files.ScanStatus(r.ScanStatus).Available() {
			perApplication[r.ApplicationID]++
			e.Path = fmt.Sprintf("files/%s/%02d-%s", r.ApplicationID, perApplication[r.ApplicationID], files.CleanName(r.Name))
		}
		entries = append(entries, e)
	}
	list, _ := json.Marshal(entries)
	out.WriteString(`,"files":`)
	out.Write(list)
	out.WriteByte('}')
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("dataexport: commit: %w", err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out.Bytes(), "", "  "); err != nil {
		return nil, nil, fmt.Errorf("dataexport: build json: %w", err)
	}
	return pretty.Bytes(), entries, nil
}

// exportable — файл такого вида можно положить в архив соискателя. Второй замок после запроса: письма рекомендателей
// соискателю не отдаются никогда (D-082), неизвестный вид закрыт для всех.
func exportable(kind string) bool {
	k, ok := files.Parse(kind)
	return ok && k.VisibleTo(files.Applicant)
}

// Write пишет ZIP в w: README.txt, data.json и файлы потоком из хранилища, по одному. Ошибка посреди записи
// означает, что архив оборван: вызывающий обязан оборвать ответ, а не закончить его как удавшийся.
func (s *Service) Write(ctx context.Context, w io.Writer, a *Archive) error {
	zw := zip.NewWriter(w)
	if err := addFile(zw, "README.txt", a.at, zip.Deflate, a.Readme); err != nil {
		return err
	}
	if err := addFile(zw, "data.json", a.at, zip.Deflate, a.JSON); err != nil {
		return err
	}
	for _, f := range a.Files {
		if f.Path == "" {
			continue // файл ещё на проверке или отвергнут: в архиве его нет, в data.json он перечислен
		}
		key, err := s.q.ExportFileKey(ctx, dbgen.ExportFileKeyParams{ID: f.ID, UserID: a.user})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // файл убрали, пока собирался архив
		}
		if err != nil {
			return fmt.Errorf("dataexport: file: %w", err)
		}
		if err := s.addObject(ctx, zw, f.Path, a.at, *key); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("dataexport: close archive: %w", err)
	}
	return nil
}

func addFile(zw *zip.Writer, name string, at time.Time, method uint16, data []byte) error {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: at})
	if err != nil {
		return fmt.Errorf("dataexport: add %s: %w", name, err)
	}
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("dataexport: write %s: %w", name, err)
	}
	return nil
}

// addObject кладёт в архив объект из хранилища потоком. PDF уже сжаты: Store не тратит время зря. Пропавший объект
// проверенного файла — не повод тихо выдать неполный архив, а ошибка (ответ обрывается).
func (s *Service) addObject(ctx context.Context, zw *zip.Writer, name string, at time.Time, key string) error {
	obj, err := s.vault.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("dataexport: open %s: %w", name, err)
	}
	defer obj.Close() //nolint:errcheck // чтение закончено; закрытие ничего не пишет
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: at})
	if err != nil {
		return fmt.Errorf("dataexport: add %s: %w", name, err)
	}
	if _, err := io.Copy(fw, obj); err != nil {
		return fmt.Errorf("dataexport: write %s: %w", name, err)
	}
	return nil
}

func readme(product string, at time.Time) []byte {
	msk := time.FixedZone("MSK", 3*3600)
	var b strings.Builder
	fmt.Fprintf(&b, "Ваши данные из сервиса «%s»\nСобрано: %s (МСК)\n\n", product, at.In(msk).Format("02.01.2006 15:04"))
	b.WriteString("Что в архиве\n")
	b.WriteString("- data.json — всё, что сервис хранит про вас: аккаунт, профиль, отклики, приглашения, избранное, поиски,\n")
	b.WriteString("  уведомления, организации, сеансы входа и записи журнала о ваших действиях. Файл текстовый, его открывает любой\n")
	b.WriteString("  редактор; поля названы по-английски, даты в формате ГГГГ-ММ-ДД и времени по Гринвичу.\n")
	b.WriteString("- files/ — ваши резюме и файлы, которые вы прикладывали к откликам. Путь каждого файла указан в data.json, в разделе files.\n")
	b.WriteString("  Файл, который ещё проверяется на вирусы или не прошёл проверку, в архив не кладётся: он только перечислен в data.json.\n\n")
	b.WriteString("Чего в архиве нет\n")
	b.WriteString("- Рекомендательных писем о вас: их видит только организация, куда вы откликнулись.\n")
	b.WriteString("- Данных других людей: имён сотрудников организаций, чужих профилей. Почты и имена рекомендателей есть там, где вы их сами указали.\n")
	b.WriteString("- Текстов вакансий и страниц организаций: они принадлежат организациям.\n")
	b.WriteString("- Секретов: паролей, ссылок из писем и кодов сеансов. Пароль хранится только в виде необратимого отпечатка.\n\n")
	b.WriteString("Ваши права\n")
	b.WriteString("Вы можете исправить данные в профиле и настройках, а на странице «Аккаунт → Безопасность» удалить аккаунт:\n")
	b.WriteString("через 14 дней данные будут стёрты, отклики в организациях станут обезличенными.\n")
	return []byte(b.String())
}
