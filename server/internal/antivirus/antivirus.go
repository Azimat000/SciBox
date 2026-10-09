// Package antivirus — проверка файлов на вирусы через ClamAV (clamd по TCP, команда INSTREAM). Критичная зона
// (docs/TESTING.md): ошибка здесь пропускает заражённый файл организации или теряет честный.
//
// Правило пакета: «не знаем» — не значит «чисто». Любой сбой (нет связи, обрыв, непонятный ответ, срок) возвращается
// как ErrUnavailable, и вызывающий не считает файл проверенным.
package antivirus

import (
	"context"
	"errors"
	"io"
)

// ErrUnavailable — проверить не удалось (clamd не отвечает, оборвал связь, ответил непонятно или не уложился в срок).
// Это не вердикт: файл надо проверить позже.
var ErrUnavailable = errors.New("antivirus: scanner unavailable")

// Verdict — итог проверки. Infected == false значит, что сканер прочитал файл целиком и ничего не нашёл.
type Verdict struct {
	Infected bool
	// Signature — название найденной сигнатуры («Win.Test.EICAR_HDB-1»); пусто, если файл чист.
	Signature string
}

// Scanner проверяет поток байт.
type Scanner interface {
	// Scan читает r до конца и проверяет. Ошибка чтения r возвращается как есть (это не сбой сканера);
	// сбой самого сканера — ErrUnavailable.
	Scan(ctx context.Context, r io.Reader) (Verdict, error)
	// Ping проверяет, что сканер жив и готов (clamd отвечает PONG только после загрузки баз сигнатур).
	Ping(ctx context.Context) error
}
