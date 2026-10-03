package testkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/auth"
)

// API — настоящие обработчики аккаунтов и проверяемого раздела за настоящим роутером.
type API struct {
	*World
	H http.Handler
}

// NewAPI собирает роутер: аккаунты (проверка источника запроса и вход) и то, что подключит mount.
func NewAPI(w *World, mount func(r chi.Router, requireUser func(http.Handler) http.Handler)) *API {
	authH := auth.NewHandler(w.Auth, w.Log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(authH.SameOrigin, authH.Authenticate)
		mount(r, authH.RequireUser)
	})
	return &API{World: w, H: r}
}

// Reply — ответ сервера.
type Reply struct {
	Code   int
	Header http.Header
	Raw    string
}

// JSON разбирает тело ответа как объект.
func (r Reply) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Raw), &m); err != nil {
		t.Fatalf("not JSON (%d): %s", r.Code, r.Raw)
	}
	return m
}

// ErrCode — код ошибки из ответа.
func (r Reply) ErrCode(t *testing.T) string {
	t.Helper()
	e, _ := r.JSON(t)["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

// Fields — поля с ошибками из ответа 422.
func (r Reply) Fields(t *testing.T) map[string]any {
	t.Helper()
	e, _ := r.JSON(t)["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	return f
}

// Do отправляет запрос; body — строка (как есть) или любое значение (в JSON). who == nil — аноним.
func (a *API) Do(who *Person, method, path string, body any) Reply {
	a.T.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.send(who, req)
}

// FilePart — файл в multipart-запросе.
type FilePart struct {
	Name string
	Data []byte
}

// DoForm отправляет multipart-запрос: часть data с JSON и части file с файлами.
func (a *API) DoForm(who *Person, method, path string, data any, files ...FilePart) Reply {
	a.T.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(data)
	fw, _ := mw.CreateFormField("data")
	_, _ = fw.Write(raw)
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f.Name))
		h.Set("Content-Type", "application/pdf")
		pw, _ := mw.CreatePart(h)
		_, _ = pw.Write(f.Data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.send(who, req)
}

func (a *API) send(who *Person, req *http.Request) Reply {
	a.T.Helper()
	if who != nil {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.Token})
	}
	rec := httptest.NewRecorder()
	a.H.ServeHTTP(rec, req)
	return Reply{Code: rec.Code, Header: rec.Header(), Raw: rec.Body.String()}
}
