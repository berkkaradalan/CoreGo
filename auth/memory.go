package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// defaultListLimit is the page size used by MemoryStore list operations
// when ListOptions.Limit is zero or negative.
const defaultListLimit = 100

// Compile-time check that MemoryStore satisfies Store.
var _ Store = (*MemoryStore)(nil)

// MemoryStore is a complete in-memory Store implementation intended
// for development, examples and tests. Use mongostore or pgstore for
// persistent storage.
//
// Behavior beyond the general Store contract:
//
//   - Get and List methods return deep copies: mutating a returned
//     record never changes store state, and Update stores a copy of
//     the record passed in.
//   - Unique constraints (user email, account provider+providerAccountID,
//     account userID+provider, session token hash and verification
//     token identifier+valueHash) are enforced atomically under the
//     write lock; violated creates and updates return ErrDuplicate.
//   - Record IDs are 32 hex characters generated with crypto/rand.
//   - List methods sort by CreatedAt (users and sessions newest first,
//     accounts oldest first) with the record ID as a deterministic
//     tie-breaker, then apply Limit (default 100) and Offset.
//   - Single record deletes return ErrNotFound when the record is
//     missing; bulk deletes (DeleteSessionsByUser, DeleteExpiredSessions,
//     DeleteVerificationTokensByIdentifier,
//     DeleteExpiredVerificationTokens) and the user cascade succeed on
//     zero matches.
//   - DeleteUser cascades to accounts and sessions only; verification
//     tokens are removed by the service.
//   - Update methods force UpdatedAt to the current time, in the
//     store and on the passed struct.
//   - Users and accounts are versioned (optimistic concurrency):
//     creates set Version to 1, and UpdateUser / UpdateAccount succeed
//     only when the passed Version equals the stored one, incrementing
//     it on success. A stale Version yields ErrConflict; the check
//     runs before the unique constraint checks, all under the write
//     lock.
//   - Timestamps the store sets itself (zero CreatedAt and UpdatedAt on
//     create, UpdatedAt on update) are in UTC, like the database stores.
type MemoryStore struct {
	mu sync.RWMutex

	users   map[string]*User  // user ID -> record
	byEmail map[string]string // email -> user ID

	accounts          map[string]*Account // account ID -> record
	accountByProvider map[accountProviderKey]string
	accountByUser     map[accountUserKey]string

	sessions       map[string]*Session // session ID -> record
	sessionByToken map[string]string   // token hash -> session ID

	tokens            map[string]*VerificationToken // token ID -> record
	tokenByIdentifier map[identifierKey]string      // identifier+valueHash -> token ID
}

// Composite map keys for the unique indexes above.
type (
	accountProviderKey struct {
		provider          string
		providerAccountID string
	}
	accountUserKey struct {
		userID   string
		provider string
	}
	identifierKey struct {
		identifier string
		valueHash  string
	}
)

// NewMemoryStore returns an empty MemoryStore ready for use.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:             make(map[string]*User),
		byEmail:           make(map[string]string),
		accounts:          make(map[string]*Account),
		accountByProvider: make(map[accountProviderKey]string),
		accountByUser:     make(map[accountUserKey]string),
		sessions:          make(map[string]*Session),
		sessionByToken:    make(map[string]string),
		tokens:            make(map[string]*VerificationToken),
		tokenByIdentifier: make(map[identifierKey]string),
	}
}

// newMemoryID returns a random 32 character hex ID. Failing to read
// crypto/rand is unrecoverable for an in-memory store, so it panics.
func newMemoryID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("auth: MemoryStore ID generation: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// stampCreate fills the zero ID and timestamp fields of a newly created
// record and returns the current time in UTC.
func stampCreate(id *string, createdAt, updatedAt *time.Time) time.Time {
	if *id == "" {
		*id = newMemoryID()
	}
	now := time.Now().UTC()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = now
	}
	return now
}

