package auth

import (
	"context"
	"errors"
	"time"
)

// Errors returned by Store implementations. Implementations must map
// backend specific errors (SQLSTATE 23505, MongoDB duplicate key, ...)
// to these sentinel errors so the service layer can react uniformly.
var (
	// ErrNotFound is returned when the requested record does not exist.
	ErrNotFound = errors.New("auth: record not found")

	// ErrDuplicate is returned when a create or update violates a unique
	// constraint, for example a second user with the same email.
	ErrDuplicate = errors.New("auth: duplicate record")

	// ErrConflict is returned by UpdateUser and UpdateAccount when the
	// stored record's Version no longer matches the Version of the
	// record passed in: somebody else modified it after it was read.
	// The service re-reads and retries such updates a few times and
	// returns ErrConflict itself only when every attempt lost the race.
	ErrConflict = errors.New("auth: record was modified concurrently")
)

// ListOptions controls pagination for list operations.
type ListOptions struct {
	// Limit is the maximum number of records returned. Zero selects the
	// store default (100). Stores may cap large values.
	Limit int
	// Offset skips the first Offset records. Zero starts from the
	// beginning.
	Offset int
}

// Store persists authentication records. All methods must be safe for
// concurrent use.
//
// Contracts every implementation must honor:
//
//   - Create methods populate ID, CreatedAt and UpdatedAt on the passed
//     struct when the caller left them zero, and keep caller supplied
//     values otherwise, so callers can rely on the values after a
//     successful call.
//   - Update methods replace the full record and bump UpdatedAt (in
//     the store and on the passed struct).
//   - Users and accounts use optimistic concurrency. CreateUser and
//     CreateAccount set Version to 1 on the passed struct (whatever it
//     held before). UpdateUser and UpdateAccount replace the record
//     only when the stored Version equals the passed Version; on
//     success they increment Version in the store and on the passed
//     struct. A missing record yields ErrNotFound and a Version
//     mismatch yields ErrConflict; neither changes anything. The check
//     and the write must be atomic (one conditional statement or one
//     critical section). Get and List methods return the stored
//     Version.
//   - Get methods return ErrNotFound when nothing matches; list methods
//     return an empty (non nil) slice instead.
//   - Single record deletes return ErrNotFound when nothing matches;
//     bulk deletes (DeleteSessionsByUser,
//     DeleteVerificationTokensByIdentifier, DeleteExpiredSessions,
//     DeleteExpiredVerificationTokens) succeed on zero matches.
//   - Plaintext secrets are never stored: Account.Password holds a
//     bcrypt hash and Session.TokenHash / VerificationToken.ValueHash
//     hold SHA-256 hex digests supplied by the service layer.
//   - Deleting a user cascades to its accounts and sessions. Verification
//     tokens are NOT the store's responsibility: their identifiers are
//     purpose prefixed, and the service removes them itself before it
//     deletes the user.
//   - "Expired" means ExpiresAt strictly before the given instant.
type Store interface {
	// User records.
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	ListUsers(ctx context.Context, opts ListOptions) ([]*User, error)
	CountUsers(ctx context.Context) (int64, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id string) error

	// Account records.
	CreateAccount(ctx context.Context, account *Account) error
	GetAccount(ctx context.Context, provider, providerAccountID string) (*Account, error)
	GetAccountByUser(ctx context.Context, userID, provider string) (*Account, error)
	ListAccountsByUser(ctx context.Context, userID string) ([]*Account, error)
	UpdateAccount(ctx context.Context, account *Account) error
	DeleteAccount(ctx context.Context, id string) error

	// Session records.
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	ListSessionsByUser(ctx context.Context, userID string) ([]*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error
	DeleteSessionsByUser(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context, before time.Time) error

	// VerificationToken records.
	CreateVerificationToken(ctx context.Context, token *VerificationToken) error
	GetVerificationToken(ctx context.Context, identifier, valueHash string) (*VerificationToken, error)
	DeleteVerificationToken(ctx context.Context, id string) error
	DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error
	DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error
}
