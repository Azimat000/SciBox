// Package audit — журнал действий (срез 21, T-17, D-147…D-150).
//
// Критичная зона (docs/TESTING.md): журнал отвечает на вопрос «кто это сделал», и сам содержит персональные данные.
// Запись делается в той же транзакции, что и действие: нет записи — нет и действия. Таблица только для дописывания
// (триггер базы не даёт менять и удалять записи; единственное исключение — адрес стирается вместе с аккаунтом).
//
// В записи только минимум: кто (номер аккаунта), что (код действия из списка ниже), над чем (вид, номер и подпись
// объекта на момент действия), над каким человеком (номер аккаунта), мелкие подробности из кода (роли, статусы) и
// адрес. Имена, письма, тексты людей и их контакты в запись не попадают: имя берётся из аккаунта при показе, поэтому
// после стирания аккаунта в журнале видно «Аккаунт удалён».
package audit

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Action — что произошло, в виде «вид.действие». Строки лежат в базе и в интерфейсе, не менять.
type Action string

// Действия организации: видны её владельцу.
const (
	MemberRoleChanged Action = "member.role_changed" // details: from, to
	MemberRemoved     Action = "member.removed"      // владелец убрал сотрудника
	MemberLeft        Action = "member.left"         // сотрудник ушёл сам
	MemberJoined      Action = "member.joined"       // принял приглашение; details: role
	InvitationSent    Action = "invitation.sent"     // details: role, email (скрыта частично)
	InvitationRevoked Action = "invitation.revoked"
	UnitHeadChanged   Action = "unit.head_changed" // subject: новый руководитель, пусто — руководителя сняли

	VacancyPublished Action = "vacancy.published"
	VacancyClosed    Action = "vacancy.closed"
	VacancyArchived  Action = "vacancy.archived" // архива нет с D-256: действие осталось только в старых записях журнала
	VacancyReopened  Action = "vacancy.reopened" // закрытая опубликована снова
	VacancyRestored  Action = "vacancy.restored" // из архива в закрытые; тоже только в старых записях (D-256)
	// ApplicationsExported — отклики выгружены в файл (срез 16). target: вакансия, если выгружали одну (иначе пусто);
	// details: rows — сколько откликов этой организации в файле, scope — vacancy|list, status — отбор по статусу.
	ApplicationsExported Action = "vacancy.applications_exported"

	ApplicationViewed             Action = "application.viewed" // организация впервые открыла отклик
	ApplicationAccepted           Action = "application.accepted"
	ApplicationRejected           Action = "application.rejected"
	// Три действия ниже были, пока внутри отклика жили приглашения (до D-256); остались только в старых записях журнала.
	ApplicationInvited            Action = "application.invited" // details: kind
	ApplicationInvitationCanceled Action = "application.invitation_canceled"
	ApplicationProposalAccepted   Action = "application.proposal_accepted"
	ApplicationFileDownloaded     Action = "application.file_downloaded" // details: kind

	OfferSent     Action = "offer.sent" // приглашение человека на вакансию; subject: приглашённый
	OfferCanceled Action = "offer.canceled"
)

// Действия с собственным аккаунтом: видны только самому человеку (у записи нет организации).
const (
	AccountLogin             Action = "account.login"
	AccountPasswordChanged   Action = "account.password_changed"
	AccountPasswordReset     Action = "account.password_reset"
	AccountEmailChangeAsked  Action = "account.email_change_asked"
	AccountEmailChanged      Action = "account.email_changed"
	AccountEmailReverted     Action = "account.email_reverted"
	AccountDeletionRequested Action = "account.deletion_requested"
	AccountDeletionCanceled  Action = "account.deletion_canceled"
	AccountErased            Action = "account.erased"        // данные стёрты по сроку (actor пусто)
	AccountDataExported      Action = "account.data_exported" // человек скачал архив своих данных (T-20)
)

type scope uint8

const (
	scopeOrg scope = iota + 1
	scopeAccount
)

// registry — все известные действия и чьи они. Действие вне списка записать нельзя.
var registry = map[Action]scope{
	MemberRoleChanged: scopeOrg, MemberRemoved: scopeOrg, MemberLeft: scopeOrg, MemberJoined: scopeOrg,
	InvitationSent: scopeOrg, InvitationRevoked: scopeOrg, UnitHeadChanged: scopeOrg,
	VacancyPublished: scopeOrg, VacancyClosed: scopeOrg, VacancyArchived: scopeOrg, VacancyReopened: scopeOrg, VacancyRestored: scopeOrg, ApplicationsExported: scopeOrg,
	ApplicationViewed: scopeOrg, ApplicationAccepted: scopeOrg, ApplicationRejected: scopeOrg, ApplicationInvited: scopeOrg,
	ApplicationInvitationCanceled: scopeOrg, ApplicationProposalAccepted: scopeOrg, ApplicationFileDownloaded: scopeOrg,
	OfferSent: scopeOrg, OfferCanceled: scopeOrg,

	AccountLogin: scopeAccount, AccountPasswordChanged: scopeAccount, AccountPasswordReset: scopeAccount,
	AccountEmailChangeAsked: scopeAccount, AccountEmailChanged: scopeAccount, AccountEmailReverted: scopeAccount,
	AccountDeletionRequested: scopeAccount, AccountDeletionCanceled: scopeAccount, AccountErased: scopeAccount,
	AccountDataExported: scopeAccount,
}

