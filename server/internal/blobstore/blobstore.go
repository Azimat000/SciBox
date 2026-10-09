// Package blobstore — хранилище файлов: объекты по ключу в S3-совместимом сервисе (локально — контейнер из
// docker-compose.yml, в бою — Yandex Object Storage, Selectel, VK Cloud и т.п.). Критичная зона (docs/TESTING.md):
// здесь решается, что файл записан целиком или не записан совсем и что удалённого больше нет.
//
// Пакет ничего не знает о правах и отклике: ключ — случайный номер, имя файла и человек в нём не участвуют.
package blobstore

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound — объекта с таким ключом нет.
var ErrNotFound = errors.New("blobstore: object not found")

// Info — что хранилище знает об объекте.
type Info struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// Object — открытый объект. Размер известен до чтения, чтобы ответ можно было начать с Content-Length.
type Object interface {
	io.ReadCloser
	Size() int64
}

// Store — хранилище. Все методы безопасны для одновременного вызова.
type Store interface {
	// Put записывает объект из r потоком, не зная заранее длины, и возвращает записанное число байт. Объект виден
	// только после успешной записи целиком: если r вернул ошибку (в том числе «файл слишком большой»), хранилище
	// не оставляет ни куска, ни пустого объекта. Тот же ключ записывается заново поверх.
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	// Get открывает объект; нет такого — ErrNotFound.
	Get(ctx context.Context, key string) (Object, error)
	// Delete удаляет объекты; объект, которого нет, не ошибка (повтор безопасен).
	Delete(ctx context.Context, keys ...string) error
	// Walk перебирает объекты с этим началом ключа, пока fn не вернёт ошибку.
	Walk(ctx context.Context, prefix string, fn func(Info) error) error
	// Ping проверяет, что хранилище отвечает и бакет доступен.
	Ping(ctx context.Context) error
}
