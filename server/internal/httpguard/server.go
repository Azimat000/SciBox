package httpguard

import (
	"net/http"
	"time"
)

// Timeouts — сроки соединения (T-04). Без них медленный клиент держит соединение сколько угодно
// (атака slowloris), а сервер копит открытые соединения и горутины.
type Timeouts struct {
	// Header — на приём заголовков запроса.
	Header time.Duration
	// Read — на приём всего запроса вместе с телом.
	Read time.Duration
	// Write — на ответ: от конца приёма заголовков до конца ответа.
	Write time.Duration
	// Idle — сколько держать открытым соединение между запросами.
	Idle time.Duration
	// Transfer — сроки чтения и записи для долгих передач (Route.Slow): загрузка файлов отклика,
	// выдача файлов и PDF. 25 МБ по медленной связи (1 Мбит/с) идут около 3,5 минут.
	Transfer time.Duration
}

// DefaultTimeouts — сроки сервера в работе. Обычный запрос к API отвечает за доли секунды.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Header:   10 * time.Second,
		Read:     30 * time.Second,
		Write:    60 * time.Second,
		Idle:     2 * time.Minute,
		Transfer: 5 * time.Minute,
	}
}

// MaxHeaderBytes — предел заголовков запроса. Наши заголовки (cookie сессии, адреса) занимают
// около килобайта; по умолчанию в Go целый мегабайт.
const MaxHeaderBytes = 64 << 10

// NewServer — HTTP-сервер со сроками t и пределом заголовков. Нулевой или отрицательный срок заменяется
// сроком из DefaultTimeouts: у http.Server он означал бы «без срока».
func NewServer(h http.Handler, t Timeouts) *http.Server {
	t = t.orDefaults()
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: t.Header,
		ReadTimeout:       t.Read,
		WriteTimeout:      t.Write,
		IdleTimeout:       t.Idle,
		MaxHeaderBytes:    MaxHeaderBytes,
	}
}

func (t Timeouts) orDefaults() Timeouts {
	d := DefaultTimeouts()
	return Timeouts{
		Header:   positive(t.Header, d.Header),
		Read:     positive(t.Read, d.Read),
		Write:    positive(t.Write, d.Write),
		Idle:     positive(t.Idle, d.Idle),
		Transfer: positive(t.Transfer, d.Transfer),
	}
}

// positive — срок d, если он больше нуля, иначе запасной.
func positive(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
