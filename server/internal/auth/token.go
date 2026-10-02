package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newToken возвращает случайный токен (его отдаём человеку) и его хеш (его кладём в базу).
// 32 случайных байта: угадать нельзя, поэтому быстрого хеша SHA-256 достаточно.
// Начиная с Go 1.24 rand.Read не возвращает ошибку: при сбое источника случайности программа аварийно завершается.
func newToken() (raw string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
