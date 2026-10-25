package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenEntropy is the amount of random entropy in a freshly generated
// token: 256 bits.
const tokenEntropy = 32

// NewToken returns a new opaque high-entropy token: 32 random bytes
// from crypto/rand, encoded with base64.RawURLEncoding (43 characters).
// Tokens are single use and must only be persisted as their SHA-256
// hash (see HashToken).
func NewToken() (string, error) {
	buf := make([]byte, tokenEntropy)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the SHA-256 hex digest of token. Session and
// verification tokens are always hashed before they are stored so a
// compromised store does not yield usable tokens.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
