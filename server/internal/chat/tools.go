package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
	"scibox/server/internal/textsafe"
)

// Инструменты переписки (срез 38, D-237…D-239): блокировка организации учёным, жалобы, шаблоны ответов организации.

// Пределы шаблонов и жалоб (contracts/chat/templates-*.json, complaint.json).
const (
	MaxTemplates     = 50
	MaxTemplateTitle = 120
	MaxComplaintNote = 2000
)

// Причины жалобы (contracts/chat/enums.json, complaint_reason).
var complaintReasons = []string{"spam", "fraud", "abuse", "other"}

// Ошибки инструментов; обработчики превращают их в ответы API.
var (
	// ErrForbidden — человек участник переписки или организации, но это действие стороне не положено: шаблоны правит только
	// владелец и кадровик, блокирует организацию и жалуется только учёный.
	ErrForbidden = errors.New("chat: forbidden")
	// ErrAlreadyBlocked — организация уже заблокирована этим учёным.
	ErrAlreadyBlocked = errors.New("chat: organization already blocked")
	// ErrTemplateNotFound — у организации нет такого шаблона.
	ErrTemplateNotFound = errors.New("chat: template not found")
	// ErrTemplateLimit — у организации уже MaxTemplates шаблонов.
	ErrTemplateLimit = errors.New("chat: too many templates")
)

// ---- блокировка ----

// Block — заблокированная организация.
type Block struct {
	OrgID     uuid.UUID `json:"org_id"`
	OrgName   string    `json:"org_name"`
	OrgSlug   string    `json:"org_slug"`
	CreatedAt time.Time `json:"created_at"`
}

// Blocked — заблокировал ли учёный организацию. Организации с этим человеком больше не пишут и не зовут его на вакансии
// (D-237): проверка стоит перед приглашением на вакансию и приглашением отклика.
func Blocked(ctx context.Context, q *dbgen.Queries, userID, orgID uuid.UUID) (bool, error) {
	ok, err := q.IsOrgBlockedBy(ctx, dbgen.IsOrgBlockedByParams{UserID: userID, OrgID: orgID})
	if err != nil {
		return false, fmt.Errorf("chat: check block: %w", err)
	}
	return ok, nil
}

// announceBlock дописывает строку о блокировке или её снятии в каждую переписку пары, которую иначе ничто не закрывает:
// закрытым отказом или отзывом переписки это не нужно. Замки берутся по возрастанию номера, после блокировки (или её
// снятия) в той же транзакции: отправка, которая успела раньше, остаётся перед строкой.
func announceBlock(ctx context.Context, q *dbgen.Queries, userID, orgID uuid.UUID, event string, at time.Time) error {
	pair, err := q.ListPairChats(ctx, dbgen.ListPairChatsParams{UserID: userID, OrgID: orgID})
	if err != nil {
		return fmt.Errorf("chat: list pair chats: %w", err)
	}
	for _, c := range pair {
		if _, err := q.LockChat(ctx, c.ID); err != nil {
			return fmt.Errorf("chat: lock chat: %w", err)
		}
		if StateOf(c.ApplicationStatus, c.OfferStatus, false).Closed {
			continue
		}
		if _, err := AppendSystem(ctx, q, c.ID, event, nil, at); err != nil {
			return err
		}
	}
	return nil
}

// Block блокирует организацию переписки: все переписки учёного с ней закрываются, её сотрудники не могут писать ему и
// звать его на вакансии. Только учёный. Повторная блокировка — ErrAlreadyBlocked.
func (s *Service) Block(ctx context.Context, user auth.User, id uuid.UUID) (Block, error) {
	var out Block
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, w, err := s.load(ctx, q, user, id)
		if err != nil {
			return err
		}
		if w.side != SideScientist {
			return ErrForbidden
		}
		now := s.now()
		n, err := q.InsertChatBlock(ctx, dbgen.InsertChatBlockParams{UserID: user.ID, OrgID: row.OrgID, Now: now})
		if err != nil {
			return fmt.Errorf("chat: insert block: %w", err)
		}
		if n == 0 {
			return ErrAlreadyBlocked
		}
		out = Block{OrgID: row.OrgID, OrgName: row.OrgName, OrgSlug: row.OrgSlug, CreatedAt: now.UTC()}
		return announceBlock(ctx, q, user.ID, row.OrgID, EventChatBlocked, now)
	})
	return out, err
}

// Unblock снимает блокировку: переписки, которые закрывала только она, снова открыты. Нет блокировки — ErrNotFound
// (повтор и «чужая» организация неотличимы).
func (s *Service) Unblock(ctx context.Context, user auth.User, orgID uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.DeleteChatBlock(ctx, dbgen.DeleteChatBlockParams{UserID: user.ID, OrgID: orgID})
		if err != nil {
			return fmt.Errorf("chat: delete block: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return announceBlock(ctx, q, user.ID, orgID, EventChatUnblocked, s.now())
	})
}

