package references

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newToken возвращает случайный токен (его отдаём рекомендателю) и его хеш (его кладём в базу).
// 32 случайных байта: угадать нельзя, поэтому быстрого хеша SHA-256 достаточно. При утечке таблицы войти по ссылке нельзя.
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
