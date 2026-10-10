package blobstore_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scibox/server/internal/blobstore"
)

// Настройки хранилища для тестов совпадают с docker-compose.yml (сервис s3); `make test` поднимает его сам.
func testConfig(bucket string) blobstore.S3Config {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	return blobstore.S3Config{
		Endpoint:     get("TEST_S3_ENDPOINT", "localhost:8333"),
		Bucket:       bucket,
		AccessKey:    get("TEST_S3_ACCESS_KEY", "scibox"),
		SecretKey:    get("TEST_S3_SECRET_KEY", "scibox-dev-secret"),
		Region:       "us-east-1",
		PathStyle:    true,
		CreateBucket: true,
	}
}

func newS3(t *testing.T) *blobstore.S3 {
	t.Helper()
	s, err := blobstore.NewS3(t.Context(), testConfig("t-"+strings.ToLower(rand.Text()[:12])))
	if err != nil {
		t.Fatalf("S3 из docker-compose недоступно (make db-up): %v", err)
	}
	t.Cleanup(func() {
		if err := s.Destroy(context.Background()); err != nil {
			t.Errorf("destroy test bucket: %v", err)
		}
	})
	return s
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func readAll(t *testing.T, s blobstore.Store, key string) []byte {
	t.Helper()
	o, err := s.Get(t.Context(), key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer func() { _ = o.Close() }()
	data, err := io.ReadAll(o)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if int64(len(data)) != o.Size() {
		t.Fatalf("size %d, read %d", o.Size(), len(data))
	}
	return data
}

type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// storeContract — то, чего остальной код ждёт от любого хранилища; одинаково проверяется настоящее и в памяти.
func storeContract(t *testing.T, s blobstore.Store) {
	t.Helper()
	ctx := t.Context()

	t.Run("small and multipart objects come back byte for byte", func(t *testing.T) {
		for _, n := range []int{1, 1 << 10, 5 << 20, 5<<20 + 1, 12 << 20} {
			want := randomBytes(t, n)
			key := fmt.Sprintf("files/size-%d", n)
			got, err := s.Put(ctx, key, bytes.NewReader(want))
			if err != nil || got != int64(n) {
				t.Fatalf("put %d bytes: wrote %d, err %v", n, got, err)
			}
			if !bytes.Equal(readAll(t, s, key), want) {
				t.Fatalf("%d bytes came back different", n)
			}
		}
	})

	t.Run("an empty object is allowed and has size zero", func(t *testing.T) {
		if _, err := s.Put(ctx, "files/empty", bytes.NewReader(nil)); err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, s, "files/empty"); len(got) != 0 {
			t.Fatalf("got %d bytes", len(got))
		}
	})

	t.Run("a failed upload leaves nothing behind", func(t *testing.T) {
		boom := errors.New("client went away")
		for _, n := range []int{100, 6 << 20} {
			key := fmt.Sprintf("files/broken-%d", n)
			_, err := s.Put(ctx, key, &failingReader{data: randomBytes(t, n), err: boom})
			if err == nil {
				t.Fatalf("%d bytes: upload with a broken reader succeeded", n)
			}
			if _, err := s.Get(ctx, key); !errors.Is(err, blobstore.ErrNotFound) {
				t.Fatalf("%d bytes: broken upload is visible: %v", n, err)
			}
		}
	})

	t.Run("a missing object is ErrNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "files/nope"); !errors.Is(err, blobstore.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("delete is repeatable and removes only what was named", func(t *testing.T) {
		for _, k := range []string{"del/a", "del/b", "del/c"} {
			if _, err := s.Put(ctx, k, strings.NewReader("x")); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Delete(ctx, "del/a", "del/b", "del/never-existed"); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, "del/a"); err != nil {
			t.Fatalf("second delete: %v", err)
		}
		if _, err := s.Get(ctx, "del/a"); !errors.Is(err, blobstore.ErrNotFound) {
			t.Fatalf("a still exists: %v", err)
		}
		if got := readAll(t, s, "del/c"); string(got) != "x" {
			t.Fatalf("c damaged: %q", got)
		}
		if err := s.Delete(ctx); err != nil {
			t.Fatalf("empty delete: %v", err)
		}
	})

	t.Run("walk lists by prefix with size and time", func(t *testing.T) {
		for _, k := range []string{"walk/1", "walk/2", "other/1"} {
			if _, err := s.Put(ctx, k, strings.NewReader("abc")); err != nil {
				t.Fatal(err)
			}
		}
		var got []blobstore.Info
		if err := s.Walk(ctx, "walk/", func(in blobstore.Info) error { got = append(got, in); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Key != "walk/1" || got[1].Key != "walk/2" {
			t.Fatalf("got %+v", got)
		}
		for _, in := range got {
			if in.Size != 3 || time.Since(in.ModTime) > time.Hour || in.ModTime.After(time.Now().Add(time.Hour)) {
				t.Fatalf("bad info %+v", in)
			}
		}
	})

	t.Run("walk stops at the first error from the callback", func(t *testing.T) {
		stop := errors.New("enough")
		calls := 0
		err := s.Walk(ctx, "walk/", func(blobstore.Info) error { calls++; return stop })
		if !errors.Is(err, stop) || calls != 1 {
			t.Fatalf("err %v, calls %d", err, calls)
		}
	})

	t.Run("ping answers", func(t *testing.T) {
		if err := s.Ping(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func TestS3Contract(t *testing.T) { storeContract(t, newS3(t)) }

func TestMemoryContract(t *testing.T) { storeContract(t, blobstore.NewMemory()) }

func TestNewS3Refuses(t *testing.T) {
	ctx := t.Context()
	base := testConfig("t-" + strings.ToLower(rand.Text()[:12]))
	t.Run("missing settings", func(t *testing.T) {
		for _, mod := range []func(*blobstore.S3Config){
			func(c *blobstore.S3Config) { c.Endpoint = "" },
			func(c *blobstore.S3Config) { c.Bucket = "" },
			func(c *blobstore.S3Config) { c.AccessKey = "" },
			func(c *blobstore.S3Config) { c.SecretKey = "" },
		} {
			cfg := base
			mod(&cfg)
			if _, err := blobstore.NewS3(ctx, cfg); err == nil {
				t.Errorf("accepted %+v", cfg)
			}
		}
	})
	t.Run("endpoint that cannot be parsed", func(t *testing.T) {
		cfg := base
		cfg.Endpoint = "bad host with spaces/and/path"
		if _, err := blobstore.NewS3(ctx, cfg); err == nil {
			t.Error("accepted a broken endpoint")
		}
	})
	t.Run("nobody listens", func(t *testing.T) {
		cfg := base
		cfg.Endpoint = "127.0.0.1:1"
		short, cancel := context.WithTimeout(ctx, time.Second) // библиотека повторяет попытки: ждать все не нужно
		defer cancel()
		if _, err := blobstore.NewS3(short, cfg); err == nil {
			t.Error("connected to a closed port")
		}
	})
	t.Run("wrong keys", func(t *testing.T) {
		cfg := base
		cfg.SecretKey = "not-the-secret"
		if _, err := blobstore.NewS3(ctx, cfg); err == nil {
			t.Error("wrong keys accepted")
		}
	})
	t.Run("no bucket and no permission to create it", func(t *testing.T) {
		cfg := base
		cfg.CreateBucket = false
		if _, err := blobstore.NewS3(ctx, cfg); err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("an existing bucket is used as it is", func(t *testing.T) {
		first, err := blobstore.NewS3(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = first.Destroy(context.Background()) })
		cfg := base
		cfg.CreateBucket = false
		if _, err := blobstore.NewS3(ctx, cfg); err != nil {
			t.Errorf("existing bucket refused: %v", err)
		}
	})
}

// Бакет пропал (настройка, чужая рука): операции говорят об этом ошибкой, и ни одну не принять за «нет файла».
// Локальное хранилище само создаёт бакет при записи, поэтому ответы S3 о пропавшем бакете берутся от подставного сервера.
func TestS3BucketGone(t *testing.T) {
	var gone atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gone.Load() {
			w.WriteHeader(http.StatusOK) // бакет на месте: HEAD /bucket
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message><BucketName>b</BucketName></Error>`)
	}))
	defer srv.Close()
	ctx := t.Context()
	s, err := blobstore.NewS3(ctx, blobstore.S3Config{
		Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "bucket-x", AccessKey: "k", SecretKey: "s", Region: "us-east-1", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gone.Store(true)
	if _, err := s.Put(ctx, "files/a", strings.NewReader("x")); err == nil || errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("put into a missing bucket: %v", err)
	}
	if err := s.Delete(ctx, "files/a"); err == nil {
		t.Error("delete in a missing bucket succeeded")
	}
	if err := s.Walk(ctx, "", func(blobstore.Info) error { return nil }); err == nil {
		t.Error("walk in a missing bucket succeeded")
	}
	if err := s.Ping(ctx); err == nil {
		t.Error("ping of a missing bucket succeeded")
	}
	if err := s.Destroy(ctx); err == nil {
		t.Error("destroy of a missing bucket succeeded")
	}
}

func TestS3Cancelled(t *testing.T) {
	s := newS3(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Put(ctx, "files/a", strings.NewReader("x")); err == nil {
		t.Error("put with a cancelled context succeeded")
	}
	if err := s.Ping(ctx); err == nil {
		t.Error("ping with a cancelled context succeeded")
	}
	if err := s.Delete(ctx, "files/a"); err == nil {
		t.Error("delete with a cancelled context succeeded")
	}
}

func TestMemoryFaultsAndHelpers(t *testing.T) {
	m := blobstore.NewMemory()
	ctx := t.Context()
	boom := errors.New("down")
	for _, op := range []string{"put", "get", "delete", "walk", "ping"} {
		m.Fault = func(o, _ string) error {
			if o == op {
				return boom
			}
			return nil
		}
		var err error
		switch op {
		case "put":
			_, err = m.Put(ctx, "k", strings.NewReader("x"))
		case "get":
			_, err = m.Get(ctx, "k")
		case "delete":
			err = m.Delete(ctx, "k")
		case "walk":
			err = m.Walk(ctx, "", func(blobstore.Info) error { return nil })
		case "ping":
			err = m.Ping(ctx)
		}
		if !errors.Is(err, boom) {
			t.Errorf("%s: %v", op, err)
		}
	}
	m.Fault = nil
	if _, err := m.Put(ctx, "k", &failingReader{err: boom}); !errors.Is(err, boom) {
		t.Errorf("failed source: %v", err)
	}
	if m.Count() != 0 {
		t.Error("a failed put stored something")
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m.Now = func() time.Time { return now }
	_, _ = m.Put(ctx, "b", strings.NewReader("12"))
	_, _ = m.Put(ctx, "a", strings.NewReader("1"))
	if got := m.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" || !m.Has("a") || m.Has("zzz") {
		t.Errorf("keys %v", got)
	}
	m.Corrupt("a")
	m.Corrupt("missing")
	o, _ := m.Get(ctx, "a")
	data, _ := io.ReadAll(o)
	if string(data) != "x1" {
		t.Errorf("corrupted object: %q", data)
	}
	var seen []blobstore.Info
	_ = m.Walk(ctx, "", func(in blobstore.Info) error { seen = append(seen, in); return nil })
	if len(seen) != 2 || !seen[0].ModTime.Equal(now) {
		t.Errorf("walk: %+v", seen)
	}
}

// scriptedS3 — подставной S3, который отвечает по сценарию: нужен там, где настоящее хранилище не воспроизводит сбой.
type scriptedS3 struct {
	bucketExists bool
	// existsAfterCreate — бакет появляется после попытки его создать (его создал другой процесс: сервер и воркер стартуют вместе).
	existsAfterCreate bool
	createAttempted   atomic.Bool
	headObject   int // код ответа на HEAD объекта
	deleteObject int // код ответа на DELETE объекта
	createBucket int // код ответа на PUT бакета
	listObjects  bool
}

func (c *scriptedS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	isObject := len(parts) > 1
	xmlErr := func(code int, name string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(code)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><Error><Code>%s</Code><Message>scripted</Message></Error>`, name)
	}
	switch {
	case r.Method == http.MethodHead && !isObject:
		if c.bucketExists || (c.existsAfterCreate && c.createAttempted.Load()) {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	case r.Method == http.MethodHead:
		w.WriteHeader(c.headObject)
	case r.Method == http.MethodPut && !isObject:
		c.createAttempted.Store(true)
		if c.createBucket == http.StatusOK {
			w.WriteHeader(http.StatusOK)
			return
		}
		xmlErr(c.createBucket, "AccessDenied")
	case r.Method == http.MethodDelete && isObject:
		if c.deleteObject == http.StatusNotFound {
			xmlErr(http.StatusNotFound, "NoSuchKey")
			return
		}
		if c.deleteObject >= 400 {
			xmlErr(c.deleteObject, "AccessDenied")
			return
		}
		w.WriteHeader(c.deleteObject)
	case r.Method == http.MethodGet && !isObject && c.listObjects:
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><ListBucketResult><Name>bucket-x</Name><IsTruncated>false</IsTruncated><Contents><Key>files/one</Key><Size>1</Size><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents></ListBucketResult>`)
	default:
		xmlErr(http.StatusInternalServerError, "InternalError")
	}
}

func scripted(t *testing.T, c *scriptedS3, create bool) (*blobstore.S3, error) {
	t.Helper()
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	return blobstore.NewS3(t.Context(), blobstore.S3Config{
		Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "bucket-x", AccessKey: "k", SecretKey: "s", Region: "us-east-1", PathStyle: true, CreateBucket: create,
	})
}

func TestS3ErrorMapping(t *testing.T) {
	ctx := t.Context()
	t.Run("the bucket cannot be created", func(t *testing.T) {
		if _, err := scripted(t, &scriptedS3{createBucket: http.StatusForbidden}, true); err == nil || !strings.Contains(err.Error(), "create bucket") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("the bucket was created by another process at the same moment", func(t *testing.T) {
		if _, err := scripted(t, &scriptedS3{createBucket: http.StatusConflict, existsAfterCreate: true}, true); err != nil {
			t.Errorf("err = %v: a bucket that exists after the attempt is not a failure", err)
		}
	})
	t.Run("the bucket is created", func(t *testing.T) {
		if _, err := scripted(t, &scriptedS3{createBucket: http.StatusOK}, true); err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("get: no such key", func(t *testing.T) {
		s, err := scripted(t, &scriptedS3{bucketExists: true, headObject: http.StatusNotFound}, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, "files/a"); !errors.Is(err, blobstore.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("get: the storage fails, which is not 'no such file'", func(t *testing.T) {
		s, err := scripted(t, &scriptedS3{bucketExists: true, headObject: http.StatusForbidden}, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, "files/a"); err == nil || errors.Is(err, blobstore.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("get: an impossible key is refused before any request", func(t *testing.T) {
		s, err := scripted(t, &scriptedS3{bucketExists: true}, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, ""); err == nil || errors.Is(err, blobstore.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("delete: a missing object is not an error, a failure is", func(t *testing.T) {
		s, err := scripted(t, &scriptedS3{bucketExists: true, deleteObject: http.StatusNotFound}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, "files/a", "files/b"); err != nil {
			t.Errorf("missing objects: %v", err)
		}
		s, err = scripted(t, &scriptedS3{bucketExists: true, deleteObject: http.StatusForbidden}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, "files/a"); err == nil {
			t.Error("a failed delete was swallowed")
		}
	})
	t.Run("destroy stops at a failed delete", func(t *testing.T) {
		s, err := scripted(t, &scriptedS3{bucketExists: true, deleteObject: http.StatusForbidden, listObjects: true}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Destroy(ctx); err == nil || !strings.Contains(err.Error(), "destroy") {
			t.Errorf("err = %v", err)
		}
	})
}
