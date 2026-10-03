package applications

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"scibox/server/internal/auth"
)

// Пределы приглашений (совпадают с CHECK в базе, где они есть).
const (
	maxMessageRunes = 2000
	maxPlaceRunes   = 500
	maxNameRunes    = 120
	maxPhoneRunes   = 40
	maxNoteRunes    = 1000
	maxAnswerRunes  = 300
	// Не больше стольких приглашений на один отклик (защита соискателя от потока писем).
	maxInvitations = 10
	// Собеседование и предложенное время назначаются не дальше чем на год вперёд.
	maxAhead = 366 * 24 * time.Hour
)

// Форматы собеседования.
const (
	PlaceOnline = "online"
	PlaceOnsite = "onsite"
)

// Тексты ошибок полей.
const (
	msgKind          = "Выберите вид приглашения"
	msgMessageLong   = "Сообщение длиннее 2000 знаков"
	msgStartsNeeded  = "Укажите дату и время собеседования"
	msgTimeBad       = "Не удалось понять дату и время"
	msgTimePast      = "Это время уже прошло. Выберите время в будущем"
	msgTimeFar       = "Назначайте не дальше чем на год вперёд"
	msgPlaceKind     = "Выберите формат: онлайн или очно"
	msgLinkNeeded    = "Вставьте ссылку на встречу"
	msgLinkBad       = "Нужна ссылка вида https://…"
	msgAddressNeeded = "Укажите адрес"
	msgPlaceLong     = "Не больше 500 знаков"
	msgContactNeeded = "Укажите почту или телефон"
	msgPhoneBad      = "Телефон: цифры, плюс, пробелы, скобки и дефисы (от 5 цифр)"
	msgNameLong      = "Не больше 120 знаков"
	msgActionBad     = "Такой ответ на это приглашение не предусмотрен"
	msgNoteLong      = "Не больше 1000 знаков"
	msgContactAnswer = "Оставьте почту, телефон или другой способ связи"
	msgAnswerLong    = "Не больше 300 знаков"
)

