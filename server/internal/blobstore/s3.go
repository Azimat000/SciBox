package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// partSize — размер части при записи потоком без известной длины. Меньше S3 не принимает (5 МиБ); столько же
// памяти занимает одна запись, независимо от размера файла.
const partSize = 5 << 20

// S3Config — подключение к S3-совместимому хранилищу.
type S3Config struct {
	// Endpoint — адрес без схемы: «localhost:8333», «storage.yandexcloud.net».
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	// Region — «ru-central1» у Yandex, «ru-1» у Selectel; у локального хранилища подойдёт любой.
	Region string
	// TLS — ходить по https. В боевом режиме обязателен (config.checkProduction).
	TLS bool
	// PathStyle — адрес вида endpoint/bucket/key вместо bucket.endpoint/key. Локальному хранилищу нужен он.
	PathStyle bool
	// CreateBucket — создать бакет, если его нет. Только для разработки: в бою бакет создаёт администратор с нужными правами.
	CreateBucket bool
}

// S3 — хранилище в S3-совместимом сервисе.
type S3 struct {
	c      *minio.Client
	bucket string
}

// NewS3 подключается и проверяет бакет (создаёт, если разрешено).
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("blobstore: endpoint, bucket, access key and secret key are required")
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.TLS,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("blobstore: connect: %w", err)
	}
	s := &S3{c: c, bucket: cfg.Bucket}
	ok, err := c.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("blobstore: check bucket: %w", err)
	}
	if !ok {
		if !cfg.CreateBucket {
			return nil, fmt.Errorf("blobstore: bucket %q does not exist", cfg.Bucket)
		}
		if err := c.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("blobstore: create bucket: %w", err)
		}
	}
	return s, nil
}

// Put записывает объект потоком. Если r вернул ошибку, библиотека отменяет многочастную запись и объект не появляется.
func (s *S3) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	info, err := s.c.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{
		PartSize:    partSize,
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return 0, fmt.Errorf("blobstore: put: %w", err)
	}
	return info.Size, nil
}

type s3Object struct {
	*minio.Object
	size int64
}

func (o s3Object) Size() int64 { return o.size }

// Get открывает объект и сразу узнаёт его размер: «нет такого» выясняется здесь, а не при чтении.
func (s *S3) Get(ctx context.Context, key string) (Object, error) {
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, mapErr("get", err)
	}
	st, err := obj.Stat()
	if err != nil {
		_ = obj.Close() // закрытие ничего не пишет; важна ошибка Stat
		return nil, mapErr("get", err)
	}
	return s3Object{Object: obj, size: st.Size}, nil
}

// Delete удаляет объекты по одному; отсутствующий объект S3 удаляет без ошибки.
func (s *S3) Delete(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		if err := s.c.RemoveObject(ctx, s.bucket, k, minio.RemoveObjectOptions{}); err != nil {
			if isNotFound(err) {
				continue
			}
			return fmt.Errorf("blobstore: delete: %w", err)
		}
	}
	return nil
}

// Walk перебирает объекты с этим началом ключа.
func (s *S3) Walk(ctx context.Context, prefix string, fn func(Info) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // останавливает перечисление, если fn вернул ошибку раньше конца
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return fmt.Errorf("blobstore: list: %w", o.Err)
		}
		if err := fn(Info{Key: o.Key, Size: o.Size, ModTime: o.LastModified}); err != nil {
			return err
		}
	}
	return nil
}

// Ping проверяет, что бакет доступен.
func (s *S3) Ping(ctx context.Context) error {
	ok, err := s.c.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("blobstore: ping: %w", err)
	}
	if !ok {
		return fmt.Errorf("blobstore: bucket %q is gone", s.bucket)
	}
	return nil
}

// isNotFound — «такого объекта нет». Пропавший бакет (NoSuchBucket) сюда не входит: это поломка настройки, а не
// отсутствие файла, и выдавать её за «файла нет» нельзя.
func isNotFound(err error) bool {
	return strings.EqualFold(minio.ToErrorResponse(err).Code, "NoSuchKey")
}

func mapErr(op string, err error) error {
	if isNotFound(err) {
		return ErrNotFound
	}
	return fmt.Errorf("blobstore: %s: %w", op, err)
}

// Destroy удаляет все объекты и сам бакет. Нужен тестам, чтобы не копить временные бакеты в локальном хранилище;
// в работе сервера не вызывается.
func (s *S3) Destroy(ctx context.Context) error {
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if o.Err != nil {
			return fmt.Errorf("blobstore: list for destroy: %w", o.Err)
		}
		if err := s.c.RemoveObject(ctx, s.bucket, o.Key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("blobstore: destroy: %w", err)
		}
	}
	if err := s.c.RemoveBucket(ctx, s.bucket); err != nil {
		return fmt.Errorf("blobstore: remove bucket: %w", err)
	}
	return nil
}
