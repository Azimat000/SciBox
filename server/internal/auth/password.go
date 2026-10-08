// Package auth — аккаунты: регистрация, подтверждение почты, вход, сессии, сброс пароля.
// Критичная зона (D-033): покрытие тестами не ниже 97%.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"scibox/server/internal/num"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// HashParams — стоимость хеширования паролей argon2id.
type HashParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// DefaultHashParams — боевые параметры: 64 МиБ памяти, 3 прохода (порядка 100 мс на компьютере).
var DefaultHashParams = HashParams{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLen: 16, KeyLen: 32}

// TestHashParams — дешёвые параметры для тестов, чтобы они не тормозили.
var TestHashParams = HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}

// Hasher хеширует и проверяет пароли. Одновременно считается не больше maxParallel хешей:
// каждый занимает десятки мегабайт, и поток запросов входа иначе съел бы всю память.
type Hasher struct {
	params HashParams
	sem    chan struct{}
	// dummy — настоящий хеш случайного пароля, чтобы вход с несуществующей почтой
	// занимал столько же времени, сколько с настоящей.
	dummy string
}

// NewHasher создаёт хешер с ограничением числа одновременных вычислений.
func NewHasher(p HashParams, maxParallel int) *Hasher {
	if maxParallel < 1 {
		maxParallel = 1
	}
	h := &Hasher{params: p, sem: make(chan struct{}, maxParallel)}
	h.dummy = h.compute("dummy-password")
	return h
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash возвращает хеш в формате PHC: $argon2id$v=19$m=…,t=…,p=…$соль$хеш.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return h.compute(password), nil
}

func (h *Hasher) compute(password string) string {
	salt := make([]byte, h.params.SaltLen)
	// Начиная с Go 1.24 rand.Read не возвращает ошибку: при сбое источника случайности программа аварийно завершается.
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// ErrBadHash — строка не похожа на хеш argon2id, который мы сами создавали.
var ErrBadHash = errors.New("auth: malformed password hash")

// Verify проверяет пароль по хешу. Время сравнения не зависит от того, где пароль не совпал.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	p, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, num.Uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parseHash(encoded string) (HashParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return HashParams{}, nil, nil, ErrBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return HashParams{}, nil, nil, ErrBadHash
	}
	var p HashParams
	var par uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &par); err != nil {
		return HashParams{}, nil, nil, ErrBadHash
	}
	// Параметры пришли из нашей же базы, но память ограничиваем на случай порчи данных.
	if par == 0 || par > 255 || p.Iterations == 0 || p.MemoryKiB == 0 || p.MemoryKiB > 1<<20 {
		return HashParams{}, nil, nil, ErrBadHash
	}
	p.Parallelism = uint8(par)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return HashParams{}, nil, nil, ErrBadHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return HashParams{}, nil, nil, ErrBadHash
	}
	return p, salt, key, nil
}

// spendTime тратит столько же времени, сколько проверка настоящего пароля.
func (h *Hasher) spendTime(ctx context.Context) {
	_, _ = h.Verify(ctx, "another-password", h.dummy)
}

// passwordLen считает длину в символах, а не в байтах: кириллица занимает по два байта.
func passwordLen(s string) int { return utf8.RuneCountInString(s) }