// Actions — все известные действия по алфавиту (для тестов и проверки подписей на сайте).
func Actions() []Action { return slices.Sorted(maps.Keys(registry)) }

// OrgScoped — действие записывается в журнал организации (а не в личный журнал аккаунта).
func OrgScoped(a Action) bool { return registry[a] == scopeOrg }

// Ошибки записи: это ошибки в коде, а не в запросе человека.
var (
	ErrUnknownAction = errors.New("audit: unknown action")
	ErrScope         = errors.New("audit: organization is required for organization actions and forbidden for account actions")
)

// Пределы записи. Они же стоят в CHECK таблицы; запись, которая не поместилась, укорачивается, а не отвергается:
// сбой журнала отменяет действие, и это не должно зависеть от длины названия вакансии.
const (
	maxLabelRunes  = 300
	maxKindRunes   = 30
	maxDetailPairs = 6
	maxKeyRunes    = 20
	maxValueRunes  = 60
	maxIPLen       = 45
)

// Event — одна запись журнала.
type Event struct {
	Action Action
	// OrgID — чьё это действие. Обязательно у действий организации, запрещено у действий аккаунта.
	OrgID uuid.UUID
	// Actor — кто сделал; uuid.Nil: система или человек без аккаунта.
	Actor uuid.UUID
	// Subject — над каким человеком; uuid.Nil: ни над кем.
	Subject uuid.UUID
	// TargetKind, TargetID, TargetLabel — над каким объектом. Подпись фиксируется на момент действия и не должна
	// содержать имён людей (имена берутся из аккаунтов при показе).
	TargetKind  string
	TargetID    uuid.UUID
	TargetLabel string
	// Details — мелкие подробности из кода (роль до и после, статус). Не больше шести пар.
	Details map[string]string
	// IP — адрес, если он известен лучше, чем контекст запроса; пусто — взять из контекста (WithSource).
	IP string
	// At — время действия; нулевое — время транзакции.
	At time.Time
}

type sourceKey struct{}

// WithSource запоминает адрес человека в контексте запроса: сервисы не знают про HTTP, но записи нужен адрес.
func WithSource(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, sourceKey{}, ip)
}

// SourceIP — адрес из контекста запроса; пусто, если запрос не от человека (фоновая задача, тест).
func SourceIP(ctx context.Context) string {
	ip, _ := ctx.Value(sourceKey{}).(string)
	return ip
}

// Record дописывает запись в журнал внутри транзакции, к которой привязан q. Вызывать нужно в той же транзакции,
// что и действие: тогда запись и действие появляются или откатываются вместе.
func Record(ctx context.Context, q *dbgen.Queries, e Event) error {
	sc, ok := registry[e.Action]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAction, e.Action)
	}
	if (sc == scopeOrg) != (e.OrgID != uuid.Nil) {
		return fmt.Errorf("%w: %s", ErrScope, e.Action)
	}
	p := dbgen.InsertAuditEventParams{
		Action:      string(e.Action),
		ActorID:     optional(e.Actor),
		SubjectID:   optional(e.Subject),
		OrgID:       optional(e.OrgID),
		TargetKind:  cut(clean(e.TargetKind), maxKindRunes),
		TargetID:    optional(e.TargetID),
		TargetLabel: cut(clean(e.TargetLabel), maxLabelRunes),
		Details:     encodeDetails(e.Details),
	}
	if ip := cut(cmp.Or(e.IP, SourceIP(ctx)), maxIPLen); ip != "" {
		p.Ip = &ip
	}
	if !e.At.IsZero() {
		p.At = &e.At
	}
	if err := q.InsertAuditEvent(ctx, p); err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Action, err)
	}
	return nil
}

func optional(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// encodeDetails собирает подробности в JSON так, чтобы они гарантированно помещались в предел таблицы:
// лишние пары отбрасываются (по алфавиту ключей), длинные значения обрезаются, управляющие знаки заменяются пробелами.
// Предел таблицы считается по тексту jsonb, где «<» занимает один байт, поэтому экранирование Go его не увеличивает.
func encodeDetails(d map[string]string) []byte {
	out := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(d)) {
		if len(out) == maxDetailPairs {
			break
		}
		if key := cut(clean(k), maxKeyRunes); key != "" {
			out[key] = cut(clean(d[k]), maxValueRunes)
		}
	}
	raw, _ := json.Marshal(out) // словарь из строк закодировать нельзя только при нехватке памяти
	return raw
}

// clean заменяет управляющие знаки и переводы строк пробелами: подпись и подробности — одна строка.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

// cut обрезает строку до n знаков (не байт); обрезанное помечается многоточием.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// MaskEmail прячет большую часть адреса: «anna.petrova@msu.ru» → «a***@msu.ru». В журнале адрес нужен, чтобы понять,
// о каком приглашении речь, а не чтобы писать человеку.
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok || local == "" || domain == "" {
		return "***"
	}
	return string([]rune(local)[0]) + "***@" + strings.ToLower(domain)
}
