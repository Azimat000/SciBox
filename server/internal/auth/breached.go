package auth

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"strings"
)

// Частые пароли из утечек (T-14, D-146): откуда список и как получен — data/README.md.
//
//go:embed data/common-passwords.txt.gz
var commonPasswordsGz []byte

// extraCommonPasswords — свои дополнения к списку: русские и связанные с самим сервисом.
var extraCommonPasswords = []string{
	"1234567890", "0123456789", "12345678910", "1234567891", "0987654321", "9876543210",
	"password12", "password123", "password1234", "passw0rd123", "qwertyuiop", "qwerty1234", "qwerty12345",
	"qwertyuiop1", "asdfghjkl1", "1q2w3e4r5t", "1qaz2wsx3edc", "iloveyou12",
	"йцукенгшщз", "йцукен1234", "пароль1234", "пароль12345", "пароль123456", "парольпароль",
	"123456789a", "abcdefghij", "abc1234567", "administrator", "letmein123", "welcome123",
	"scibox1234", "scibox12345", "sciboxscibox",
}

// commonPasswords — множество частых паролей в нижнем регистре. Читается при запуске программы:
// испорченный встроенный файл остановит сервер сразу (и тест TestCommonPasswordList), а не при первой регистрации.
var commonPasswords = mustLoadCommonPasswords(commonPasswordsGz)

func mustLoadCommonPasswords(gz []byte) map[string]struct{} {
	set, err := loadCommonPasswords(gz)
	if err != nil {
		panic(fmt.Sprintf("auth: embedded common password list: %v", err))
	}
	return set
}

func loadCommonPasswords(gz []byte) (map[string]struct{}, error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	set := make(map[string]struct{}, 9000+len(extraCommonPasswords))
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			set[line] = struct{}{}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, p := range extraCommonPasswords {
		set[p] = struct{}{}
	}
	return set, nil
}

// ruToLatin — та же клавиша в английской раскладке: «йцукен» набрано вместо «qwerty».
var ruToLatin = strings.NewReplacer(
	"й", "q", "ц", "w", "у", "e", "к", "r", "е", "t", "н", "y", "г", "u", "ш", "i", "щ", "o", "з", "p", "х", "[", "ъ", "]",
	"ф", "a", "ы", "s", "в", "d", "а", "f", "п", "g", "р", "h", "о", "j", "л", "k", "д", "l", "ж", ";", "э", "'",
	"я", "z", "ч", "x", "с", "c", "м", "v", "и", "b", "т", "n", "ь", "m", "б", ",", "ю", ".", "ё", "`",
)

// isCommonPassword — пароль (в нижнем регистре) есть в списке частых сам или набранный в русской раскладке.
func isCommonPassword(lower string) bool {
	if _, ok := commonPasswords[lower]; ok {
		return true
	}
	if latin := ruToLatin.Replace(lower); latin != lower {
		_, ok := commonPasswords[latin]
		return ok
	}
	return false
}
