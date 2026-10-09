package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/config"
	"scibox/server/internal/files"
	"scibox/server/internal/filework"
	"scibox/server/internal/notifications"
)

// realVault — склад на настоящем хранилище и настоящем ClamAV из docker-compose.yml; бакет свой на тест.
func realVault(t *testing.T) (*files.Vault, *blobstore.S3) {
	t.Helper()
	s3, err := blobstore.NewS3(t.Context(), blobstore.S3Config{
		Endpoint: config.DefaultS3Endpoint, Bucket: "t-whole-" + strings.ToLower(rand.Text()[:10]), AccessKey: config.DefaultS3AccessKey,
		SecretKey: config.DefaultS3SecretKey, Region: config.DefaultS3Region, PathStyle: true, CreateBucket: true,
	})
	if err != nil {
		t.Fatalf("хранилище из docker-compose недоступно (make db-up): %v", err)
	}
	t.Cleanup(func() { _ = s3.Destroy(context.Background()) })
	clamd := antivirus.NewClamd(config.DefaultClamAVAddr)
	if err := clamd.Ping(t.Context()); err != nil {
		t.Fatalf("ClamAV из docker-compose недоступен (make db-up): %v", err)
	}
	return files.NewVault(s3, clamd, func() time.Time { return time.Now().UTC() }), s3
}

func multipartBody(t *testing.T, data any, parts ...struct {
	name string
	body []byte
}) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(data)
	fw, _ := mw.CreateFormField("data")
	_, _ = fw.Write(raw)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, p.name))
		h.Set("Content-Type", "application/pdf")
		pw, _ := mw.CreatePart(h)
		_, _ = pw.Write(p.body)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// TestFilesThroughTheWholeServerOnTheRealStorageAndAntivirus — сквозной сценарий среза 23 (T-21) без подделок:
