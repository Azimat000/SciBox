package files

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

func pdf(n int) []byte {
	b := []byte("%PDF-1.7\n")
	if n > len(b) {
		b = append(b, bytes.Repeat([]byte("x"), n-len(b))...)
	}
	return b
}

func TestKindVisibleTo(t *testing.T) {
	// Строки: вид файла; столбцы: нет читателя, соискатель, сотрудник организации, неизвестный читатель.
	cases := []struct {
		kind                          Kind
		nobody, applicant, staff, odd bool
	}{
		{KindCV, false, true, true, false},
		{KindAttachment, false, true, true, false},
		{KindReferenceLetter, false, false, true, false}, // письмо соискатель получить не может
		{Kind("unknown"), false, false, false, false},
		{Kind(""), false, false, false, false},
	}
	for _, c := range cases {
		for _, r := range []struct {
			who  Reader
			want bool
		}{{0, c.nobody}, {Applicant, c.applicant}, {Staff, c.staff}, {Reader(99), c.odd}} {
			if got := c.kind.VisibleTo(r.who); got != r.want {
				t.Errorf("%q.VisibleTo(%d) = %v, want %v", c.kind, r.who, got, r.want)
			}
		}
	}
}

func TestParse(t *testing.T) {
	for _, k := range Kinds {
		if got, ok := Parse(string(k)); !ok || got != k {
			t.Errorf("Parse(%q) = %q, %v", k, got, ok)
		}
	}
	if _, ok := Parse("exe"); ok {
		t.Error("unknown kind parsed")
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"ok", pdf(100), nil},
		{"magic after small junk", append([]byte("\xef\xbb\xbf\n"), pdf(50)...), nil},
		{"exactly max size", pdf(MaxFileSize), nil},
		{"empty", nil, ErrEmpty},
		{"one byte over", pdf(MaxFileSize + 1), ErrTooBig},
		{"text", []byte("hello world"), ErrNotPDF},
		{"exe", []byte("MZ\x90\x00\x03"), ErrNotPDF},
		{"magic too far", append(bytes.Repeat([]byte("a"), headerWindow), pdf(20)...), ErrNotPDF},
	}
	for _, c := range cases {
		if got := Check(Upload{Name: "a.pdf", Data: c.data}); !errors.Is(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckSet(t *testing.T) {
	ok := Upload{Name: "ok.pdf", Data: pdf(50)}
	many := make([]Upload, MaxAttachments+1)
	for i := range many {
		many[i] = ok
	}
	big := func(n int) Upload { return Upload{Name: "b.pdf", Data: pdf(n)} }

	if err := CheckSet(nil); err != nil {
		t.Errorf("empty set: %v", err)
	}
	if err := CheckSet(many[:MaxAttachments]); err != nil {
		t.Errorf("max count: %v", err)
	}
	if err := CheckSet(many); !errors.Is(err, ErrTooMany) {
		t.Errorf("too many: %v", err)
	}
	err := CheckSet([]Upload{ok, {Name: "../evil.exe", Data: []byte("MZ")}})
	var fe *FileError
	if !errors.As(err, &fe) || fe.Name != "evil.exe.pdf" || !errors.Is(err, ErrNotPDF) {
		t.Errorf("bad file: %v", err)
	}
	if fe != nil && !strings.Contains(fe.Error(), "evil.exe.pdf") {
		t.Errorf("error text: %q", fe.Error())
	}
	// 3 файла по 9 МБ = 27 МБ > 25 МБ, каждый по отдельности допустим.
	if err := CheckSet([]Upload{big(9 << 20), big(9 << 20), big(9 << 20)}); !errors.Is(err, ErrTooBig) {
		t.Errorf("total too big: %v", err)
	}
	var fe2 *FileError
	if err := CheckSet([]Upload{big(9 << 20), big(9 << 20), big(9 << 20)}); errors.As(err, &fe2) {
		t.Errorf("total error must not name a file: %v", err)
	}
}

func TestCleanName(t *testing.T) {
	long := strings.Repeat("я", 300)
	cases := []struct{ in, want string }{
		{"Диплом.pdf", "Диплом.pdf"},
		{"Диплом.PDF", "Диплом.pdf"},
		{"Диплом", "Диплом.pdf"},
		{"  Диплом  .pdf", "Диплом.pdf"},
		{`C:\Users\me\Мой файл.pdf`, "Мой файл.pdf"},
		{"../../etc/passwd", "passwd.pdf"},
		{"a/b/c.pdf", "c.pdf"},
		{"bad:*?\"<>|name.pdf", "badname.pdf"},
		{"tab\tи\nперевод.pdf", "tabиперевод.pdf"},
		{"", "файл.pdf"},
		{".pdf", "файл.pdf"},
		{"...", "файл.pdf"},
		{"..", "файл.pdf"},
		{"/", "файл.pdf"},
		{"\xff\xfe.pdf", "файл.pdf"},
		{"файл.exe", "файл.exe.pdf"},
	}
	for _, c := range cases {
		if got := CleanName(c.in); got != c.want {
			t.Errorf("CleanName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	got := CleanName(long + ".pdf")
	if n := len([]rune(got)); n != maxNameRunes || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name has %d runes: %q", n, got)
	}
}

func TestServe(t *testing.T) {
	rec := httptest.NewRecorder()
	Serve(rec, "Письмо Орловой.pdf", pdf(30))
	h := rec.Header()
	checks := map[string]string{
		"Content-Type":            "application/pdf",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "sandbox; default-src 'none'",
		"Content-Length":          "30",
	}
	for k, want := range checks {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	cd := h.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "UTF-8''%D0%9F%D0%B8%D1%81%D1%8C%D0%BC%D0%BE%20%D0%9E%D1%80%D0%BB%D0%BE%D0%B2%D0%BE%D0%B9.pdf") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if rec.Body.Len() != 30 {
		t.Errorf("body %d bytes", rec.Body.Len())
	}
}

// ---- чтение запроса ----

type part struct {
	name, file string
	data       []byte
}

func formRequest(t *testing.T, parts ...part) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w interface{ Write([]byte) (int, error) }
		var err error
		if p.file != "" {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, p.name, p.file))
			h.Set("Content-Type", "application/pdf")
			w, err = mw.CreatePart(h)
		} else {
			w, err = mw.CreateFormField(p.name)
		}
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(p.data)
	}
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestReadForm(t *testing.T) {
	req := formRequest(t, part{name: "data", data: []byte(`{"a":1}`)}, part{name: "file", file: "один.pdf", data: pdf(40)}, part{name: "file", file: "два.pdf", data: pdf(50)})
	form, err := ReadForm(httptest.NewRecorder(), req, 1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(form.Data) != `{"a":1}` || len(form.Uploads) != 2 || form.Uploads[0].Name != "один.pdf" || len(form.Uploads[1].Data) != 50 {
		t.Errorf("form = %+v", form)
	}
}

func TestReadFormErrors(t *testing.T) {
	data := part{name: "data", data: []byte(`{}`)}
	file := part{name: "file", file: "a.pdf", data: pdf(20)}
	cases := []struct {
		name  string
		req   func() *http.Request
		want  error
		isFor string
	}{
		{"not multipart", func() *http.Request {
			r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
			r.Header.Set("Content-Type", "application/json")
			return r
		}, ErrBadForm, ""},
		{"no content type", func() *http.Request { return httptest.NewRequest("POST", "/", strings.NewReader("x")) }, ErrBadForm, ""},
		{"multipart without boundary", func() *http.Request {
			r := httptest.NewRequest("POST", "/", strings.NewReader("x"))
			r.Header.Set("Content-Type", "multipart/form-data")
			return r
		}, ErrBadForm, ""},
		{"no data part", func() *http.Request { return formRequest(t, file) }, ErrBadForm, ""},
		{"empty request", func() *http.Request { return formRequest(t) }, ErrBadForm, ""},
		{"two data parts", func() *http.Request { return formRequest(t, data, data) }, ErrBadForm, ""},
		{"unknown part", func() *http.Request { return formRequest(t, data, part{name: "extra", data: []byte("x")}) }, ErrBadForm, ""},
		{"too many files", func() *http.Request { return formRequest(t, data, file, file, file) }, ErrTooMany, ""},
		{"data too big", func() *http.Request { return formRequest(t, part{name: "data", data: bytes.Repeat([]byte("a"), 2000)}) }, ErrTooBig, ""},
		{"file too big", func() *http.Request {
			return formRequest(t, data, part{name: "file", file: "огромный.pdf", data: pdf(MaxFileSize + 1)})
		}, ErrTooBig, "огромный.pdf"},
		{"truncated body", func() *http.Request {
			r := formRequest(t, data, file)
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(r.Body)
			b := buf.Bytes()
			r.Body = http.NoBody
			r2 := httptest.NewRequest("POST", "/", bytes.NewReader(b[:len(b)/2]))
			r2.Header = r.Header
			return r2
		}, ErrBadForm, ""},
	}
	for _, c := range cases {
		_, err := ReadForm(httptest.NewRecorder(), c.req(), 1024, 2)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
			continue
		}
		if c.isFor != "" {
			var fe *FileError
			if !errors.As(err, &fe) || fe.Name != c.isFor {
				t.Errorf("%s: file name in error: %v", c.name, err)
			}
		}
	}
}

func TestReadFormCutInsideFile(t *testing.T) {
	r := formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(5000)})
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r.Body)
	b := buf.Bytes()
	cut := httptest.NewRequest("POST", "/", bytes.NewReader(b[:len(b)-100]))
	cut.Header = r.Header
	if _, err := ReadForm(httptest.NewRecorder(), cut, 1024, 2); !errors.Is(err, ErrBadForm) {
		t.Errorf("err = %v", err)
	}
}

