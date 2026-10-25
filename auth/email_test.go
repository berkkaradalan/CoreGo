package auth

import (
	"errors"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user@example.com", "user@example.com"},
		{"  User@Example.COM  ", "user@example.com"},
		{"MIXED@Case.IO", "mixed@case.io"},
		{"", ""},
		{"\tspaces@example.com\n", "spaces@example.com"},
	}
	for _, tt := range tests {
		if got := normalizeEmail(tt.input); got != tt.want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		wantError error
	}{
		{"empty", "", ErrEmailRequired},
		{"plain address", "user@example.com", nil},
		{"uppercase address", "USER@EXAMPLE.COM", nil},
		{"local domain", "a@b", nil},
		{"no at sign", "user.example.com", ErrInvalidEmail},
		{"double at", "user@@example.com", ErrInvalidEmail},
		{"missing local part", "@example.com", ErrInvalidEmail},
		{"missing domain", "user@", ErrInvalidEmail},
		{"display name form", "Alice <alice@example.com>", ErrInvalidEmail},
		{"spaces inside", "user name@example.com", ErrInvalidEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail(tt.email)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("validateEmail(%q) error = %v, want %v", tt.email, err, tt.wantError)
			}
		})
	}
}

func TestNormalizeAndValidateEmail(t *testing.T) {
	got, err := normalizeAndValidateEmail("  Alice@Example.COM ")
	if err != nil {
		t.Fatalf("normalizeAndValidateEmail: %v", err)
	}
	if got != "alice@example.com" {
		t.Errorf("got %q, want %q", got, "alice@example.com")
	}

	if _, err := normalizeAndValidateEmail("not-an-email"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("error = %v, want ErrInvalidEmail", err)
	}
	if _, err := normalizeAndValidateEmail("   "); !errors.Is(err, ErrEmailRequired) {
		t.Errorf("error = %v, want ErrEmailRequired", err)
	}
}