// Blocks — организации, которые заблокировал человек, новые сверху.
func (s *Service) Blocks(ctx context.Context, user auth.User) ([]Block, error) {
	rows, err := s.q.ListChatBlocks(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("chat: list blocks: %w", err)
	}
	out := make([]Block, 0, len(rows))
	for _, r := range rows {
		out = append(out, Block{OrgID: r.OrgID, OrgName: r.OrgName, OrgSlug: r.OrgSlug, CreatedAt: r.CreatedAt.UTC()})
	}
	return out, nil
}

// ---- жалобы ----

// ComplaintInput — тело жалобы.
type ComplaintInput struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

// Complaint — принятая жалоба.
type Complaint struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// plainText — текст без управляющих знаков (перенос строки и табуляция допустимы) и без знаков смены направления.
func plainText(s string) bool {
	if !textsafe.Valid(s) {
		return false
	}
	for _, r := range s {
		if (r != '\n' && r != '\t' && unicode.IsControl(r)) || isBidiControl(r) {
			return false
		}
	}
	return true
}

// Complain сохраняет жалобу учёного на организацию переписки. Жалоба только хранится: разбирает её модерация (срез 30).
// Переписку она не закрывает.
func (s *Service) Complain(ctx context.Context, user auth.User, id uuid.UUID, in ComplaintInput) (Complaint, error) {
	row, w, err := s.load(ctx, s.q, user, id)
	if err != nil {
		return Complaint{}, err
	}
	if w.side != SideScientist {
		return Complaint{}, ErrForbidden
	}
	note := strings.TrimSpace(strings.ReplaceAll(in.Note, "\r\n", "\n"))
	fields := map[string]string{}
	if !slices.Contains(complaintReasons, in.Reason) {
		fields["reason"] = "Выберите причину жалобы"
	}
	switch {
	case !plainText(note):
		fields["note"] = "В пояснении есть недопустимые знаки"
	case utf8.RuneCountInString(note) > MaxComplaintNote:
		fields["note"] = "Пояснение не длиннее 2000 знаков"
	case in.Reason == "other" && note == "":
		fields["note"] = "Опишите, что случилось"
	}
	if len(fields) > 0 {
		return Complaint{}, &auth.ValidationError{Fields: fields}
	}
	now := s.now()
	var r dbgen.InsertChatComplaintRow
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		// Пределы жалоб (срез 40): под замком человека, чтобы пачка запросов не прошла проверку с одним нулём.
		if err := s.checkComplaintLimits(ctx, q, user, id, now); err != nil {
			return err
		}
		var err error
		r, err = q.InsertChatComplaint(ctx, dbgen.InsertChatComplaintParams{
			UserID: user.ID, ChatID: &id, OrgID: row.OrgID, VacancyID: &row.VacancyID, Reason: in.Reason, Note: note, Now: now,
		})
		if err != nil {
			return fmt.Errorf("chat: insert complaint: %w", err)
		}
		return nil
	})
	if err != nil {
		return Complaint{}, err
	}
	return Complaint{ID: r.ID, CreatedAt: r.CreatedAt.UTC()}, nil
}

// ---- шаблоны ----

// Template — шаблон ответа организации.
type Template struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TemplateList — шаблоны организации и что можно смотрящему.
type TemplateList struct {
	Items   []Template `json:"items"`
	CanEdit bool       `json:"can_edit"`
	Limit   int        `json:"limit"`
}

// TemplateInput — тело шаблона.
type TemplateInput struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// canUseTemplates — человек ведёт отклики организации: всей или хотя бы одного подразделения.
func canUseTemplates(a access.Actor) bool {
	if a.Can(access.ViewApplications, access.NoUnit) {
		return true
	}
	return slices.ContainsFunc(a.HeadOf, func(unit uuid.UUID) bool { return a.Can(access.ViewApplications, unit) })
}

// templateActor проверяет, что человек вправе пользоваться шаблонами организации (читать): чужой организации и
// организации, где человек не ведёт отклики, для него «нет» (ErrNotFound).
func (s *Service) templateActor(ctx context.Context, q *dbgen.Queries, user auth.User, orgID uuid.UUID) (access.Actor, error) {
	actor, err := orgs.ActorOf(ctx, q, orgID, user.ID)
	if err != nil {
		return access.Actor{}, err
	}
	if !canUseTemplates(actor) {
		return access.Actor{}, ErrNotFound
	}
	return actor, nil
}

// editActor то же для правки: читать можно, править нельзя — ErrForbidden.
func (s *Service) editActor(ctx context.Context, q *dbgen.Queries, user auth.User, orgID uuid.UUID) error {
	actor, err := s.templateActor(ctx, q, user, orgID)
	if err != nil {
		return err
	}
	if !actor.Can(access.EditChatTemplates, access.NoUnit) {
		return ErrForbidden
	}
	return nil
}

