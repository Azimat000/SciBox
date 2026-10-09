// Package httpcache — короткий кеш для публичных ответов, которые одинаковы для всех и тяжело считаются (срез 24):
// числа главной страницы, справочники. Кеш лежит в памяти копии сервера и живёт секунды-минуты: у копий он свой, но
// ответы публичные, и разница между копиями не больше срока жизни. Секретов и данных конкретного человека сюда не кладём.
//
// Вместе с кешем сервера ответ получает ETag и Cache-Control, чтобы браузер и общий кеш перед сервером не спрашивали
// чаще нужного, а на повторный запрос с If-None-Match получали 304 без тела.
package httpcache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Doc — готовый к выдаче ответ: тело JSON и его ETag.
type Doc struct {
	Body []byte
	ETag string
}

// Encode превращает значение в готовый ответ. ETag — отпечаток тела: тот же ответ, тот же ETag на любой копии сервера.
func Encode(v any) (Doc, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return Doc{}, fmt.Errorf("httpcache: encode: %w", err)
	}
	body = append(body, '\n')
	sum := sha256.Sum256(body)
	return Doc{Body: body, ETag: `"` + base64.RawURLEncoding.EncodeToString(sum[:12]) + `"`}, nil
}

// Cache хранит один готовый ответ на срок ttl. Пока ответ обновляется, остальные запросы ждут его: нагрузка на базу не
// умножается числом одновременных посетителей в момент, когда срок вышел.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	doc   Doc
	until time.Time
}

// New создаёт кеш на срок ttl. Срок не больше нуля заменяется на 1 секунду: кеш «навсегда» и кеш «никогда» оба ошибка.
func New(ttl time.Duration) *Cache { return newWithClock(ttl, time.Now) }

func newWithClock(ttl time.Duration, now func() time.Time) *Cache {
	if ttl <= 0 {
		ttl = time.Second
	}
	return &Cache{ttl: ttl, now: now}
}

// Get возвращает сохранённый ответ или собирает новый. Ошибка сборки не кешируется: следующий запрос попробует снова.
func (c *Cache) Get(load func() (any, error)) (Doc, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Before(c.until) {
		return c.doc, nil
	}
	v, err := load()
	if err != nil {
		return Doc{}, err
	}
	doc, err := Encode(v)
	if err != nil {
		return Doc{}, err
	}
	c.doc, c.until = doc, c.now().Add(c.ttl)
	return doc, nil
}

// Serve отвечает документом с ETag и Cache-Control: public, max-age. Если у запроса тот же ETag, отвечает 304 без тела.
func Serve(w http.ResponseWriter, r *http.Request, doc Doc, maxAge time.Duration) {
	h := w.Header()
	h.Set("ETag", doc.ETag)
	h.Set("Cache-Control", "public, max-age="+strconv.Itoa(int(maxAge/time.Second)))
	if matches(r.Header.Get("If-None-Match"), doc.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Body) // клиент мог уйти: писать в ответ больше некуда и нечего
}

// matches сверяет заголовок If-None-Match (список ETag через запятую, «*», слабые W/"…") с ETag ответа.
func matches(header, etag string) bool {
	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}
