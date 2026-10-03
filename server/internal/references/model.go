// Package references — рекомендательные письма (срез 8, D-014): соискатель просит рекомендателя по почте,
// тот пишет письмо по одноразовой ссылке без аккаунта; письмо видит организация, соискатель не видит.
//
// Критичная зона (docs/TESTING.md): ошибка здесь показывает письмо тому, о ком оно написано, оставляет ссылку
// рабочей после ответа или даёт ответить от чужого имени.
package references

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Статусы просьбы. Строки лежат в базе.
const (
	StatusPending  = "pending"  // ждём ответа
	StatusReceived = "received" // письмо получено
	StatusDeclined = "declined" // рекомендатель отказался
)

// Пределы.
const (
	// MaxPerApplication — сколько рекомендателей можно просить в одном отклике.
	MaxPerApplication = 3
	// MaxLetterRunes — самое длинное письмо, набранное на странице.
	MaxLetterRunes = 12000
	maxNameRunes   = 120
	minNameRunes   = 2
	maxRelRunes    = 120
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	// ErrNotFound — отклика или просьбы нет, либо они чужие (человеку они «не существуют»).
	ErrNotFound = errors.New("references: not found")
	// ErrTooMany — в отклике уже максимум рекомендателей.
	ErrTooMany = errors.New("references: too many referees")
	// ErrClosed — отклик уже закрыт (отозван, отклонён или принят): новых просьб не принимаем.
	ErrClosed = errors.New("references: application is closed")
	// ErrNotPending — просьба уже получила ответ: повторить или отменить её нельзя.
	ErrNotPending = errors.New("references: request is not pending")
	// ErrInvalidLink — такой ссылки нет (или она заменена новой).
	ErrInvalidLink = errors.New("references: invalid link")
	// ErrExpired — срок ссылки вышел.
	ErrExpired = errors.New("references: link expired")
	// ErrGone — кандидат отозвал отклик: письмо больше не нужно.
	ErrGone = errors.New("references: application withdrawn")
	// ErrAlreadyAnswered — на эту ссылку уже ответили.
	ErrAlreadyAnswered = errors.New("references: already answered")
)

// TooSoonError — письмо с просьбой отправлено недавно: повторить можно позже.
type TooSoonError struct{ RetryAfter time.Duration }

func (e *TooSoonError) Error() string { return "references: resend too soon" }

// RefereeInput — рекомендатель, как его вводит соискатель.
type RefereeInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Relation string `json:"relation"`
}

// Referee — проверенный рекомендатель.
type Referee struct {
	Name, Email, Relation string
}

// AppInfo — об отклике ровно столько, сколько нужно для письма рекомендателю.
type AppInfo struct {
	ID            uuid.UUID
	ApplicantName string
	VacancyTitle  string
	OrgName       string
}

// Request — просьба глазами соискателя: кому, когда и что ответили. Самого письма здесь нет и быть не должно (D-014).
type Request struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Email      string     `json:"email"`
	Relation   string     `json:"relation"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastSentAt time.Time  `json:"last_sent_at"`
	AnsweredAt *time.Time `json:"answered_at"`
	// CanResend — можно ли отправить письмо с просьбой ещё раз; ResendAt — когда, если пока нельзя.
	CanResend bool       `json:"can_resend"`
	ResendAt  *time.Time `json:"resend_at"`
}

// FileInfo — файл письма (самого содержимого здесь нет, его отдаёт выдача файлов отклика).
type FileInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Size int       `json:"size"`
}

// Letter — письмо рекомендателя: текст и/или PDF.
type Letter struct {
	Text string    `json:"text"`
	File *FileInfo `json:"file"`
}

// StaffRequest — просьба глазами организации: то же, что видит соискатель, и само письмо.
type StaffRequest struct {
	Request
	Letter *Letter `json:"letter"`
}

// Info — что видит рекомендатель на странице по ссылке.
type Info struct {
	RefereeName   string    `json:"referee_name"`
	Relation      string    `json:"relation"`
	ApplicantName string    `json:"applicant_name"`
	VacancyTitle  string    `json:"vacancy_title"`
	OrgName       string    `json:"org_name"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// LetterInput — письмо, набранное на странице; PDF приходит отдельно, файлом.
type LetterInput struct {
	Text string `json:"text"`
}