// демо-данные уходят в настоящее хранилище и проходят настоящий ClamAV, в базе нет содержимого файлов, отклик с PDF
// принимается, тестовый вирус EICAR отклоняется понятным сообщением, а удаление аккаунта убирает объекты из хранилища.
func TestFilesThroughTheWholeServerOnTheRealStorageAndAntivirus(t *testing.T) {
	vault, s3 := realVault(t)
	w := newPerimeterWorldOn(t, vault)
	h := w.handler(nil)
	ctx := t.Context()

	// 1. Демо-файлы: все в хранилище и проверены настоящим ClamAV, в базе содержимого нет.
	var total, clean, withKey, withContent int
	if err := w.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE scan_status = 'clean'), count(*) FILTER (WHERE storage_key IS NOT NULL), count(*) FILTER (WHERE legacy_data IS NOT NULL) FROM application_files`).Scan(&total, &clean, &withKey, &withContent); err != nil {
		t.Fatal(err)
	}
	if total == 0 || clean != total || withKey != total || withContent != 0 {
		t.Fatalf("files %d, clean %d, in the storage %d, with content in the database %d", total, clean, withKey, withContent)
	}
	objects := 0
	if err := s3.Walk(ctx, files.KeyPrefix, func(blobstore.Info) error { objects++; return nil }); err != nil || objects != total {
		t.Fatalf("objects in the storage: %d (%v), rows: %d", objects, err, total)
	}

	// 2. Скачивание: тело совпадает с контрольной суммой из базы.
	var email string
	var appID, fileID string
	var sum []byte
	if err := w.pool.QueryRow(ctx, `SELECT u.email, a.id::text, f.id::text, f.sha256 FROM application_files f
		JOIN applications a ON a.id = f.application_id JOIN users u ON u.id = a.user_id WHERE f.kind = 'attachment' LIMIT 1`).Scan(&email, &appID, &fileID, &sum); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, h, email, "198.51.100.40:5000", nil)
	rec := send(h, req{path: "/api/applications/" + appID + "/files/" + fileID, cookie: cookie, remote: "198.51.100.40:5000"})
	got := sha256.Sum256(rec.Body.Bytes())
	if rec.Code != http.StatusOK || !bytes.Equal(got[:], sum) || rec.Header().Get("Content-Disposition") == "" {
		t.Fatalf("download: %d, checksum matches: %v", rec.Code, bytes.Equal(got[:], sum))
	}

	// 3. Отклик с PDF проходит, отклик с EICAR отклоняется.
	var applicant, vacancy string
	if err := w.pool.QueryRow(ctx, `
		SELECT u.email, v.id::text FROM users u JOIN profiles p ON p.user_id = u.id
		CROSS JOIN LATERAL (SELECT id FROM vacancies WHERE status = 'published' AND (deadline IS NULL OR deadline >= current_date) ORDER BY id LIMIT 1) v
		WHERE p.headline <> '' AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM org_members m WHERE m.user_id = u.id) ORDER BY u.email LIMIT 1`).Scan(&applicant, &vacancy); err != nil {
		t.Fatalf("no demo person without applications: %v", err)
	}
	me := login(t, h, applicant, "198.51.100.41:5000", nil)
	in := map[string]any{"vacancy_id": vacancy, "contact_email": applicant, "cover_letter": "Здравствуйте! Меня заинтересовала эта вакансия, мой опыт подходит.", "referees": []any{}}
	pdf := append([]byte("%PDF-1.7\n% real file\n"), bytes.Repeat([]byte("p"), 500)...)
	eicar := append(append([]byte("%PDF-1.7\n"), antivirus.EICAR()...), '\n')

	body, ct := multipartBody(t, in, struct {
		name string
		body []byte
	}{"счёт.pdf", eicar})
	rec = send(h, req{method: http.MethodPost, path: "/api/applications", body: body, length: int64(body.Len()), contentType: ct, cookie: me, remote: "198.51.100.41:5000"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "вирус") || !strings.Contains(rec.Body.String(), "счёт.pdf") {
		t.Fatalf("EICAR was not refused with a clear message: %d %s", rec.Code, rec.Body)
	}
	var count int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM applications a JOIN users u ON u.id = a.user_id WHERE u.email = $1`, applicant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("an application with a virus was created: %d %v", count, err)
	}
	after := 0
	if err := s3.Walk(ctx, files.KeyPrefix, func(blobstore.Info) error { after++; return nil }); err != nil || after != objects {
		t.Fatalf("the refused request left objects in the storage: %d, was %d (%v)", after, objects, err)
	}

	body, ct = multipartBody(t, in, struct {
		name string
		body []byte
	}{"Список публикаций.pdf", pdf})
	rec = send(h, req{method: http.MethodPost, path: "/api/applications", body: body, length: int64(body.Len()), contentType: ct, cookie: me, remote: "198.51.100.41:5000"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply with a PDF: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Application struct {
			ID    string `json:"id"`
			Files []struct {
				ID   string `json:"id"`
				Scan string `json:"scan"`
			} `json:"files"`
		} `json:"application"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || len(created.Application.Files) != 1 || created.Application.Files[0].Scan != "clean" {
		t.Fatalf("answer: %v %s", err, rec.Body)
	}
	rec = send(h, req{path: "/api/applications/" + created.Application.ID + "/files/" + created.Application.Files[0].ID, cookie: me, remote: "198.51.100.41:5000"})
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pdf) {
		t.Fatalf("the uploaded file came back different: %d", rec.Code)
	}

	// 4. Удаление аккаунта убирает объекты: фоновая задача удаляет их из хранилища по очереди.
	var user string
	if err := w.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email = $1`, applicant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	var keys []string
	rows, err := w.pool.Query(ctx, `SELECT f.storage_key FROM application_files f JOIN applications a ON a.id = f.application_id WHERE a.user_id = $1::uuid`, user)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if len(keys) != 2 { // резюме и приложение
		t.Fatalf("keys of the new application: %v", keys)
	}
	if err := eraseInTx(ctx, w.pool, uuid.MustParse(user)); err != nil {
		t.Fatal(err)
	}
	notes := notifications.NewService(w.pool, notifications.Config{ProductName: "ИОН", PublicURL: "http://localhost:5173"})
	rep, err := filework.NewService(w.pool, vault, notes, filework.DefaultConfig(), discardLogger()).Pass(ctx)
	if err != nil || rep.Deleted < 2 {
		t.Fatalf("file work: %+v %v", rep, err)
	}
	for _, k := range keys {
		if _, err := s3.Get(ctx, k); err == nil {
			t.Errorf("object %s survived the erasure of its owner", k)
		}
	}
}
