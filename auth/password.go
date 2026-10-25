package auth

import (
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// maxPasswordLength is the bcrypt input limit: passwords longer than 72
// bytes cannot be hashed and are rejected by the password policy with
// ErrWeakPassword.
const maxPasswordLength = 72

// dummyPassword is the plaintext behind the dummy hash.
const dummyPassword = "corego-dummy-password-for-timing-equalization"

// dummyHashOnce guards the lazy computation of dummyHashValue.
var (
	dummyHashOnce  sync.Once
	dummyHashValue string
)

// dummyHash returns a bcrypt hash of dummyPassword at
// bcrypt.DefaultCost. SignIn compares against it when the requested
// user does not exist so that response timing does not reveal whether
// an email address is registered. It is computed on first use instead
// of at package initialization, so importing the package costs
// nothing.
func dummyHash() string {
	dummyHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte(dummyPassword), bcrypt.DefaultCost)
		if err != nil {
			// Unreachable for a short constant input at DefaultCost.
			panic("auth: computing dummy hash: " + err.Error())
		}
		dummyHashValue = string(hash)
	})
	return dummyHashValue
}

// dummyCompare runs one bcrypt comparison against the dummy hash and
// discards the result. It is a package variable so tests can observe
// that the timing equalization path executed.
var dummyCompare = func(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash()), []byte(password))
}

// HashPassword hashes password with bcrypt at bcrypt.DefaultCost.
//
// Passwords longer than 72 bytes cannot be hashed by bcrypt and yield
// an error; the service methods reject such passwords earlier via the
// password policy (ErrWeakPassword).
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches the bcrypt hash. It
// returns false on any mismatch, including a malformed or empty hash,
// instead of exposing the underlying bcrypt error.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// validatePassword checks password against the configured policy: it
// must be non-empty (ErrPasswordRequired) and between
// MinPasswordLength and 72 bytes long (ErrWeakPassword on either
// bound violation; 72 bytes is the bcrypt input limit).
func (a *Auth) validatePassword(password string) error {
	if password == "" {
		return ErrPasswordRequired
	}
	if len(password) < a.cfg.MinPasswordLength || len(password) > maxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}
