package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h := NewHasher(TestHashParams, 2)
	ctx := context.Background()
	enc, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("unexpected PHC prefix: %q", enc)
	}
	other, _ := h.Hash(ctx, "correct horse battery")
	if enc == other {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
	for _, tc := range []struct {
		name, password string
		want           bool
	}{
		{"right password", "correct horse battery", true},
		{"wrong password", "correct horse batterY", false},
		{"empty password", "", false},
		{"cyrillic wrong", "правильный пароль", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.Verify(ctx, tc.password, enc)
			if err != nil || got != tc.want {
				t.Fatalf("Verify = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	cyr, _ := h.Hash(ctx, "правильный пароль")
	if ok, err := h.Verify(ctx, "правильный пароль", cyr); err != nil || !ok {
		t.Fatalf("cyrillic roundtrip: %v %v", ok, err)
	}
}

func TestVerifyUsesParamsFromHash(t *testing.T) {
	cheap := NewHasher(TestHashParams, 1)
	enc, _ := cheap.Hash(context.Background(), "correct horse battery")
	// Другой хешер с другими параметрами всё равно проверяет по параметрам из самого хеша.
	other := NewHasher(HashParams{MemoryKiB: 128, Iterations: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}, 1)
	if ok, err := other.Verify(context.Background(), "correct horse battery", enc); err != nil || !ok {
		t.Fatalf("Verify = %v, %v", ok, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	h := NewHasher(TestHashParams, 1)
	good, _ := h.Hash(context.Background(), "x")
	parts := strings.Split(good, "$") // "", argon2id, v=19, params, salt, key
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	cases := map[string]string{
		"empty":            "",
		"plain text":       "password",
		"too few parts":    "$argon2id$v=19$m=64,t=1,p=1$salt",
		"wrong algorithm":  with(1, "argon2i"),
		"bad version":      with(2, "v=x"),
		"old version":      with(2, "v=16"),
		"bad params":       with(3, "m=a,t=b,p=c"),
		"zero parallelism": with(3, "m=64,t=1,p=0"),
		"huge parallelism": with(3, "m=64,t=1,p=300"),
		"zero iterations":  with(3, "m=64,t=0,p=1"),
		"zero memory":      with(3, "m=0,t=1,p=1"),
		"huge memory":      with(3, "m=2000000,t=1,p=1"),
		"bad salt":         with(4, "!!!"),
		"empty salt":       with(4, ""),
		"bad key":          with(5, "!!!"),
		"empty key":        with(5, ""),
	}
	for name, enc := range cases {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify(context.Background(), "x", enc)
			if ok || !errors.Is(err, ErrBadHash) {
				t.Fatalf("Verify = %v, %v; want ErrBadHash", ok, err)
			}
		})
	}
}

func TestHasherLimitsParallelismAndHonoursContext(t *testing.T) {
	h := NewHasher(TestHashParams, 0) // 0 превращается в 1
	if cap(h.sem) != 1 {
		t.Fatalf("sem capacity = %d, want 1", cap(h.sem))
	}
	h.sem <- struct{}{} // место занято
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hash err = %v", err)
	}
	enc := h.dummy
	if _, err := h.Verify(ctx, "x", enc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify err = %v", err)
	}
	h.spendTime(ctx) // не должен паниковать и зависать
	<-h.sem
	if _, err := h.Hash(context.Background(), "x"); err != nil {
		t.Fatalf("Hash after release: %v", err)
	}
}

func TestDummyHashIsARealHash(t *testing.T) {
	h := NewHasher(TestHashParams, 1)
	if ok, err := h.Verify(context.Background(), "dummy-password", h.dummy); err != nil || !ok {
		t.Fatalf("dummy must be a valid hash, so that a missing account costs as much time as a real one: %v %v", ok, err)
	}
	if ok, _ := h.Verify(context.Background(), "another-password", h.dummy); ok {
		t.Fatal("spendTime password must not match the dummy")
	}
}