// InvitationInput — приглашение, как его присылает сайт. Поля чужого вида отбрасываются.
type InvitationInput struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// StartsAt — начало собеседования, RFC 3339 (сайт присылает московское время со смещением +03:00).
	StartsAt     string `json:"starts_at"`
	PlaceKind    string `json:"place_kind"`
	Place        string `json:"place"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
}

// validInvitation — проверенное приглашение, готовое к записи.
type validInvitation struct {
	Kind, Message                           string
	StartsAt                                *time.Time
	PlaceKind                               *string
	Place, ContactName, ContactEmail, Phone string
}

// parseMoment разбирает время ответа сайта и проверяет, что оно в пределах от «сейчас» до года вперёд.
func parseMoment(raw string, now time.Time, needed string) (t time.Time, msg string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, needed
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, msgTimeBad
	}
	t = t.UTC().Truncate(time.Minute)
	switch {
	case !t.After(now):
		return time.Time{}, msgTimePast
	case t.After(now.Add(maxAhead)):
		return time.Time{}, msgTimeFar
	}
	return t, ""
}

// cleanPhone: телефон без лишних пробелов; цифр от 5 до 15, допустимы только цифры, плюс в начале, пробелы, скобки, дефисы.
func cleanPhone(raw string) (string, bool) {
	p := strings.Join(strings.Fields(raw), " ")
	digits := 0
	for i, r := range p {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+' && i == 0, r == ' ', r == '(', r == ')', r == '-':
		default:
			return "", false
		}
	}
	return p, digits >= 5 && digits <= 15 && utf8.RuneCountInString(p) <= maxPhoneRunes
}

// validateInvitation проверяет приглашение и возвращает ошибки по полям.
func validateInvitation(in InvitationInput, now time.Time) (validInvitation, map[string]string) {
	errs := map[string]string{}
	v := validInvitation{Kind: in.Kind, Message: strings.TrimSpace(in.Message)}
	if !isOneOf(in.Kind, InvitationKinds) {
		errs["kind"] = msgKind
		return v, errs
	}
	if utf8.RuneCountInString(v.Message) > maxMessageRunes {
		errs["message"] = msgMessageLong
	}
	switch in.Kind {
	case InvInterview:
		if t, msg := parseMoment(in.StartsAt, now, msgStartsNeeded); msg != "" {
			errs["starts_at"] = msg
		} else {
			v.StartsAt = &t
		}
		v.Place = strings.TrimSpace(in.Place)
		switch in.PlaceKind {
		case PlaceOnline, PlaceOnsite:
			kind := in.PlaceKind
			v.PlaceKind = &kind
			if msg := checkPlace(kind, v.Place); msg != "" {
				errs["place"] = msg
			}
		default:
			errs["place_kind"] = msgPlaceKind
		}
	case InvContacts:
		v.ContactName = strings.Join(strings.Fields(in.ContactName), " ")
		if utf8.RuneCountInString(v.ContactName) > maxNameRunes {
			errs["contact_name"] = msgNameLong
		}
		email := strings.TrimSpace(in.ContactEmail)
		phone := strings.TrimSpace(in.ContactPhone)
		if email == "" && phone == "" {
			errs["contact_email"] = msgContactNeeded
			break
		}
		if email != "" {
			norm, msg := auth.NormalizeEmail(email)
			if msg != "" {
				errs["contact_email"] = msg
			}
			v.ContactEmail = norm
		}
		if phone != "" {
			p, ok := cleanPhone(phone)
			if !ok {
				errs["contact_phone"] = msgPhoneBad
			}
			v.Phone = p
		}
	}
	return v, errs
}

// checkPlace: онлайн — ссылка http(s), очно — адрес.
func checkPlace(kind, place string) string {
	switch {
	case place == "" && kind == PlaceOnline:
		return msgLinkNeeded
	case place == "":
		return msgAddressNeeded
	case utf8.RuneCountInString(place) > maxPlaceRunes:
		return msgPlaceLong
	case kind == PlaceOnline:
		u, err := url.Parse(place)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(place, " \t\r\n") {
			return msgLinkBad
		}
	}
	return ""
}

// AnswerInput — ответ соискателя на приглашение.
type AnswerInput struct {
	Action string `json:"action"`
	// ProposedAt — другое время собеседования (RFC 3339), только для action = propose.
	ProposedAt string `json:"proposed_at"`
	Note       string `json:"note"`
	// Contact и Time — для action = reply: как связаться и когда удобно.
	Contact string `json:"contact"`
	Time    string `json:"time"`
}

// validAnswer — проверенный ответ.
type validAnswer struct {
	Status     string
	ProposedAt *time.Time
	Note       string
	Contact    string
	Time       string
}

// validateAnswer проверяет ответ на приглашение вида kind.
func validateAnswer(kind string, in AnswerInput, now time.Time) (validAnswer, map[string]string) {
	errs := map[string]string{}
	status, ok := AnswerTarget(kind, in.Action)
	if !ok {
		errs["action"] = msgActionBad
		return validAnswer{}, errs
	}
	v := validAnswer{Status: status, Note: strings.TrimSpace(in.Note)}
	if utf8.RuneCountInString(v.Note) > maxNoteRunes {
		errs["note"] = msgNoteLong
	}
	switch in.Action {
	case AnswerPropose:
		if t, msg := parseMoment(in.ProposedAt, now, msgStartsNeeded); msg != "" {
			errs["proposed_at"] = msg
		} else {
			v.ProposedAt = &t
		}
	case AnswerReply:
		v.Contact = strings.Join(strings.Fields(in.Contact), " ")
		v.Time = strings.Join(strings.Fields(in.Time), " ")
		switch {
		case v.Contact == "":
			errs["contact"] = msgContactAnswer
		case utf8.RuneCountInString(v.Contact) > maxAnswerRunes:
			errs["contact"] = msgAnswerLong
		}
		if utf8.RuneCountInString(v.Time) > maxAnswerRunes {
			errs["time"] = msgAnswerLong
		}
	}
	return v, errs
}