func TestReadFormWholeBodyTooBig(t *testing.T) {
	// Мусор перед первой границей читается, пока не упрётся в общий предел запроса.
	body := bytes.Repeat([]byte("junk\n"), 100_000)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	_, err := ReadForm(httptest.NewRecorder(), req, 1024, 0)
	if !errors.Is(err, ErrBodyTooBig) {
		t.Errorf("err = %v", err)
	}
}

func TestDecode(t *testing.T) {
	type in struct {
		Text string `json:"text"`
	}
	good := func() *http.Request {
		return formRequest(t, part{name: "data", data: []byte(`{"text":"привет"}`)}, part{name: "file", file: "a.pdf", data: pdf(30)})
	}
	var got in
	rec := httptest.NewRecorder()
	ups, ok := Decode(rec, good(), &got, 1024, 2, "files")
	if !ok || got.Text != "привет" || len(ups) != 1 {
		t.Fatalf("ok=%v got=%+v ups=%d", ok, got, len(ups))
	}

	cases := []struct {
		name string
		req  *http.Request
		code int
		body string
	}{
		{"unknown json field", formRequest(t, part{name: "data", data: []byte(`{"nope":1}`)}), 400, "bad_request"},
		{"broken json", formRequest(t, part{name: "data", data: []byte(`{`)}), 400, "bad_request"},
		{"not multipart", httptest.NewRequest("POST", "/", strings.NewReader("{}")), 400, "bad_request"},
		{"file too big", formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "big.pdf", data: pdf(MaxFileSize + 1)}), 422, `"letter":"«big.pdf»: Файл больше 10 МБ"`},
		{"too many", formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(30)}, part{name: "file", file: "b.pdf", data: pdf(30)}, part{name: "file", file: "c.pdf", data: pdf(30)}), 422, "Слишком много файлов"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		if _, ok := Decode(rec, c.req, &in{}, 1024, 2, "letter"); ok {
			t.Errorf("%s: decoded", c.name)
			continue
		}
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestWriteError(t *testing.T) {
	cases := []struct {
		err  error
		code int
		body string
	}{
		{ErrBodyTooBig, 413, CodeBodyTooBig},
		{&FileError{Name: "a.pdf", Err: ErrNotPDF}, 422, "не похож на PDF"},
		{&FileError{Name: "a.pdf", Err: ErrEmpty}, 422, "Файл пустой"},
		{ErrTooBig, 422, "25 МБ"},
		{ErrTooMany, 422, "Слишком много файлов"},
		{ErrBadForm, 400, "bad_request"},
		{errors.New("other"), 400, "bad_request"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, c.err, "files")
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body)
		}
	}
	if _, ok := FieldMessage(errors.New("x")); ok {
		t.Error("foreign error got a message")
	}
}
