package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
)

var bg = context.Background()

type rig struct {
	store *blobstore.Memory
	av    *antivirus.Fake
	vault *Vault
}

func newRig() *rig {
	r := &rig{store: blobstore.NewMemory(), av: &antivirus.Fake{}}
	r.vault = NewVault(r.store, r.av, func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) })
	return r
}

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

func TestVaultPut(t *testing.T) {
	r := newRig()
	data := pdf(1000)
	st, err := r.vault.Put(bg, `C:\Мои документы\Диплом.pdf`, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.Key, KeyPrefix) || strings.Contains(st.Key, "Диплом") || st.Size != 1000 || st.Scan != ScanClean || st.ScannedAt.IsZero() {
		t.Errorf("stored = %+v", st)
	}
	if st.Sum != sha256.Sum256(data) {
		t.Error("checksum differs from the checksum of the file")
	}
	if got := r.store.Count(); got != 1 || !r.store.Has(st.Key) {
		t.Errorf("objects in the storage: %v", r.store.Keys())
	}
	if r.av.Scans() != 1 {
		t.Errorf("scans: %d", r.av.Scans())
	}
	// У каждой загрузки свой ключ, даже у одинакового файла.
	st2, err := r.vault.Put(bg, "Диплом.pdf", bytes.NewReader(data))
	if err != nil || st2.Key == st.Key {
		t.Errorf("second key %q (%v)", st2.Key, err)
	}
}

func TestVaultPutRejectsBeforeWriting(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrEmpty},
		{"text", []byte("hello world"), ErrNotPDF},
		{"exe", []byte("MZ\x90\x00\x03"), ErrNotPDF},
		{"magic too far", append(bytes.Repeat([]byte("a"), headerWindow), pdf(20)...), ErrNotPDF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig()
			_, err := r.vault.Put(bg, "../evil.pdf", bytes.NewReader(c.data))
			var fe *FileError
			if !errors.Is(err, c.want) || !errors.As(err, &fe) || fe.Name != "evil.pdf" {
				t.Fatalf("err = %v, want %v for evil.pdf", err, c.want)
			}
			if r.store.Count() != 0 || r.av.Scans() != 0 {
				t.Errorf("a refused file reached the storage (%d) or the antivirus (%d)", r.store.Count(), r.av.Scans())
			}
		})
	}
	t.Run("magic after small junk is fine", func(t *testing.T) {
		if _, err := newRig().vault.Put(bg, "a.pdf", bytes.NewReader(append([]byte("\xef\xbb\xbf\n"), pdf(50)...))); err != nil {
			t.Fatal(err)
		}
	})
}

