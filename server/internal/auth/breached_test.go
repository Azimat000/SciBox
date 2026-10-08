package auth

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// Встроенный список читается целиком и содержит пароли, которые сервер раньше пропускал (T-14).
func TestCommonPasswordList(t *testing.T) {
	if n := len(commonPasswords); n < 8000 {
		t.Fatalf("only %d common passwords loaded", n)
	}
	for p := range commonPasswords {
		if p != strings.ToLower(p) || passwordLen(p) < MinPasswordLen && !isExtra(p) {
			t.Fatalf("list entry %q must be lower case and at least %d characters", p, MinPasswordLen)
		}
	}
	// Из списка NCSC: длинные, разные знаки, раньше проходили проверку.
	for _, p := range []string{"iloveyou123", "basketball", "1qaz2wsx3edc4rfv", "ятебялюблю", "1й2ц3у4к5е"} {
		if !isCommonPassword(p) {
			t.Errorf("%q must be rejected as common", p)
		}
	}
	if isCommonPassword("violet ridge seventy") {
		t.Error("an uncommon passphrase must pass")
	}
}

func isExtra(p string) bool {
	for _, x := range extraCommonPasswords {
		if x == p {
			return true
		}
	}
	return false
}

// Пароль, набранный в русской раскладке вместо английской, — тот же частый пароль.
func TestCommonPasswordInRussianLayout(t *testing.T) {
	cases := []struct {
		typed string
		want  bool
	}{
		{"ифылуеифдд", true},  // basketball
		{"йцукенгшщз", true},  // qwertyuiop
		{"зфыыцщкв123", true}, // password123
		{"фиолетовый закат", false},
	}
	for _, tc := range cases {
		if got := isCommonPassword(tc.typed); got != tc.want {
			t.Errorf("isCommonPassword(%q) = %v, want %v", tc.typed, got, tc.want)
		}
	}
	if msg := ValidatePassword("ИФЫЛУЕИФДД", "ivan@example.ru"); msg != msgPasswordWeak {
		t.Fatalf("upper case in Russian layout: %q", msg)
	}
}

func TestLoadCommonPasswordsRejectsBrokenData(t *testing.T) {
	if _, err := loadCommonPasswords([]byte("not gzip")); err == nil {
		t.Fatal("broken data must be an error")
	}
	// Начало верное, конец обрезан: ошибка при чтении, а не тихо неполный список.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(strings.Repeat("password12345\n", 2000)))
	_ = zw.Close()
	if _, err := loadCommonPasswords(buf.Bytes()[:buf.Len()-12]); err == nil {
		t.Fatal("truncated data must be an error")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("broken embedded list must stop the program at start")
		}
	}()
	mustLoadCommonPasswords([]byte("not gzip"))
}
