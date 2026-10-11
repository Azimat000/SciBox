package files

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzCleanName: имя из чего угодно безопасно кладётся в заголовок Content-Disposition и в журнал: без путей, управляющих
// и обманных знаков (переключатели направления письма), кавычек и двоеточий; короткое, валидный UTF-8, всегда .pdf.
func FuzzCleanName(f *testing.F) {
	for _, s := range []string{
		"cv.pdf", "../../etc/passwd", `C:\Users\x\cv.pdf`, "a\r\nSet-Cookie: x=1", "report\u202etxt.exe", "\x00", "",
		strings.Repeat("я", 400), "..pdf", `"; filename="evil.exe`, "\xff\xfe", "a\u200bb", "x\u2028y",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := CleanName(raw)
		if !utf8.ValidString(got) || !strings.HasSuffix(got, ".pdf") || utf8.RuneCountInString(got) > maxNameRunes {
			t.Fatalf("CleanName(%q) = %q: bad shape", raw, got)
		}
		for _, r := range got {
			if unicode.IsControl(r) || spoofing(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
				t.Fatalf("CleanName(%q) = %q: contains U+%04X", raw, got, r)
			}
		}
		if again := CleanName(got); again != got {
			t.Fatalf("CleanName is not idempotent: %q -> %q -> %q", raw, got, again)
		}
	})
}

// FuzzReadForm: любое тело запроса не роняет разбор; отказ не оставляет в хранилище ничего; успех не принимает больше
// файлов, чем разрешено, и данных больше предела.
func FuzzReadForm(f *testing.F) {
	build := func(parts ...func(*multipart.Writer)) []byte {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		_ = w.SetBoundary("fuzzb")
		for _, p := range parts {
			p(w)
		}
		_ = w.Close()
		return b.Bytes()
	}
	data := func(s string) func(*multipart.Writer) {
		return func(w *multipart.Writer) { _ = w.WriteField("data", s) }
	}
	file := func(name string, body []byte) func(*multipart.Writer) {
		return func(w *multipart.Writer) {
			p, _ := w.CreateFormFile("file", name)
			_, _ = p.Write(body)
		}
	}
	f.Add(build(data(`{"a":1}`), file("cv.pdf", pdf(100))))
	f.Add(build(file("cv.pdf", pdf(100)), data(`{}`)))
	f.Add(build(data(`{}`), file("x.exe", []byte("MZ\x90")), file("y.pdf", pdf(10))))
	f.Add(build(data(strings.Repeat("a", 5000))))
	f.Add([]byte("--fuzzb\r\nContent-Disposition: form-data; name=\"data\"\r\n\r\n"))
	f.Add([]byte("garbage"))
	const maxData, maxFiles = 1024, 3
	f.Fuzz(func(t *testing.T, body []byte) {
		r := newRig()
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=fuzzb")
		form, err := ReadForm(bg, httptest.NewRecorder(), req, r.vault, maxData, maxFiles, nil)
		if err != nil {
			if n := r.store.Count(); n != 0 {
				t.Fatalf("a refused form left %d objects in the storage: %v", n, err)
			}
			return
		}
		if len(form.Files) > maxFiles || len(form.Data) > maxData {
			t.Fatalf("form over the limits: %d files, %d bytes of data", len(form.Files), len(form.Data))
		}
		if r.store.Count() != len(form.Files) {
			t.Fatalf("storage holds %d objects for %d files", r.store.Count(), len(form.Files))
		}
		for i, s := range form.Files {
			if s.Size <= 0 || s.Size > MaxFileSize {
				t.Fatalf("file %d: size %d", i, s.Size)
			}
			obj, err := r.store.Get(bg, s.Key)
			if err != nil {
				t.Fatalf("file %d: %v", i, err)
			}
			// Метка PDF допускается в первых 1024 байтах (так сказано в спецификации и так проверяет склад).
			head, _ := io.ReadAll(io.LimitReader(obj, 1024))
			_ = obj.Close()
			if !bytes.Contains(head, []byte("%PDF-")) {
				t.Fatalf("file %d: stored a non-PDF (%s)", i, fmt.Sprintf("%q", head[:min(len(head), 16)]))
			}
		}
	})
}
