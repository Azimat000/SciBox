// Package applications — отклики на вакансии (срез 8, D-009): профиль, сопроводительное письмо, PDF-файлы и резюме,
// собранное из профиля; «Мои отклики»; карточка отклика глазами соискателя и организации.
//
// Критичная зона (docs/TESTING.md): здесь решается, кто видит чужой отклик, файлы и рекомендательные письма.
// Права организации спрашиваются только у пакета access (ViewApplications) через orgs.ActorOf.
package applications

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/profiles"
	"scibox/server/internal/references"
)

// Статусы отклика (D-009). Строки лежат в базе.
const (
	StatusSent      = "sent"      // отправлен
	StatusViewed    = "viewed"    // просмотрен организацией
	StatusInvited   = "invited"   // приглашён
	StatusRejected  = "rejected"  // отказ
	StatusAccepted  = "accepted"  // принят
	StatusWithdrawn = "withdrawn" // отозван соискателем
)

// Statuses — все статусы.
var Statuses = []string{StatusSent, StatusViewed, StatusInvited, StatusRejected, StatusAccepted, StatusWithdrawn}

// withdrawable — из каких статусов соискатель может отозвать отклик: пока решения нет.
var withdrawable = []string{StatusSent, StatusViewed, StatusInvited}

// CanWithdraw: можно ли отозвать отклик в таком статусе.
func CanWithdraw(status string) bool {
	for _, s := range withdrawable {
		if s == status {
			return true
		}
	}
	return false
}

// Роли читателя отклика в ответе.
const (
	RoleApplicant = "applicant"
	RoleStaff     = "staff"
)

// Пределы.
const (
	minCoverRunes = 20
	maxCoverRunes = 6000
	maxFormJSON   = 64 << 10
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	// ErrNotFound — отклика, вакансии или файла нет, либо человеку они «не существуют» (чужой отклик, скрытое письмо).
	ErrNotFound = errors.New("applications: not found")
	// ErrVacancyClosed — вакансия закрыта: отклики не принимаются.
	ErrVacancyClosed = errors.New("applications: vacancy is closed")
	// ErrDeadlinePassed — срок подачи прошёл.
	ErrDeadlinePassed = errors.New("applications: deadline passed")
	// ErrOwnVacancy — человек сам ведёт эту вакансию (видит отклики на неё): откликаться нельзя.
	ErrOwnVacancy = errors.New("applications: own vacancy")
	// ErrAlreadyApplied — на эту вакансию человек уже откликнулся.
	ErrAlreadyApplied = errors.New("applications: already applied")
	// ErrBadStatus — отклик в таком статусе нельзя отозвать, решить по нему или пригласить.
	ErrBadStatus = errors.New("applications: invalid status change")
	// ErrBadInvitation — приглашение уже в другом состоянии (отвечено, отменено): действие не подходит.
	ErrBadInvitation = errors.New("applications: invalid invitation state")
	// ErrTooManyInvitations — на отклик уже отправлено предельное число приглашений.
	ErrTooManyInvitations = errors.New("applications: too many invitations")
)

// VacancyRef — вакансия, на которую пришёл отклик.
type VacancyRef struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	OrgName  string    `json:"org_name"`
	OrgSlug  string    `json:"org_slug"`
	Deadline *string   `json:"deadline"`
}

// FileRef — файл отклика (содержимое отдаётся отдельным адресом).
type FileRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Size int       `json:"size"`
}

// RefsCount — сколько рекомендателей просили и сколько письм получено.
type RefsCount struct {
	Total    int `json:"total"`
	Received int `json:"received"`
}

