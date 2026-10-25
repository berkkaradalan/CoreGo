package mongostore

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
)

// userDoc is the MongoDB representation of auth.User. Field names are
// snake_case and the entity ID doubles as the document _id, so IDs stay
// portable strings instead of ObjectIDs.
type userDoc struct {
	ID            string     `bson:"_id"`
	Name          string     `bson:"name"`
	Email         string     `bson:"email"`
	EmailVerified bool       `bson:"email_verified"`
	Image         string     `bson:"image"`
	Banned        bool       `bson:"banned"`
	BanReason     string     `bson:"ban_reason"`
	BanExpires    *time.Time `bson:"ban_expires"`
	CreatedAt     time.Time  `bson:"created_at"`
	UpdatedAt     time.Time  `bson:"updated_at"`
	Version       int64      `bson:"version"`
}

// accountDoc is the MongoDB representation of auth.Account.
type accountDoc struct {
	ID                string    `bson:"_id"`
	UserID            string    `bson:"user_id"`
	Provider          string    `bson:"provider"`
	ProviderAccountID string    `bson:"provider_account_id"`
	Password          string    `bson:"password"`
	CreatedAt         time.Time `bson:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at"`
	Version           int64     `bson:"version"`
}

// sessionDoc is the MongoDB representation of auth.Session.
type sessionDoc struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"user_id"`
	TokenHash string    `bson:"token_hash"`
	ExpiresAt time.Time `bson:"expires_at"`
	IPAddress string    `bson:"ip_address"`
	UserAgent string    `bson:"user_agent"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// verificationTokenDoc is the MongoDB representation of
// auth.VerificationToken. The entity has no UpdatedAt field, so the
// document does not either.
type verificationTokenDoc struct {
	ID         string    `bson:"_id"`
	Identifier string    `bson:"identifier"`
	ValueHash  string    `bson:"value_hash"`
	ExpiresAt  time.Time `bson:"expires_at"`
	CreatedAt  time.Time `bson:"created_at"`
}

// userToDoc converts an auth.User to its document form.
func userToDoc(u *auth.User) *userDoc {
	return &userDoc{
		ID:            u.ID,
		Name:          u.Name,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		Image:         u.Image,
		Banned:        u.Banned,
		BanReason:     u.BanReason,
		BanExpires:    copyTime(u.BanExpires),
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		Version:       u.Version,
	}
}

// docToUser converts a document back to an auth.User.
func docToUser(d *userDoc) *auth.User {
	return &auth.User{
		ID:            d.ID,
		Name:          d.Name,
		Email:         d.Email,
		EmailVerified: d.EmailVerified,
		Image:         d.Image,
		Banned:        d.Banned,
		BanReason:     d.BanReason,
		BanExpires:    copyTime(d.BanExpires),
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
		Version:       d.Version,
	}
}

// accountToDoc converts an auth.Account to its document form.
func accountToDoc(a *auth.Account) *accountDoc {
	return &accountDoc{
		ID:                a.ID,
		UserID:            a.UserID,
		Provider:          a.Provider,
		ProviderAccountID: a.ProviderAccountID,
		Password:          a.Password,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
		Version:           a.Version,
	}
}

// docToAccount converts a document back to an auth.Account.
func docToAccount(d *accountDoc) *auth.Account {
	return &auth.Account{
		ID:                d.ID,
		UserID:            d.UserID,
		Provider:          d.Provider,
		ProviderAccountID: d.ProviderAccountID,
		Password:          d.Password,
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
		Version:           d.Version,
	}
}

// sessionToDoc converts an auth.Session to its document form.
func sessionToDoc(s *auth.Session) *sessionDoc {
	return &sessionDoc{
		ID:        s.ID,
		UserID:    s.UserID,
		TokenHash: s.TokenHash,
		ExpiresAt: s.ExpiresAt,
		IPAddress: s.IPAddress,
		UserAgent: s.UserAgent,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// docToSession converts a document back to an auth.Session.
func docToSession(d *sessionDoc) *auth.Session {
	return &auth.Session{
		ID:        d.ID,
		UserID:    d.UserID,
		TokenHash: d.TokenHash,
		ExpiresAt: d.ExpiresAt,
		IPAddress: d.IPAddress,
		UserAgent: d.UserAgent,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// tokenToDoc converts an auth.VerificationToken to its document form.
func tokenToDoc(t *auth.VerificationToken) *verificationTokenDoc {
	return &verificationTokenDoc{
		ID:         t.ID,
		Identifier: t.Identifier,
		ValueHash:  t.ValueHash,
		ExpiresAt:  t.ExpiresAt,
		CreatedAt:  t.CreatedAt,
	}
}

// docToToken converts a document back to an auth.VerificationToken.
func docToToken(d *verificationTokenDoc) *auth.VerificationToken {
	return &auth.VerificationToken{
		ID:         d.ID,
		Identifier: d.Identifier,
		ValueHash:  d.ValueHash,
		ExpiresAt:  d.ExpiresAt,
		CreatedAt:  d.CreatedAt,
	}
}

// copyTime returns a copy of *t, or nil for nil, so documents and
// records never share a pointer.
func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := *t
	return &copied
}

// newID returns a 32 character hex string built from 16 crypto/rand
// bytes: the ID format of every store backend, so IDs stay portable.
//
// A failure of crypto/rand is unrecoverable (the system entropy source
// is broken), so newID panics rather than silently degrading.
func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("mongostore: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// ensureID returns id, or a freshly generated ID when id is empty.
func ensureID(id string) string {
	if id == "" {
		return newID()
	}
	return id
}

// Time precision
//
// BSON dates have millisecond precision; the driver floors the
// nanoseconds of a time.Time when it encodes one. Two consequences
// shape the helpers below:
//
//   - Flooring is monotonic: if a <= b then floor(a) <= floor(b). The
//     auth service rejects a verification token whose CreatedAt is
//     before its user's CreatedAt; both values are read back from this
//     store, so a token created in the same millisecond as (or any time
//     after) its user always compares as "not before" and is accepted.
//   - Timestamps the store generates itself (zero CreatedAt/UpdatedAt
//     on create, the UpdatedAt bump on update) are produced with
//     millisecond precision, so the value written back into the
//     caller's struct is exactly what a later read returns.

// storeNow returns the current UTC time with the millisecond precision
// MongoDB stores.
func storeNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// ensureCreateTimestamps replaces zero timestamps with storeNow and
// leaves caller supplied values untouched. updatedAt may be nil for
// entities without that field (auth.VerificationToken).
func ensureCreateTimestamps(createdAt, updatedAt *time.Time) {
	now := storeNow()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt != nil && updatedAt.IsZero() {
		*updatedAt = now
	}
}

// ceilMillis rounds t up to the next whole millisecond (t itself when
// it already is one).
//
// The bulk expiry deletes use it for their bound: a stored ExpiresAt is
// a whole millisecond m, and for such values m < t holds exactly when
// m < ceilMillis(t). Querying with the ceiled bound therefore deletes
// precisely the records whose stored ExpiresAt is strictly before t,
// even when t carries sub-millisecond digits the driver would
// otherwise floor away.
func ceilMillis(t time.Time) time.Time {
	floored := t.Truncate(time.Millisecond)
	if floored.Equal(t) {
		return floored
	}
	return floored.Add(time.Millisecond)
}
