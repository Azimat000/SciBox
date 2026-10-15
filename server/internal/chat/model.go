// Package chat — переписка учёного и организации по вакансии (срезы 37–39, D-214…D-216, docs/CHAT.md).
//
// Критичная зона (docs/TESTING.md): здесь решается, кто читает и пишет в переписку о судьбе человека. Права спрашиваются
// только у пакета access через orgs.ActorOf (ViewApplications на вакансию, при каждом запросе), приватность учёного
// только у пакета privacy. Чужая и несуществующая переписка неотличимы: один и тот же ErrNotFound.
//
// Переписка не хранит «закрыта»: открыта она или нет, следует из состояния отклика и приглашения (closure.go).
package chat

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Side — сторона переписки, глазами которой смотрят. Человек может быть и учёным, и сотрудником организации (D-004).
type Side string

// Стороны. Строки лежат в базе и в контракте, не менять.
const (
	SideScientist Side = "scientist"
	SideOrg       Side = "org"
)

// Other — противоположная сторона.
func (s Side) Other() Side {
	if s == SideScientist {
		return SideOrg
	}
	return SideScientist
}

// Valid — известная сторона.
func (s Side) Valid() bool { return s == SideScientist || s == SideOrg }

// Вкладки списка.
const (
	BoxAll     = "all"
	BoxUnread  = "unread"
	BoxArchive = "archive"
)

// Виды сообщения.
const (
	KindText   = "text"
	KindSystem = "system"
)

// Виды строк о событии (contracts/chat/enums.json, event_type). Строки лежат в базе, не менять.
const (
	EventApplicationSent       = "application.sent"
	EventApplicationViewed     = "application.viewed"
	EventInvitationCreated     = "invitation.created"
	EventInvitationAnswered    = "invitation.answered"
	EventInvitationAccepted    = "invitation.proposal_accepted"
	EventInvitationCancelled   = "invitation.cancelled"
	EventApplicationDecided    = "application.decided"
	EventApplicationWithdrawn  = "application.withdrawn"
	EventOfferCreated          = "offer.created"
	EventOfferAnswered         = "offer.answered"
	EventOfferCancelled        = "offer.cancelled"
	EventChatBlocked           = "chat.blocked"
	EventChatUnblocked         = "chat.unblocked"
	sourceApplication          = "application"
	sourceOffer                = "offer"
	statusApplicationRejected  = "rejected"
	statusApplicationWithdrawn = "withdrawn"
	statusOfferDeclined        = "declined"
	statusOfferCancelled       = "cancelled"
)

// Причины, по которым переписка закрыта (contracts/chat/enums.json, closed_reason).
const (
	ClosedRejected       = "rejected"
	ClosedWithdrawn      = "withdrawn"
	ClosedOfferDeclined  = "offer_declined"
	ClosedOfferCancelled = "offer_cancelled"
)

// Пределы.
const (
	// MaxText — самое длинное сообщение в знаках (кодовых точках), D-215.
	MaxText = 4000
	// EditWindow — сколько после отправки своё сообщение можно исправить или удалить (D-215).
	EditWindow = 24 * time.Hour
	// maxSearch — самый длинный поисковый запрос списка.
	maxSearch = 100
	// maxBody — самое большое тело запроса: сообщение до 4000 знаков по-русски ≈ 8 КБ, с запасом на разметку JSON.
	maxBody = 32 << 10
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	// ErrNotFound — переписки нет или она человеку «не существует»: чужая, нет прав на вакансию, не тот номер.
	ErrNotFound = errors.New("chat: not found")
	// ErrMessageNotFound — в видимой переписке нет такого сообщения (или оно уже стёрто там, где править нечего).
	ErrMessageNotFound = errors.New("chat: message not found")
	// ErrClosed — переписка закрыта: писать, править, удалять и печатать нельзя.
	ErrClosed = errors.New("chat: closed")
	// ErrEditWindowClosed — прошли сутки: исправить или удалить сообщение уже нельзя.
	ErrEditWindowClosed = errors.New("chat: edit window closed")
	// ErrSystemMessage — строку о событии нельзя исправить или удалить.
	ErrSystemMessage = errors.New("chat: system message")
	// ErrNotAuthor — чужое сообщение исправить или удалить нельзя, в том числе коллеге по организации.
	ErrNotAuthor = errors.New("chat: not the author")
	// ErrUnknownEvent — строка о событии неизвестного вида.
	ErrUnknownEvent = errors.New("chat: unknown event type")
)

// BadParamError — неверное значение параметра адреса или тела (400 bad_request).
type BadParamError struct{ Param string }

func (e *BadParamError) Error() string { return "chat: bad parameter " + e.Param }

// Author — автор сообщения глазами смотрящего.
type Author struct {
	Side Side   `json:"side"`
	Name string `json:"name"`
	// IsMe — это сообщение написал сам смотрящий (коллега по организации не «я»).
	IsMe bool `json:"is_me"`
	// Anonymous — имя учёного организации не показывается (D-199, D-200): пустое имя и эта отметка.
	Anonymous bool `json:"anonymous"`
	// Former — сотрудника уже нет в организации или его аккаунт удалён: сайт подписывает «Сотрудник организации».
	Former bool `json:"former"`
}

