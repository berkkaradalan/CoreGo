package auth

import (
	"net/mail"
	"strings"
)

// normalizeEmail trims surrounding whitespace and lowercases the
// address so the same mailbox always maps to the same stored user.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validateEmail reports whether email is a syntactically valid address.
// The empty string yields ErrEmailRequired. Anything that
// net/mail.ParseAddress rejects, or that parses only with a display
// name (for example "Alice <alice@example.com>"), yields
// ErrInvalidEmail.
func validateEmail(email string) error {
	if email == "" {
		return ErrEmailRequired
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return ErrInvalidEmail
	}
	return nil
}

// normalizeAndValidateEmail combines normalizeEmail and validateEmail
// and returns the normalized address.
func normalizeAndValidateEmail(email string) (string, error) {
	email = normalizeEmail(email)
	if err := validateEmail(email); err != nil {
		return "", err
	}
	return email, nil
}