// Summary — отклик в списке «Мои отклики».
type Summary struct {
	ID              uuid.UUID  `json:"id"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	StatusChangedAt time.Time  `json:"status_changed_at"`
	Vacancy         VacancyRef `json:"vacancy"`
	References      RefsCount  `json:"references"`
	// PendingInvitations — приглашения, которые ждут ответа соискателя.
	PendingInvitations int `json:"pending_invitations"`
}

// List — страница откликов.
type List struct {
	Items []Summary `json:"items"`
	Total int64     `json:"total"`
}

// Base — то, что видят и соискатель, и организация.
type Base struct {
	ID              uuid.UUID     `json:"id"`
	Status          string        `json:"status"`
	CreatedAt       time.Time     `json:"created_at"`
	StatusChangedAt time.Time     `json:"status_changed_at"`
	Vacancy         VacancyRef    `json:"vacancy"`
	ApplicantName   string        `json:"applicant_name"`
	ContactEmail    string        `json:"contact_email"`
	CoverLetter     string        `json:"cover_letter"`
	Profile         profiles.View `json:"profile"`
	CV              *FileRef      `json:"cv"`
	Files           []FileRef     `json:"files"`
	// DecisionNote — записка организации к решению (принят, отказ); её видят обе стороны.
	DecisionNote string       `json:"decision_note"`
	Invitations  []Invitation `json:"invitations"`
}

// ViewerInfo — кто смотрит и что ему можно. Сайт показывает кнопки по этим полям, но проверяет всё сервер.
type ViewerInfo struct {
	Role        string `json:"role"`
	CanWithdraw bool   `json:"can_withdraw"`
	// Decisions — какие решения организация может поставить сейчас (accepted, rejected); у соискателя пусто.
	Decisions []string `json:"decisions"`
	// CanInvite — можно ли отправить приглашение; у соискателя false.
	CanInvite bool `json:"can_invite"`
}

// Detail — карточка отклика. References зависит от того, кто смотрит: соискателю []references.Request (без писем),
// организации []references.StaffRequest (с письмами).
type Detail struct {
	Base
	Viewer     ViewerInfo `json:"viewer"`
	References any        `json:"references"`
}

// Input — поля отклика, как их присылает сайт (часть data формы).
type Input struct {
	VacancyID    uuid.UUID                 `json:"vacancy_id"`
	ContactEmail string                    `json:"contact_email"`
	CoverLetter  string                    `json:"cover_letter"`
	Referees     []references.RefereeInput `json:"referees"`
}

// Причины, по которым откликнуться нельзя (для кнопки на странице вакансии).
const (
	ReasonOwn     = "own_vacancy"
	ReasonClosed  = "closed"
	ReasonExpired = "deadline_passed"
	ReasonApplied = "applied"
)

// ApplicationRef — отклик человека на эту вакансию.
type ApplicationRef struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

// VacancyState — можно ли человеку откликнуться на вакансию и почему нет.
type VacancyState struct {
	CanApply    bool            `json:"can_apply"`
	Reason      string          `json:"reason,omitempty"`
	Application *ApplicationRef `json:"application"`
}

// InvitationAnswer — ответ соискателя на приглашение.
type InvitationAnswer struct {
	ProposedAt *time.Time `json:"proposed_at"`
	Note       string     `json:"note"`
	Contact    string     `json:"contact"`
	Time       string     `json:"time"`
	AnsweredAt time.Time  `json:"answered_at"`
}

// Invitation — приглашение; обе стороны видят его целиком. Флаги can_* зависят от того, кто смотрит.
type Invitation struct {
	ID           uuid.UUID         `json:"id"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	Message      string            `json:"message"`
	StartsAt     *time.Time        `json:"starts_at"`
	PlaceKind    string            `json:"place_kind"`
	Place        string            `json:"place"`
	ContactName  string            `json:"contact_name"`
	ContactEmail string            `json:"contact_email"`
	ContactPhone string            `json:"contact_phone"`
	Answer       *InvitationAnswer `json:"answer"`
	CreatedAt    time.Time         `json:"created_at"`
	// CanAnswer — соискатель может ответить; CanCancel и CanAcceptProposal — организация может отменить и принять предложенное время.
	CanAnswer         bool `json:"can_answer"`
	CanCancel         bool `json:"can_cancel"`
	CanAcceptProposal bool `json:"can_accept_proposal"`
}

// Candidate — отклик в списке организации.
type Candidate struct {
	ID              uuid.UUID  `json:"id"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	StatusChangedAt time.Time  `json:"status_changed_at"`
	ApplicantName   string     `json:"applicant_name"`
	Headline        string     `json:"headline"`
	Vacancy         VacancyRef `json:"vacancy"`
	UnitName        string     `json:"unit_name"`
	References      RefsCount  `json:"references"`
	// PendingInvitations ждут ответа соискателя, ProposedInvitations ждут решения организации.
	PendingInvitations  int `json:"pending_invitations"`
	ProposedInvitations int `json:"proposed_invitations"`
}

// CandidateList — страница откликов организации и число откликов по каждому статусу (при выбранной вакансии — на неё).
type CandidateList struct {
	Items  []Candidate    `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// CandidateFilter — что показать в списке: вакансия (uuid.Nil — любая), статус (пусто — любой), страница.
type CandidateFilter struct {
	VacancyID uuid.UUID
	Status    string
	Limit     int
	Offset    int
}

// VacancyCount — вакансия с числом откликов (отозванные не считаются) и числом непросмотренных.
type VacancyCount struct {
	ID      uuid.UUID `json:"id"`
	Title   string    `json:"title"`
	Status  string    `json:"status"`
	OrgName string    `json:"org_name"`
	OrgSlug string    `json:"org_slug"`
	Total   int       `json:"total"`
	New     int       `json:"new"`
}