// Event — строка о событии: вид и данные, из которых сайт собирает текст.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Message — сообщение переписки. У строки о событии автора нет, текст пуст, заполнено event.
type Message struct {
	ID     uuid.UUID `json:"id"`
	Seq    int       `json:"seq"`
	Kind   string    `json:"kind"`
	Author *Author   `json:"author"`
	Text   string    `json:"text"`
	Event  *Event    `json:"event"`
	// CreatedAt, EditedAt — время в UTC.
	CreatedAt time.Time  `json:"created_at"`
	EditedAt  *time.Time `json:"edited_at"`
	Deleted   bool       `json:"deleted"`
	// EditableUntil — до какого момента своё сообщение можно исправить или удалить; у чужих, системных и удалённых null.
	EditableUntil *time.Time `json:"editable_until"`
	// ClientID — номер, который выдал сайт при отправке; его видит только автор.
	ClientID *uuid.UUID `json:"client_id"`
}

// Counterpart — собеседник глазами смотрящего.
type Counterpart struct {
	Kind      Side   `json:"kind"`
	Name      string `json:"name"`
	Anonymous bool   `json:"anonymous"`
	// ProfileID — номер профиля учёного, только организации и только если профиль ей виден.
	ProfileID *uuid.UUID `json:"profile_id"`
}

// VacancyRef — вакансия в строке списка.
type VacancyRef struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	OrgName  string    `json:"org_name"`
	OrgSlug  string    `json:"org_slug"`
	UnitName string    `json:"unit_name"`
}

// VacancyHeader — вакансия в шапке: ещё состояние и срок.
type VacancyHeader struct {
	VacancyRef
	Status   string  `json:"status"`
	Deadline *string `json:"deadline"`
}

// Status — состояние отклика или, пока отклика нет, приглашения на вакансию.
type Status struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}

// Viewer — что положено смотрящему в этой переписке.
type Viewer struct {
	Side             Side       `json:"side"`
	CanUseTemplates  bool       `json:"can_use_templates"`
	CanEditTemplates bool       `json:"can_edit_templates"`
	OrgID            *uuid.UUID `json:"org_id"`
	CanBlock         bool       `json:"can_block"`
	CanComplain      bool       `json:"can_complain"`
}

// Read — докуда прочитано (номера seq): у смотрящего и у другой стороны.
type Read struct {
	Mine   int `json:"mine"`
	Theirs int `json:"theirs"`
}

// Chat — шапка переписки.
type Chat struct {
	ID            uuid.UUID     `json:"id"`
	Side          Side          `json:"side"`
	Vacancy       VacancyHeader `json:"vacancy"`
	Counterpart   Counterpart   `json:"counterpart"`
	ApplicationID *uuid.UUID    `json:"application_id"`
	OfferID       *uuid.UUID    `json:"offer_id"`
	Status        Status        `json:"status"`
	// NeedsAnswer — учёного ждёт приглашение, на которое он ещё не ответил.
	NeedsAnswer  bool    `json:"needs_answer"`
	CanWrite     bool    `json:"can_write"`
	ClosedReason *string `json:"closed_reason"`
	Archived     bool    `json:"archived"`
	Viewer       Viewer  `json:"viewer"`
	Read         Read    `json:"read"`
}

// LastMessage — последнее сообщение в строке списка.
type LastMessage struct {
	Seq       int       `json:"seq"`
	Kind      string    `json:"kind"`
	Author    *Author   `json:"author"`
	Text      string    `json:"text"`
	EventType *string   `json:"event_type"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
}

// ListItem — строка списка переписок.
type ListItem struct {
	ID           uuid.UUID    `json:"id"`
	Side         Side         `json:"side"`
	Vacancy      VacancyRef   `json:"vacancy"`
	Counterpart  Counterpart  `json:"counterpart"`
	Status       Status       `json:"status"`
	NeedsAnswer  bool         `json:"needs_answer"`
	LastMessage  *LastMessage `json:"last_message"`
	Unread       int          `json:"unread"`
	Archived     bool         `json:"archived"`
	CanWrite     bool         `json:"can_write"`
	ClosedReason *string      `json:"closed_reason"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Counts — числа вкладок стороны.
type Counts struct {
	Unread  int `json:"unread"`
	Archive int `json:"archive"`
}

// List — страница списка.
type List struct {
	Items      []ListItem `json:"items"`
	NextCursor *string    `json:"next_cursor"`
	Counts     Counts     `json:"counts"`
}

// UnreadCount — сколько переписок с непрочитанным вне архива у каждой стороны.
type UnreadCount struct {
	Total     int `json:"total"`
	Scientist int `json:"scientist"`
	Org       int `json:"org"`
}

// VacancyOption — вакансия с переписками для отбора у организации.
type VacancyOption struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	OrgName  string    `json:"org_name"`
	UnitName string    `json:"unit_name"`
	Chats    int       `json:"chats"`
}

// Page — страница ленты: от старых к новым; HasMore — есть ещё в сторону листания.
type Page struct {
	Items   []Message `json:"items"`
	HasMore bool      `json:"has_more"`
}

// ListQuery — параметры списка, как их прислал человек.
type ListQuery struct {
	Side      string
	Box       string
	Search    string
	VacancyID *uuid.UUID
	Cursor    string
	Limit     int
}

// MessagesQuery — параметры ленты. Не больше одного из Before и After.
type MessagesQuery struct {
	Before *int
	After  *int
	Limit  int
}

// SendInput — тело отправки.
type SendInput struct {
	Text     string `json:"text"`
	ClientID string `json:"client_id"`
}

// EditInput — тело правки.
type EditInput struct {
	Text string `json:"text"`
}

// ReadInput — тело отметки «прочитано».
type ReadInput struct {
	UpTo *int `json:"up_to"`
}
