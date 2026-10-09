package antivirus

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
)

// EICAR — стандартная тестовая «зараза» (https://www.eicar.org/): безвредный текст, который любой антивирус обязан
// считать вирусом. Строка собрана из частей, чтобы антивирус на компьютере не удалял исходники.
func EICAR() []byte {
	return []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$` + "EICAR" + `-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
}

// Fake — сканер для тестов: находит EICAR в потоке, остальное считает чистым. Fault позволяет изобразить сбой.
type Fake struct {
	// Fault, если задана, вызывается перед проверкой; непустая ошибка возвращается как есть.
	Fault func() error
	scans atomic.Int64
}

// Scan читает поток целиком и ищет EICAR.
func (f *Fake) Scan(_ context.Context, r io.Reader) (Verdict, error) {
	f.scans.Add(1)
	if f.Fault != nil {
		if err := f.Fault(); err != nil {
			return Verdict{}, err
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Verdict{}, readErr{err}
	}
	if bytes.Contains(data, EICAR()) {
		return Verdict{Infected: true, Signature: "Eicar-Test-Signature"}, nil
	}
	return Verdict{}, nil
}

// Ping отвечает, если нет сбоя.
func (f *Fake) Ping(context.Context) error {
	if f.Fault != nil {
		return f.Fault()
	}
	return nil
}

// Scans — сколько проверок сделано (для тестов).
func (f *Fake) Scans() int { return int(f.scans.Load()) }

// Skip — «сканер», который ничего не проверяет и считает все файлы чистыми. Только для разработки без ClamAV
// (SCIBOX_CLAMAV_ADDR=off); боевой режим такую настройку не принимает (config.checkProduction).
type Skip struct{}

// Scan читает поток и объявляет его чистым.
func (Skip) Scan(_ context.Context, r io.Reader) (Verdict, error) {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return Verdict{}, readErr{err}
	}
	return Verdict{}, nil
}

// Ping всегда отвечает.
func (Skip) Ping(context.Context) error { return nil }
