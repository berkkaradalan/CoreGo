package auth

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == testPassword {
		t.Fatal("HashPassword returned the plaintext")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash %q does not look like bcrypt", hash)
	}
	if !VerifyPassword(hash, testPassword) {
		t.Error("VerifyPassword rejected the correct password")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Error("VerifyPassword accepted a wrong password")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	first, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password are identical; salt not applied")
	}
}

func TestHashPasswordTooLong(t *testing.T) {
	_, err := HashPassword(strings.Repeat("x", 73))
	if err == nil {
		t.Fatal("HashPassword accepted a 73 byte password (bcrypt limit is 72)")
	}
}

func TestVerifyPasswordMalformedHash(t *testing.T) {
	tests := []string{"", "not-a-hash", "abc", "$2a$10$short"}
	for _, hash := range tests {
		if VerifyPassword(hash, testPassword) {
			t.Errorf("VerifyPassword(%q, ...) = true, want false", hash)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	tests := []struct {
		name      string
		password  string
		wantError error
	}{
		{"empty", "", ErrPasswordRequired},
		{"one char too short", "1234567", ErrWeakPassword},
		{"exactly min length", "12345678", nil},
		{"longer than min", testPassword, nil},
		{"exactly bcrypt limit", strings.Repeat("x", 72), nil},
		{"over bcrypt limit", strings.Repeat("x", 73), ErrWeakPassword},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.validatePassword(tt.password)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("validatePassword(%d bytes) error = %v, want %v", len(tt.password), err, tt.wantError)
			}
		})
	}
}

func TestValidatePasswordCustomMinLength(t *testing.T) {
	a, _ := newTestAuth(t, Config{MinPasswordLength: 12})
	if err := a.validatePassword("a-11-chars"); err == nil {
		t.Error("11 char password accepted with MinPasswordLength 12")
	}
	if err := a.validatePassword("a-12-chars!!"); err != nil {
		t.Errorf("12 char password rejected: %v", err)
	}
}

func TestDummyHash(t *testing.T) {
	hash := dummyHash()
	if hash == "" {
		t.Fatal("dummyHash is empty")
	}
	if !VerifyPassword(hash, dummyPassword) {
		t.Error("dummyHash does not verify against dummyPassword")
	}
	if VerifyPassword(hash, "wrong") {
		t.Error("dummyHash verifies against the wrong password")
	}
	if again := dummyHash(); again != hash {
		t.Error("dummyHash is recomputed instead of memoized")
	}
}

// TestDummyHashIsLazy guards against computing the bcrypt hash at
// package initialization: nothing is computed until the first call,
// and concurrent first calls agree on one value.
func TestDummyHashIsLazy(t *testing.T) {
	dummyHashOnce = sync.Once{}
	dummyHashValue = ""
	if dummyHashValue != "" {
		t.Fatal("dummy hash computed before first use")
	}

	const callers = 8
	results := make([]string, callers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = dummyHash()
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if got == "" || got != results[0] {
			t.Fatalf("caller %d got %q, want the single memoized hash %q", i, got, results[0])
		}
	}
}

func TestDummyCompareExecutesBcrypt(t *testing.T) {
	// dummyCompare must run without panicking and swallow the result.
	dummyCompare(testPassword)
	dummyCompare("")
}
