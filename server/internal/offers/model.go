// Package offers — приглашения учёных на вакансии (срез 10, D-095…D-097). Организация находит учёного в каталоге и
// приглашает его на конкретную вакансию; учёный отвечает один раз («Интересно» или «Не сейчас»), организация видит ответ.
//
// Критичная зона (docs/TESTING.md): здесь решается, кому можно написать человеку от имени организации. Права спрашиваются
// только у пакета access (ManageVacancies) через orgs.ActorOf, приватность профиля только у пакета privacy.
package offers

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Состояния приглашения. Строки лежат в базе, не менять.
const (
	StatusPending    = "pending"    // ждёт ответа учёного
	StatusInterested = "interested" // учёный ответил «Интересно»
	StatusDeclined   = "declined"   // учёный ответил «Не сейчас»
	StatusCancelled  = "cancelled"  // организация отозвала приглашение, пока ответа не было
)

// Statuses — все состояния в порядке показа на вкладках организации.
var Statuses = []string{StatusPending, StatusInterested, StatusDeclined, StatusCancelled}

// Ответы учёного.
const (
	ActionInterested = "interested"
	ActionDeclined   = "declined"
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	// ErrNotFound — приглашения, вакансии или профиля нет, либо человеку они «не существуют» (нет прав, чужое приглашение,
	// закрытый приватностью профиль).
	ErrNotFound = errors.New("offers: not found")
	// ErrVacancyClosed — вакансия не опубликована или закрыта: приглашать на неё нельзя.
	ErrVacancyClosed = errors.New("offers: vacancy is not open")
	// ErrDeadlinePassed — срок подачи заявок прошёл.
	ErrDeadlinePassed = errors.New("offers: deadline passed")
	// ErrAlreadyOffered — этого человека уже приглашали на эту вакансию.
	ErrAlreadyOffered = errors.New("offers: already invited to this vacancy")
	// ErrAlreadyApplied — этот человек уже откликнулся на вакансию.
	ErrAlreadyApplied = errors.New("offers: already applied to this vacancy")
	// ErrStaffInvitee — приглашаемый сам ведёт эту вакансию (в том числе это сам приглашающий).
	ErrStaffInvitee = errors.New("offers: the person runs this vacancy")
	// ErrBadState — приглашение уже не ждёт ответа: ответили, отозвали.
	ErrBadState = errors.New("offers: offer is not pending")
)

// Пределы.
const (
	maxMessage = 1000
	maxNote    = 1000
)

// InviteInput — приглашение, как его прислала организация.
type InviteInput struct {
	VacancyID uuid.UUID `json:"vacancy_id"`
	// ProfileID — номер профиля учёного (тот же, что в адресе его страницы): номер аккаунта наружу не отдаётся.
	ProfileID uuid.UUID `json:"profile_id"`
	Message   string    `json:"message"`
}

// AnswerInput — ответ учёного.
type AnswerInput struct {
	Action string `json:"action"` // interested | declined
	Note   string `json:"note"`
}

// VacancyRef — вакансия в приглашении.
type VacancyRef struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	OrgName  string    `json:"org_name"`
	OrgSlug  string    `json:"org_slug"`
	UnitName string    `json:"unit_name"`
	City     string    `json:"city"`
	Deadline *string   `json:"deadline"`
}

// ScientistRef — приглашённый учёный (в списках организации).
type ScientistRef struct {
	ProfileID uuid.UUID `json:"profile_id"`
	Name      string    `json:"name"`
}

// Offer — приглашение. Учёный видит организацию и вакансию (без имени сотрудника, D-096), организация видит учёного.
type Offer struct {
	ID         uuid.UUID  `json:"id"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	AnswerNote string     `json:"answer_note"`
	AnsweredAt *time.Time `json:"answered_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Vacancy    VacancyRef `json:"vacancy"`
	// Scientist — только в ответах организации.
	Scientist *ScientistRef `json:"scientist,omitempty"`
	// ApplicationID — только в ответах учёного: его живой отклик на эту вакансию, если есть.
	ApplicationID *uuid.UUID `json:"application_id"`
	// CanAnswer — учёный ещё может ответить; CanCancel — организация ещё может отозвать приглашение.
	CanAnswer bool `json:"can_answer"`
	CanCancel bool `json:"can_cancel"`
}

// MineList — «Приглашения» учёного: новые сверху; отозванные ему не показываются.
type MineList struct {
	Items []Offer `json:"items"`
	Total int     `json:"total"`
	// Counts — по состояниям (без отозванных); Pending — сколько ждёт ответа.
	Counts  map[string]int `json:"counts"`
	Pending int            `json:"pending"`
}

// SentList — «Отправленные приглашения» организации.
type SentList struct {
	Items  []Offer        `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// SentFilter — что показывать в списке организации.
type SentFilter struct {
	VacancyID *uuid.UUID
	Status    string
	Limit     int
	Offset    int
}

// Target — вакансия, на которую можно пригласить конкретного учёного.
type Target struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	OrgName  string    `json:"org_name"`
	OrgSlug  string    `json:"org_slug"`
	UnitName string    `json:"unit_name"`
	Deadline *string   `json:"deadline"`
	// Offered — этого человека на вакансию уже приглашали; Applied — он уже откликнулся. В обоих случаях звать не нужно.
	Offered bool `json:"offered"`
	Applied bool `json:"applied"`
}
