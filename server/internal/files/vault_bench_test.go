package files

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
)

// BenchmarkVaultPut — приём одного файла потоком на настоящем хранилище и настоящем ClamAV из docker-compose.yml
// (замеры записаны в docs/SCALING.md). Без контейнеров бенчмарк пропускается.
func BenchmarkVaultPut(b *testing.B) {
	s3, err := blobstore.NewS3(b.Context(), blobstore.S3Config{
		Endpoint: "localhost:8333", Bucket: "t-bench-" + strings.ToLower(rand.Text()[:8]), AccessKey: "scibox", SecretKey: "scibox-dev-secret",
		Region: "us-east-1", PathStyle: true, CreateBucket: true,
	})
	if err != nil {
		b.Skipf("хранилище из docker-compose недоступно: %v", err)
	}
	b.Cleanup(func() { _ = s3.Destroy(b.Context()) })
	clamd := antivirus.NewClamd("localhost:3310")
	if err := clamd.Ping(b.Context()); err != nil {
		b.Skipf("ClamAV из docker-compose недоступен: %v", err)
	}
	vault := NewVault(s3, clamd, time.Now)
	for _, size := range []struct {
		name string
		n    int
	}{{"100KB", 100 << 10}, {"1MB", 1 << 20}, {"10MB", MaxFileSize}} {
		data := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("0123456789abcdef"), (size.n-16)/16)...)
		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				st, err := vault.Put(b.Context(), "a.pdf", bytes.NewReader(data))
				if err != nil || st.Scan != ScanClean {
					b.Fatalf("%+v %v", st, err)
				}
				b.StopTimer()
				vault.Discard(b.Context(), st)
				b.StartTimer()
			}
		})
	}
}
