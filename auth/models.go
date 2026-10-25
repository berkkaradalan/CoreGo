// Package auth provides authentication and session management for
// applications built with CoreGo.
//
// The package is framework agnostic: the core service works with any
// storage backend that implements the Store interface and ships with an
// in-memory implementation for development and testing. Database backed
// implementations live in subpackages (mongostore, pgstore), and the
// HTTP layer lives in auth/authhttp.
package auth

import "time"

// User is the primary identity record. Emails are always stored
// lowercase.
//
// The JSON tags give the record a snake_case shape that is safe to
// render (Version is never marshalled). Note that it includes the ban
// fields; the auth/authhttp routes render their own, narrower user
// object.
type User struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Image         string     `json:"image"`
	Banned        bool       `json:"banned"`
	BanReason     string     `json:"ban_reason"`
	BanExpires    *time.Time `json:"ban_expires,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Version is the optimistic concurrency counter managed by the
	// store: Store.CreateUser sets it to 1 and every successful
	// Store.UpdateUser increments it. Never set it yourself except by
	// passing back the value you read; an update carrying a stale
	// Version fails with ErrConflict instead of overwriting a
	// concurrent change. It is never marshalled to JSON.
	Version int64 `json:"-"`
}

// IsBanned reports whether the user is banned right now according to
// the wall clock. It is shorthand for IsBannedAt(time.Now()).
func (u *User) IsBanned() bool {
	return u.IsBannedAt(time.Now())
}

// IsBannedAt reports whether the user is banned at the instant t. A
// ban without BanExpires is permanent; a ban whose BanExpires is not
// after t has lapsed and no longer counts.
func (u *User) IsBannedAt(t time.Time) bool {
	if !u.Banned {
		return false
	}
	if u.BanExpires == nil {
		return true
	}
	return t.Before(*u.BanExpires)
}

// Account links a user to an authentication provider. A user may hold
// multiple accounts, for example a credential account and a Google
// account. The password hash and Version are never marshalled to JSON.
type Account struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	Provider          string    `json:"provider"`            // "credential", "google", "apple", "facebook", ...
	ProviderAccountID string    `json:"provider_account_id"` // provider specific user id; the email for credential accounts
	Password          string    `json:"-"`                   // bcrypt hash; empty for OAuth accounts
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// Version is the optimistic concurrency counter managed by the
	// store: Store.CreateAccount sets it to 1 and every successful
	// Store.UpdateAccount increments it. Never set it yourself except
	// by passing back the value you read; an update carrying a stale
	// Version fails with ErrConflict instead of overwriting a
	// concurrent change. It is never marshalled to JSON.
	Version int64 `json:"-"`
}

// Session represents a server side session. Only the SHA-256 hash of the
// session token is persisted; the raw token is returned to the caller
// once and never stored. The token hash is never marshalled to JSON.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VerificationToken is a single use token used for email verification
// and password reset. Identifier is what the token authenticates: the
// service stores a purpose prefix followed by the email address (for
// example "password-reset:ada@example.com"), so a token issued for one
// flow can never be redeemed in another. Only the SHA-256 hash of the
// token value is persisted. The value hash is never marshalled to JSON.
type VerificationToken struct {
	ID         string    `json:"id"`
	Identifier string    `json:"identifier"`
	ValueHash  string    `json:"-"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}