func templateOf(id uuid.UUID, title, body string, updated time.Time) Template {
	return Template{ID: id, Title: title, Text: body, UpdatedAt: updated.UTC()}
}

// cleanTemplate проверяет название и текст шаблона; второе значение — ошибки по полям.
func cleanTemplate(in TemplateInput) (title, text string, fields map[string]string) {
	fields = map[string]string{}
	title = strings.TrimSpace(in.Title)
	switch {
	case !textsafe.Valid(title) || strings.ContainsFunc(title, func(r rune) bool { return unicode.IsControl(r) || isBidiControl(r) }):
		fields["title"] = "В названии есть недопустимые знаки"
	case title == "":
		fields["title"] = "Введите название шаблона"
	case utf8.RuneCountInString(title) > MaxTemplateTitle:
		fields["title"] = "Название не длиннее 120 знаков"
	}
	text, problem := cleanText(in.Text)
	switch problem {
	case "":
	case msgEmpty:
		fields["text"] = "Введите текст шаблона"
	case msgTooLong:
		fields["text"] = "Текст не длиннее 4000 знаков"
	default:
		fields["text"] = "В тексте есть недопустимые знаки"
	}
	return title, text, fields
}

// Templates возвращает шаблоны организации по названию. Видят все, кто ведёт отклики организации.
func (s *Service) Templates(ctx context.Context, user auth.User, orgID uuid.UUID) (TemplateList, error) {
	actor, err := s.templateActor(ctx, s.q, user, orgID)
	if err != nil {
		return TemplateList{}, err
	}
	rows, err := s.q.ListChatTemplates(ctx, orgID)
	if err != nil {
		return TemplateList{}, fmt.Errorf("chat: list templates: %w", err)
	}
	out := TemplateList{Items: make([]Template, 0, len(rows)), CanEdit: actor.Can(access.EditChatTemplates, access.NoUnit), Limit: MaxTemplates}
	for _, r := range rows {
		out.Items = append(out.Items, templateOf(r.ID, r.Title, r.Body, r.UpdatedAt))
	}
	return out, nil
}

// CreateTemplate добавляет шаблон. Не больше MaxTemplates на организацию: счёт и вставка идут под замком организации.
func (s *Service) CreateTemplate(ctx context.Context, user auth.User, orgID uuid.UUID, in TemplateInput) (Template, error) {
	if err := s.editActor(ctx, s.q, user, orgID); err != nil {
		return Template{}, err
	}
	title, text, fields := cleanTemplate(in)
	if len(fields) > 0 {
		return Template{}, &auth.ValidationError{Fields: fields}
	}
	var out Template
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := q.LockRateKey(ctx, "chat_templates:"+orgID.String()); err != nil {
			return fmt.Errorf("chat: lock templates: %w", err)
		}
		n, err := q.CountChatTemplates(ctx, orgID)
		if err != nil {
			return fmt.Errorf("chat: count templates: %w", err)
		}
		if int(n) >= MaxTemplates {
			return ErrTemplateLimit
		}
		r, err := q.InsertChatTemplate(ctx, dbgen.InsertChatTemplateParams{OrgID: orgID, Title: title, Body: text, CreatedBy: &user.ID, Now: s.now()})
		if err != nil {
			return fmt.Errorf("chat: insert template: %w", err)
		}
		out = templateOf(r.ID, r.Title, r.Body, r.UpdatedAt)
		return nil
	})
	return out, err
}

// UpdateTemplate исправляет шаблон организации.
func (s *Service) UpdateTemplate(ctx context.Context, user auth.User, orgID, id uuid.UUID, in TemplateInput) (Template, error) {
	if err := s.editActor(ctx, s.q, user, orgID); err != nil {
		return Template{}, err
	}
	title, text, fields := cleanTemplate(in)
	if len(fields) > 0 {
		return Template{}, &auth.ValidationError{Fields: fields}
	}
	r, err := s.q.UpdateChatTemplate(ctx, dbgen.UpdateChatTemplateParams{ID: id, OrgID: orgID, Title: title, Body: text, Now: s.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrTemplateNotFound
	}
	if err != nil {
		return Template{}, fmt.Errorf("chat: update template: %w", err)
	}
	return templateOf(r.ID, r.Title, r.Body, r.UpdatedAt), nil
}

// DeleteTemplate удаляет шаблон организации.
func (s *Service) DeleteTemplate(ctx context.Context, user auth.User, orgID, id uuid.UUID) error {
	if err := s.editActor(ctx, s.q, user, orgID); err != nil {
		return err
	}
	n, err := s.q.DeleteChatTemplate(ctx, dbgen.DeleteChatTemplateParams{ID: id, OrgID: orgID})
	if err != nil {
		return fmt.Errorf("chat: delete template: %w", err)
	}
	if n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}
