package auth

import (
	"encoding/base64"
	"testing"
)

func TestNewTokenFormat(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if len(token) != 43 { // 32 bytes in base64.RawURLEncoding
		t.Errorf("len(token) = %d, want 43", len(token))
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not valid raw URL base64: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("decoded %d bytes, want 32", len(raw))
	}
}

func TestNewTokenUniqueness(t *testing.T) {
	const count = 1000
	seen := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		token, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken iteration %d: %v", i, err)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatalf("NewToken produced a duplicate token after %d draws", i)
		}
		seen[token] = struct{}{}
	}
}

func TestHashTokenKnownVectors(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}
	for _, tt := range tests {
		if got := HashToken(tt.input); got != tt.want {
			t.Errorf("HashToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHashTokenProperties(t *testing.T) {
	first := HashToken("token-a")
	second := HashToken("token-a")
	third := HashToken("token-b")

	if first != second {
		t.Error("HashToken is not deterministic")
	}
	if first == third {
		t.Error("HashToken collided for different inputs")
	}
	if len(first) != 64 {
		t.Errorf("len(hash) = %d, want 64 hex characters", len(first))
	}
}