// paginate applies opts to a sorted slice and always returns a non-nil
// slice.
func paginate[T any](items []T, opts ListOptions) []T {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// cloneUser returns a deep copy of u.
func cloneUser(u *User) *User {
	copied := *u
	if u.BanExpires != nil {
		expires := *u.BanExpires
		copied.BanExpires = &expires
	}
	return &copied
}

// cloneAccount returns a copy of a.
func cloneAccount(a *Account) *Account {
	copied := *a
	return &copied
}

// cloneSession returns a copy of s.
func cloneSession(s *Session) *Session {
	copied := *s
	return &copied
}

// cloneToken returns a copy of t.
func cloneToken(t *VerificationToken) *VerificationToken {
	copied := *t
	return &copied
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// CreateUser implements Store.
func (s *MemoryStore) CreateUser(_ context.Context, user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byEmail[user.Email]; exists {
		return ErrDuplicate
	}
	if user.ID != "" {
		if _, exists := s.users[user.ID]; exists {
			return ErrDuplicate
		}
	}
	stampCreate(&user.ID, &user.CreatedAt, &user.UpdatedAt)
	user.Version = 1
	s.users[user.ID] = cloneUser(user)
	s.byEmail[user.Email] = user.ID
	return nil
}

// GetUser implements Store.
func (s *MemoryStore) GetUser(_ context.Context, id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneUser(user), nil
}

// GetUserByEmail implements Store.
func (s *MemoryStore) GetUserByEmail(_ context.Context, email string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byEmail[email]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneUser(s.users[id]), nil
}

// ListUsers implements Store. Users are returned newest first.
func (s *MemoryStore) ListUsers(_ context.Context, opts ListOptions) ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		out = append(out, cloneUser(user))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return paginate(out, opts), nil
}

// CountUsers implements Store.
func (s *MemoryStore) CountUsers(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return int64(len(s.users)), nil
}

// UpdateUser implements Store. It replaces the user only when
// user.Version matches the stored Version (ErrConflict otherwise) and
// then increments Version and sets UpdatedAt on user as well.
func (s *MemoryStore) UpdateUser(_ context.Context, user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, ok := s.users[user.ID]
	if !ok {
		return ErrNotFound
	}
	if user.Version != old.Version {
		return ErrConflict
	}
	if user.Email != old.Email {
		if _, duplicate := s.byEmail[user.Email]; duplicate {
			return ErrDuplicate
		}
		delete(s.byEmail, old.Email)
		s.byEmail[user.Email] = user.ID
	}
	user.Version = old.Version + 1
	user.UpdatedAt = time.Now().UTC()
	s.users[user.ID] = cloneUser(user)
	return nil
}

// DeleteUser implements Store. Deleting a user cascades to its
// accounts and sessions; verification tokens are left to the service.
func (s *MemoryStore) DeleteUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.users, id)
	delete(s.byEmail, user.Email)

	for accountID, account := range s.accounts {
		if account.UserID == id {
			s.removeAccount(accountID, account)
		}
	}
	for sessionID, session := range s.sessions {
		if session.UserID == id {
			s.removeSession(sessionID, session)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// CreateAccount implements Store.
func (s *MemoryStore) CreateAccount(_ context.Context, account *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	providerKey := accountProviderKey{account.Provider, account.ProviderAccountID}
	if _, duplicate := s.accountByProvider[providerKey]; duplicate {
		return ErrDuplicate
	}
	userKey := accountUserKey{account.UserID, account.Provider}
	if _, duplicate := s.accountByUser[userKey]; duplicate {
		return ErrDuplicate
	}
	if account.ID != "" {
		if _, exists := s.accounts[account.ID]; exists {
			return ErrDuplicate
		}
	}
	stampCreate(&account.ID, &account.CreatedAt, &account.UpdatedAt)
	account.Version = 1
	s.accounts[account.ID] = cloneAccount(account)
	s.accountByProvider[providerKey] = account.ID
	s.accountByUser[userKey] = account.ID
	return nil
}

// GetAccount implements Store.
func (s *MemoryStore) GetAccount(_ context.Context, provider, providerAccountID string) (*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.accountByProvider[accountProviderKey{provider, providerAccountID}]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneAccount(s.accounts[id]), nil
}

// GetAccountByUser implements Store.
func (s *MemoryStore) GetAccountByUser(_ context.Context, userID, provider string) (*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.accountByUser[accountUserKey{userID, provider}]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneAccount(s.accounts[id]), nil
}

// ListAccountsByUser implements Store. Accounts are returned oldest
// first.
func (s *MemoryStore) ListAccountsByUser(_ context.Context, userID string) ([]*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Account, 0)
	for _, account := range s.accounts {
		if account.UserID == userID {
			out = append(out, cloneAccount(account))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdateAccount implements Store. It replaces the account only when
// account.Version matches the stored Version (ErrConflict otherwise)
// and then increments Version and sets UpdatedAt on account as well.
func (s *MemoryStore) UpdateAccount(_ context.Context, account *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, ok := s.accounts[account.ID]
	if !ok {
		return ErrNotFound
	}
	if account.Version != old.Version {
		return ErrConflict
	}

	oldProviderKey := accountProviderKey{old.Provider, old.ProviderAccountID}
	newProviderKey := accountProviderKey{account.Provider, account.ProviderAccountID}
	if newProviderKey != oldProviderKey {
		if _, duplicate := s.accountByProvider[newProviderKey]; duplicate {
			return ErrDuplicate
		}
	}
	oldUserKey := accountUserKey{old.UserID, old.Provider}
	newUserKey := accountUserKey{account.UserID, account.Provider}
	if newUserKey != oldUserKey {
		if _, duplicate := s.accountByUser[newUserKey]; duplicate {
			return ErrDuplicate
		}
	}

	if newProviderKey != oldProviderKey {
		delete(s.accountByProvider, oldProviderKey)
		s.accountByProvider[newProviderKey] = account.ID
	}
	if newUserKey != oldUserKey {
		delete(s.accountByUser, oldUserKey)
		s.accountByUser[newUserKey] = account.ID
	}
	account.Version = old.Version + 1
	account.UpdatedAt = time.Now().UTC()
	s.accounts[account.ID] = cloneAccount(account)
	return nil
}

// DeleteAccount implements Store.
func (s *MemoryStore) DeleteAccount(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.accounts[id]
	if !ok {
		return ErrNotFound
	}
	s.removeAccount(id, account)
	return nil
}

// removeAccount deletes an account and its unique indexes. The write
// lock must be held.
func (s *MemoryStore) removeAccount(id string, account *Account) {
	delete(s.accounts, id)
	delete(s.accountByProvider, accountProviderKey{account.Provider, account.ProviderAccountID})
	delete(s.accountByUser, accountUserKey{account.UserID, account.Provider})
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// CreateSession implements Store.
func (s *MemoryStore) CreateSession(_ context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, duplicate := s.sessionByToken[session.TokenHash]; duplicate {
		return ErrDuplicate
	}
	if session.ID != "" {
		if _, exists := s.sessions[session.ID]; exists {
			return ErrDuplicate
		}
	}
	stampCreate(&session.ID, &session.CreatedAt, &session.UpdatedAt)
	s.sessions[session.ID] = cloneSession(session)
	s.sessionByToken[session.TokenHash] = session.ID
	return nil
}

// GetSession implements Store.
func (s *MemoryStore) GetSession(_ context.Context, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneSession(session), nil
}

// GetSessionByTokenHash implements Store.
func (s *MemoryStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.sessionByToken[tokenHash]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneSession(s.sessions[id]), nil
}

// ListSessionsByUser implements Store. Sessions are returned newest
// first.
func (s *MemoryStore) ListSessionsByUser(_ context.Context, userID string) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID {
			out = append(out, cloneSession(session))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// UpdateSession implements Store.
func (s *MemoryStore) UpdateSession(_ context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, ok := s.sessions[session.ID]
	if !ok {
		return ErrNotFound
	}
	if session.TokenHash != old.TokenHash {
		if _, duplicate := s.sessionByToken[session.TokenHash]; duplicate {
			return ErrDuplicate
		}
		delete(s.sessionByToken, old.TokenHash)
		s.sessionByToken[session.TokenHash] = session.ID
	}
	stored := cloneSession(session)
	stored.UpdatedAt = time.Now().UTC()
	s.sessions[session.ID] = stored
	return nil
}

// DeleteSession implements Store.
func (s *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.removeSession(id, session)
	return nil
}

// DeleteSessionsByUser implements Store. It succeeds when the user has
// no sessions.
func (s *MemoryStore) DeleteSessionsByUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sessionID, session := range s.sessions {
		if session.UserID == userID {
			s.removeSession(sessionID, session)
		}
	}
	return nil
}

// DeleteExpiredSessions implements Store. Sessions are removed when
// their ExpiresAt lies strictly before the given time.
func (s *MemoryStore) DeleteExpiredSessions(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sessionID, session := range s.sessions {
		if session.ExpiresAt.Before(before) {
			s.removeSession(sessionID, session)
		}
	}
	return nil
}

// removeSession deletes a session and its token index. The write lock
// must be held.
func (s *MemoryStore) removeSession(id string, session *Session) {
	delete(s.sessions, id)
	delete(s.sessionByToken, session.TokenHash)
}

// ---------------------------------------------------------------------------
// Verification tokens
// ---------------------------------------------------------------------------

// CreateVerificationToken implements Store.
func (s *MemoryStore) CreateVerificationToken(_ context.Context, token *VerificationToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := identifierKey{token.Identifier, token.ValueHash}
	if _, duplicate := s.tokenByIdentifier[key]; duplicate {
		return ErrDuplicate
	}
	if token.ID != "" {
		if _, exists := s.tokens[token.ID]; exists {
			return ErrDuplicate
		}
	}
	if token.ID == "" {
		token.ID = newMemoryID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	s.tokens[token.ID] = cloneToken(token)
	s.tokenByIdentifier[key] = token.ID
	return nil
}

// GetVerificationToken implements Store.
func (s *MemoryStore) GetVerificationToken(_ context.Context, identifier, valueHash string) (*VerificationToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.tokenByIdentifier[identifierKey{identifier, valueHash}]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneToken(s.tokens[id]), nil
}

// DeleteVerificationToken implements Store.
func (s *MemoryStore) DeleteVerificationToken(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[id]
	if !ok {
		return ErrNotFound
	}
	s.removeToken(id, token)
	return nil
}

// DeleteVerificationTokensByIdentifier implements Store. It removes
// every token whose identifier matches exactly and succeeds on zero
// matches.
func (s *MemoryStore) DeleteVerificationTokensByIdentifier(_ context.Context, identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for tokenID, token := range s.tokens {
		if token.Identifier == identifier {
			s.removeToken(tokenID, token)
		}
	}
	return nil
}

// DeleteExpiredVerificationTokens implements Store. Tokens are removed
// when their ExpiresAt lies strictly before the given time.
func (s *MemoryStore) DeleteExpiredVerificationTokens(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for tokenID, token := range s.tokens {
		if token.ExpiresAt.Before(before) {
			s.removeToken(tokenID, token)
		}
	}
	return nil
}

// removeToken deletes a verification token and its unique index. The
// write lock must be held.
func (s *MemoryStore) removeToken(id string, token *VerificationToken) {
	delete(s.tokens, id)
	delete(s.tokenByIdentifier, identifierKey{token.Identifier, token.ValueHash})
}