func TestVaultPutSizeLimit(t *testing.T) {
	t.Run("exactly the maximum", func(t *testing.T) {
		r := newRig()
		st, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(pdf(MaxFileSize)))
		if err != nil || st.Size != MaxFileSize {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("one byte over leaves nothing", func(t *testing.T) {
		r := newRig()
		_, err := r.vault.Put(bg, "big.pdf", bytes.NewReader(pdf(MaxFileSize+1)))
		var fe *FileError
		if !errors.Is(err, ErrTooBig) || !errors.As(err, &fe) || fe.Name != "big.pdf" {
			t.Fatalf("err = %v", err)
		}
		if r.store.Count() != 0 || r.av.Scans() != 0 {
			t.Errorf("objects %d, scans %d", r.store.Count(), r.av.Scans())
		}
	})
	t.Run("an endless stream is cut near the limit", func(t *testing.T) {
		r := newRig()
		src := &countingReader{prefix: []byte("%PDF-1.7\n")}
		_, err := r.vault.Put(bg, "endless.pdf", src)
		if !errors.Is(err, ErrTooBig) {
			t.Fatalf("err = %v", err)
		}
		// Прочитано не больше предела плюс запас буфера проверки первых байт: поток не вычерпывается.
		if src.n > MaxFileSize+4096 {
			t.Errorf("read %d bytes from an endless stream", src.n)
		}
		if r.store.Count() != 0 {
			t.Error("a cut upload stayed in the storage")
		}
	})
}

// countingReader — бесконечный поток после заданного начала; считает прочитанное.
type countingReader struct {
	prefix []byte
	n      int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n := 0
	if int64(len(c.prefix)) > c.n {
		n = copy(p, c.prefix[c.n:])
	}
	for ; n < len(p); n++ {
		p[n] = 'x'
	}
	c.n += int64(n)
	return n, nil
}

func TestVaultPutErrors(t *testing.T) {
	t.Run("the request breaks in the middle: the reader's error comes back and nothing stays", func(t *testing.T) {
		r := newRig()
		boom := errors.New("connection reset")
		_, err := r.vault.Put(bg, "a.pdf", io.MultiReader(bytes.NewReader(pdf(50_000)), brokenReader{boom}))
		if !errors.Is(err, boom) || errors.Is(err, ErrStorage) {
			t.Fatalf("err = %v", err)
		}
		if r.store.Count() != 0 {
			t.Error("a broken upload stayed in the storage")
		}
	})
	t.Run("the request breaks before the first bytes", func(t *testing.T) {
		boom := errors.New("connection reset")
		if _, err := newRig().vault.Put(bg, "a.pdf", brokenReader{boom}); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the storage fails", func(t *testing.T) {
		r := newRig()
		r.store.Fault = func(op, _ string) error {
			if op == "put" {
				return errors.New("503 slow down")
			}
			return nil
		}
		_, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(pdf(100)))
		if !errors.Is(err, ErrStorage) {
			t.Fatalf("err = %v", err)
		}
		if r.av.Scans() != 0 {
			t.Error("a file that was not stored was scanned")
		}
	})
}

type brokenReader struct{ err error }

func (b brokenReader) Read([]byte) (int, error) { return 0, b.err }

func TestVaultPutVirus(t *testing.T) {
	t.Run("a virus is refused and the object removed", func(t *testing.T) {
		r := newRig()
		data := append(pdf(100), antivirus.EICAR()...)
		_, err := r.vault.Put(bg, "invoice.pdf", bytes.NewReader(data))
		var fe *FileError
		var inf *InfectedError
		if !errors.Is(err, ErrInfected) || !errors.As(err, &fe) || fe.Name != "invoice.pdf" || !errors.As(err, &inf) || inf.Signature == "" {
			t.Fatalf("err = %v", err)
		}
		if r.store.Count() != 0 {
			t.Errorf("infected object stayed in the storage: %v", r.store.Keys())
		}
		msg, ok := FieldMessage(err)
		if !ok || !strings.Contains(msg, "«invoice.pdf»") || !strings.Contains(msg, "вирус") || strings.Contains(msg, "Eicar") {
			t.Errorf("message for the person: %q", msg)
		}
	})
	t.Run("the antivirus is down: the file waits for the check and is not clean", func(t *testing.T) {
		r := newRig()
		r.av.Fault = func() error { return antivirus.ErrUnavailable }
		st, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(pdf(100)))
		if err != nil {
			t.Fatal(err)
		}
		if st.Scan != ScanPending || !st.ScannedAt.IsZero() || st.Scan.Available() {
			t.Errorf("stored = %+v", st)
		}
		if !r.store.Has(st.Key) {
			t.Error("a file waiting for the check was not kept")
		}
	})
	t.Run("an antivirus error of any kind is never taken for clean", func(t *testing.T) {
		r := newRig()
		r.av.Fault = func() error { return errors.New("something odd") }
		st, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(pdf(100)))
		if err != nil || st.Scan != ScanPending {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("the object cannot be read back for the check: pending", func(t *testing.T) {
		r := newRig()
		r.store.Fault = func(op, _ string) error {
			if op == "get" {
				return errors.New("storage hiccup")
			}
			return nil
		}
		st, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(pdf(100)))
		if err != nil || st.Scan != ScanPending {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("a failed delete of an infected object does not let the file through", func(t *testing.T) {
		r := newRig()
		r.store.Fault = func(op, _ string) error {
			if op == "delete" {
				return errors.New("storage hiccup")
			}
			return nil
		}
		_, err := r.vault.Put(bg, "a.pdf", bytes.NewReader(append(pdf(100), antivirus.EICAR()...)))
		if !errors.Is(err, ErrInfected) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestVaultPutTrustedOpenDiscard(t *testing.T) {
	r := newRig()
	data := pdf(500)
	st, err := r.vault.PutTrusted(bg, "CV.pdf", data)
	if err != nil || st.Scan != ScanClean || st.Size != 500 || st.Sum != sha256.Sum256(data) || st.ScannedAt.IsZero() {
		t.Fatalf("%+v %v", st, err)
	}
	if r.av.Scans() != 0 {
		t.Error("a file made by the server was sent to the antivirus")
	}
	obj, err := r.vault.Open(bg, st.Key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(obj)
	_ = obj.Close()
	if !bytes.Equal(got, data) || obj.Size() != 500 {
		t.Error("opened file differs")
	}
	if _, err := r.vault.Open(bg, KeyPrefix+"nope"); !errors.Is(err, blobstore.ErrNotFound) || errors.Is(err, ErrStorage) {
		t.Errorf("missing object: %v", err)
	}
	r.store.Fault = func(op, _ string) error { return errors.New("down") }
	if _, err := r.vault.Open(bg, st.Key); !errors.Is(err, ErrStorage) {
		t.Errorf("storage down: %v", err)
	}
	if _, err := r.vault.PutTrusted(bg, "x.pdf", data); !errors.Is(err, ErrStorage) {
		t.Errorf("trusted put with storage down: %v", err)
	}
	r.store.Fault = nil

	// Discard работает и после отмены запроса, и без списка, и не падает на сбое.
	a, _ := r.vault.PutTrusted(bg, "a.pdf", data)
	b, _ := r.vault.PutTrusted(bg, "b.pdf", data)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	r.vault.Discard(ctx, a, b)
	r.vault.Discard(bg)
	if r.store.Has(a.Key) || r.store.Has(b.Key) {
		t.Error("discarded files are still in the storage")
	}
	r.store.Fault = func(op, _ string) error { return errors.New("down") }
	r.vault.Discard(bg, st) // сбой удаления не паникует и не возвращает ошибку
}

func TestVaultAccessors(t *testing.T) {
	r := newRig()
	if r.vault.Store() != blobstore.Store(r.store) || r.vault.Scanner() != antivirus.Scanner(r.av) || r.vault.Now().Year() != 2026 {
		t.Error("accessors return something else")
	}
	if !ScanClean.Available() || ScanPending.Available() || ScanInfected.Available() || ScanStatus("").Available() {
		t.Error("only a clean file is available")
	}
}

func TestVaultCheck(t *testing.T) {
	r := newRig()
	st, _ := r.vault.PutTrusted(bg, "a.pdf", pdf(100))
	if v, err := r.vault.Check(bg, st.Key); err != nil || v.Infected {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := r.vault.Check(bg, KeyPrefix+"gone"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("missing object: %v", err)
	}
}

func TestCheckSet(t *testing.T) {
	ok := Stored{Name: "ok.pdf", Size: 50}
	many := make([]Stored, MaxAttachments+1)
	for i := range many {
		many[i] = ok
	}
	if err := CheckSet(nil); err != nil {
		t.Errorf("empty set: %v", err)
	}
	if err := CheckSet(many[:MaxAttachments]); err != nil {
		t.Errorf("max count: %v", err)
	}
	if err := CheckSet(many); !errors.Is(err, ErrTooMany) {
		t.Errorf("too many: %v", err)
	}
	// 3 файла по 9 МБ = 27 МБ > 25 МБ, каждый по отдельности допустим.
	big := Stored{Name: "b.pdf", Size: 9 << 20}
	err := CheckSet([]Stored{big, big, big})
	var fe *FileError
	if !errors.Is(err, ErrTooBig) || errors.As(err, &fe) {
		t.Errorf("total too big (must not name a file): %v", err)
	}
	if err := CheckSet([]Stored{big, big}); err != nil {
		t.Errorf("18 MB: %v", err)
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
	if err := Serve(rec, "Письмо Орловой.pdf", 30, bytes.NewReader(pdf(30))); err != nil {
		t.Fatal(err)
	}
	h := rec.Header()
	checks := map[string]string{
		"Content-Type":            "application/pdf",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "sandbox; default-src 'none'; frame-ancestors 'none'",
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

func TestServeReportsATruncatedStream(t *testing.T) {
	// Поток короче заявленной длины: ошибка возвращается, чтобы её увидел журнал (браузер заметит обрыв по Content-Length).
	if err := Serve(httptest.NewRecorder(), "a.pdf", 100, bytes.NewReader(pdf(30))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("short stream: %v", err)
	}
	boom := errors.New("storage broke")
	if err := Serve(httptest.NewRecorder(), "a.pdf", 100, io.MultiReader(bytes.NewReader(pdf(30)), brokenReader{boom})); !errors.Is(err, boom) {
		t.Errorf("broken stream: %v", err)
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
	r := newRig()
	req := formRequest(t, part{name: "data", data: []byte(`{"a":1}`)}, part{name: "file", file: "один.pdf", data: pdf(40)}, part{name: "file", file: "два.pdf", data: pdf(50)})
	form, err := ReadForm(bg, httptest.NewRecorder(), req, r.vault, 1024, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(form.Data) != `{"a":1}` || len(form.Files) != 2 || form.Files[0].Name != "один.pdf" || form.Files[1].Size != 50 {
		t.Errorf("form = %+v", form)
	}
	if r.store.Count() != 2 {
		t.Errorf("objects: %d", r.store.Count())
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
		{"file before data", func() *http.Request { return formRequest(t, file, data) }, ErrBadForm, ""},
		{"empty request", func() *http.Request { return formRequest(t) }, ErrBadForm, ""},
		{"two data parts", func() *http.Request { return formRequest(t, data, data) }, ErrBadForm, ""},
		{"unknown part", func() *http.Request { return formRequest(t, data, part{name: "extra", data: []byte("x")}) }, ErrBadForm, ""},
		{"too many files", func() *http.Request { return formRequest(t, data, file, file, file) }, ErrTooMany, ""},
		{"data too big", func() *http.Request { return formRequest(t, part{name: "data", data: bytes.Repeat([]byte("a"), 2000)}) }, ErrTooBig, ""},
		{"file too big", func() *http.Request {
			return formRequest(t, data, part{name: "file", file: "огромный.pdf", data: pdf(MaxFileSize + 1)})
		}, ErrTooBig, "огромный.pdf"},
		{"file not a pdf", func() *http.Request {
			return formRequest(t, data, file, part{name: "file", file: "virus.pdf", data: []byte("MZ\x90\x00")})
		}, ErrNotPDF, "virus.pdf"},
		{"file with a virus", func() *http.Request {
			return formRequest(t, data, file, part{name: "file", file: "bad.pdf", data: append(pdf(50), antivirus.EICAR()...)})
		}, ErrInfected, "bad.pdf"},
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
		t.Run(c.name, func(t *testing.T) {
			r := newRig()
			_, err := ReadForm(bg, httptest.NewRecorder(), c.req(), r.vault, 1024, 2, nil)
			if !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
			if c.isFor != "" {
				var fe *FileError
				if !errors.As(err, &fe) || fe.Name != c.isFor {
					t.Errorf("file name in error: %v", err)
				}
			}
			// Любая ошибка забирает за собой уже принятые файлы: в хранилище ничего не остаётся.
			if r.store.Count() != 0 {
				t.Errorf("a refused request left %d objects in the storage", r.store.Count())
			}
		})
	}
}

func TestReadFormTotalSizeLimit(t *testing.T) {
	r := newRig()
	big := part{name: "file", file: "b.pdf", data: pdf(9 << 20)}
	req := formRequest(t, part{name: "data", data: []byte(`{}`)}, big, big, big)
	_, err := ReadForm(bg, httptest.NewRecorder(), req, r.vault, 1024, 3, nil)
	var fe *FileError
	if !errors.Is(err, ErrTooBig) || errors.As(err, &fe) {
		t.Fatalf("err = %v (must be the total limit, not a single file)", err)
	}
	if r.store.Count() != 0 {
		t.Errorf("a request over the total limit left %d objects", r.store.Count())
	}
}

func TestReadFormStopsBeforeTheFilesWhenTheFieldsAreWrong(t *testing.T) {
	r := newRig()
	refuse := errors.New("fields are wrong")
	req := formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(5000)})
	_, err := ReadForm(bg, httptest.NewRecorder(), req, r.vault, 1024, 2, func([]byte) error { return refuse })
	if !errors.Is(err, refuse) {
		t.Fatalf("err = %v", err)
	}
	if r.store.Count() != 0 || r.av.Scans() != 0 {
		t.Errorf("a request refused by its fields reached the storage (%d) or the antivirus (%d)", r.store.Count(), r.av.Scans())
	}
}

func TestReadFormStorageFailure(t *testing.T) {
	r := newRig()
	calls := 0
	r.store.Fault = func(op, _ string) error {
		if op == "put" {
			if calls++; calls == 2 {
				return errors.New("storage down")
			}
		}
		return nil
	}
	req := formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(50)}, part{name: "file", file: "b.pdf", data: pdf(50)})
	_, err := ReadForm(bg, httptest.NewRecorder(), req, r.vault, 1024, 2, nil)
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("err = %v", err)
	}
	if r.store.Count() != 0 {
		t.Error("the first file stayed in the storage after the second one failed")
	}
}

func TestReadFormCutInsideFile(t *testing.T) {
	r := newRig()
	req := formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(5000)})
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(req.Body)
	b := buf.Bytes()
	cut := httptest.NewRequest("POST", "/", bytes.NewReader(b[:len(b)-100]))
	cut.Header = req.Header
	if _, err := ReadForm(bg, httptest.NewRecorder(), cut, r.vault, 1024, 2, nil); !errors.Is(err, ErrBadForm) {
		t.Errorf("err = %v", err)
	}
	if r.store.Count() != 0 {
		t.Error("a cut upload stayed in the storage")
	}
}

func TestReadFormWholeBodyTooBig(t *testing.T) {
	// Мусор перед первой границей читается, пока не упрётся в общий предел запроса.
	body := bytes.Repeat([]byte("junk\n"), 100_000)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	_, err := ReadForm(bg, httptest.NewRecorder(), req, newRig().vault, 1024, 0, nil)
	if !errors.Is(err, ErrBodyTooBig) {
		t.Errorf("err = %v", err)
	}
}

func TestDecode(t *testing.T) {
	type in struct {
		Text string `json:"text"`
	}
	r := newRig()
	good := func() *http.Request {
		return formRequest(t, part{name: "data", data: []byte(`{"text":"привет"}`)}, part{name: "file", file: "a.pdf", data: pdf(30)})
	}
	var got in
	var seen string
	ups, err := Decode(bg, httptest.NewRecorder(), good(), r.vault, &got, 1024, 2, func() error { seen = got.Text; return nil })
	if err != nil || got.Text != "привет" || len(ups) != 1 {
		t.Fatalf("err=%v got=%+v ups=%d", err, got, len(ups))
	}
	if seen != "привет" {
		t.Errorf("the check ran before the fields were read: %q", seen)
	}

	cases := []struct {
		name string
		req  *http.Request
		want error
	}{
		{"unknown json field", formRequest(t, part{name: "data", data: []byte(`{"nope":1}`)}), ErrBadJSON},
		{"broken json", formRequest(t, part{name: "data", data: []byte(`{`)}), ErrBadJSON},
		{"not multipart", httptest.NewRequest("POST", "/", strings.NewReader("{}")), ErrBadForm},
		{"file too big", formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "big.pdf", data: pdf(MaxFileSize + 1)}), ErrTooBig},
		{"too many", formRequest(t, part{name: "data", data: []byte(`{}`)}, part{name: "file", file: "a.pdf", data: pdf(30)}, part{name: "file", file: "b.pdf", data: pdf(30)}, part{name: "file", file: "c.pdf", data: pdf(30)}), ErrTooMany},
	}
	for _, c := range cases {
		before := r.store.Count()
		if _, err := Decode(bg, httptest.NewRecorder(), c.req, r.vault, &in{}, 1024, 2, nil); !errors.Is(err, c.want) || !IsUploadError(err) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if r.store.Count() != before {
			t.Errorf("%s: left objects in the storage", c.name)
		}
	}

	// Проверка полей отвергает запрос до файлов, и её ошибка возвращается как есть (это не ошибка загрузки).
	veto := errors.New("veto")
	before := r.av.Scans()
	if _, err := Decode(bg, httptest.NewRecorder(), good(), r.vault, &in{}, 1024, 2, func() error { return veto }); !errors.Is(err, veto) || IsUploadError(err) {
		t.Errorf("veto: %v", err)
	}
	if r.av.Scans() != before {
		t.Error("a vetoed request was sent to the antivirus")
	}
}

func TestIsUploadError(t *testing.T) {
	for _, err := range []error{ErrBadForm, ErrBadJSON, ErrTooMany, ErrTooBig, ErrBodyTooBig, ErrStorage, &FileError{Name: "a", Err: ErrEmpty}} {
		if !IsUploadError(err) {
			t.Errorf("%v is an upload error", err)
		}
	}
	if IsUploadError(errors.New("other")) || IsUploadError(nil) {
		t.Error("foreign error taken for an upload error")
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
		{&FileError{Name: "a.pdf", Err: &InfectedError{Signature: "Eicar"}}, 422, "вирус"},
		{ErrTooBig, 422, "25 МБ"},
		{ErrTooMany, 422, "Слишком много файлов"},
		{fmt.Errorf("%w: boom", ErrStorage), 503, CodeStorageUnavailable},
		{ErrBadForm, 400, "bad_request"},
		{ErrBadJSON, 400, "bad_request"},
		{errors.New("other"), 400, "bad_request"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, c.err, "files")
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "boom") || strings.Contains(rec.Body.String(), "Eicar") {
			t.Errorf("%v: internal details in the answer: %s", c.err, rec.Body)
		}
	}
	if _, ok := FieldMessage(errors.New("x")); ok {
		t.Error("foreign error got a message")
	}
}
