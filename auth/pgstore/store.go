package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/berkkaradalan/CoreGo/auth"
)

// Guard against programming mistakes where a nil model pointer is
// passed to a mutating method.
var (
	errNilUser              = errors.New("pgstore: user must not be nil")
	errNilAccount           = errors.New("pgstore: account must not be nil")
	errNilSession           = errors.New("pgstore: session must not be nil")
	errNilVerificationToken = errors.New("pgstore: verification token must not be nil")
)

// Store implements auth.Store on PostgreSQL. It is safe for concurrent
// use by multiple goroutines; every method runs a single statement on
// the shared connection pool with the caller's context.
type Store struct {
	pool       *pgxpool.Pool
	prefix     string
	q          queries
	migrations []plannedMigration
	lockKey    int64
}

var _ auth.Store = (*Store)(nil)

// NewStore returns a Store that issues its queries through pool, a
// connected pool such as the one returned by database.PostgresDB.Pool.
// The store does not own the pool: closing it remains the caller's
// job.
//
// opts.TablePrefix selects the table names (see Options). NewStore
// returns ErrNilPool for a nil pool and ErrInvalidTablePrefix for an
// unusable prefix. It does not touch the database; call Migrate before
// the store is used.
func NewStore(pool *pgxpool.Pool, opts Options) (*Store, error) {
	if pool == nil {
		return nil, ErrNilPool
	}
	prefix, err := resolvePrefix(opts)
	if err != nil {
		return nil, err
	}
	return &Store{
		pool:       pool,
		prefix:     prefix,
		q:          queryTemplates.render(prefix),
		migrations: planMigrations(prefix),
		lockKey:    migrationLockKey(prefix),
	}, nil
}

// TablePrefix returns the effective table prefix of the store, for
// example "corego_" when Options.TablePrefix was left empty. Append a
// table name ("users", "accounts", "sessions", "verification_tokens",
// "schema_migrations") to get the full name.
func (s *Store) TablePrefix() string {
	return s.prefix
}

// execOne runs a statement that must affect exactly one row and maps a
// zero row count to auth.ErrNotFound.
func (s *Store) execOne(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// execBulk runs a statement that may affect any number of rows,
// including none.
func (s *Store) execBulk(ctx context.Context, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return mapError(err)
}

// updateVersioned runs a version-guarded UPDATE ... RETURNING version
// statement and returns the new version. When no row was updated it
// runs existsSQL with id to tell the two causes apart: a missing row
// yields auth.ErrNotFound, a row whose version moved on in the meantime
// auth.ErrConflict.
func (s *Store) updateVersioned(ctx context.Context, updateSQL, existsSQL, id string, args ...any) (int64, error) {
	var version int64
	err := s.pool.QueryRow(ctx, updateSQL, args...).Scan(&version)
	if err == nil {
		return version, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, mapError(err)
	}
	var one int
	if err := s.pool.QueryRow(ctx, existsSQL, id).Scan(&one); err != nil {
		// pgx.ErrNoRows maps to auth.ErrNotFound.
		return 0, mapError(err)
	}
	return 0, auth.ErrConflict
}

// queryAll runs a query and scans every row with scan. The result is
// never nil.
func queryAll[T any](ctx context.Context, s *Store, scan func(pgx.Row) (*T, error), sql string, args ...any) ([]*T, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make([]*T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// scanUser reads one users row from a pgx.Row or pgx.Rows.
func scanUser(row pgx.Row) (*auth.User, error) {
	var u auth.User
	if err := row.Scan(
		&u.ID,
		&u.Name,
		&u.Email,
		&u.EmailVerified,
		&u.Image,
		&u.Banned,
		&u.BanReason,
		&u.BanExpires,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.Version,
	); err != nil {
		return nil, mapError(err)
	}
	if u.BanExpires != nil {
		toUTC(u.BanExpires)
	}
	toUTC(&u.CreatedAt)
	toUTC(&u.UpdatedAt)
	return &u, nil
}

// CreateUser implements auth.Store. A zero ID becomes a random UUIDv4
// and zero timestamps become the current time; every timestamp on the
// passed struct is normalized to UTC at microsecond precision so it
// equals the stored value. Version is set to 1 (also on the passed
// struct), whatever the caller put there. A taken email or ID returns
// auth.ErrDuplicate.
func (s *Store) CreateUser(ctx context.Context, user *auth.User) error {
	if user == nil {
		return errNilUser
	}
	if user.ID == "" {
		user.ID = newID()
	}
	now := nowUTC()
	user.CreatedAt = utcOrNow(user.CreatedAt, now)
	user.UpdatedAt = utcOrNow(user.UpdatedAt, now)
	user.BanExpires = normalizeTimePtr(user.BanExpires)
	user.Version = 1

	return s.execBulk(ctx, s.q.createUser,
		user.ID,
		user.Name,
		user.Email,
		user.EmailVerified,
		user.Image,
		user.Banned,
		user.BanReason,
		user.BanExpires,
		user.CreatedAt,
		user.UpdatedAt,
		user.Version,
	)
}

// GetUser implements auth.Store.
func (s *Store) GetUser(ctx context.Context, id string) (*auth.User, error) {
	return scanUser(s.pool.QueryRow(ctx, s.q.getUser, id))
}

// GetUserByEmail implements auth.Store. The address is matched
// exactly; the auth service normalizes addresses to lowercase before
// they reach the store.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	return scanUser(s.pool.QueryRow(ctx, s.q.getUserByEmail, email))
}

// ListUsers implements auth.Store. Users are returned newest first
// (ties broken by ID); Limit <= 0 selects 100 and a negative Offset
// counts as 0.
func (s *Store) ListUsers(ctx context.Context, opts auth.ListOptions) ([]*auth.User, error) {
	limit, offset := normalizeListOptions(opts)
	return queryAll(ctx, s, scanUser, s.q.listUsers, limit, offset)
}

// CountUsers implements auth.Store.
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	if err := s.pool.QueryRow(ctx, s.q.countUsers).Scan(&count); err != nil {
		return 0, mapError(err)
	}
	return count, nil
}

// UpdateUser implements auth.Store with optimistic concurrency. The
// row is replaced (every column except created_at) only while its
// stored version still equals user.Version; on success the version is
// incremented and UpdatedAt set to the current time, both in the row
// and on the passed struct. A missing user returns auth.ErrNotFound, a
// version mismatch auth.ErrConflict (the row is left untouched), and an
// email taken by another user auth.ErrDuplicate. The passed struct is
// only modified on success.
func (s *Store) UpdateUser(ctx context.Context, user *auth.User) error {
	if user == nil {
		return errNilUser
	}
	updatedAt := nowUTC()
	banExpires := normalizeTimePtr(user.BanExpires)

	version, err := s.updateVersioned(ctx, s.q.updateUser, s.q.userExists, user.ID,
		user.Name,
		user.Email,
		user.EmailVerified,
		user.Image,
		user.Banned,
		user.BanReason,
		banExpires,
		updatedAt,
		user.ID,
		user.Version,
	)
	if err != nil {
		return err
	}
	user.BanExpires = banExpires
	user.UpdatedAt = updatedAt
	user.Version = version
	return nil
}

// DeleteUser implements auth.Store. The user's accounts and sessions
// are removed by the ON DELETE CASCADE foreign keys in the same
// statement; verification tokens are left to the service.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.execOne(ctx, s.q.deleteUser, id)
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// scanAccount reads one accounts row from a pgx.Row or pgx.Rows.
func scanAccount(row pgx.Row) (*auth.Account, error) {
	var a auth.Account
	if err := row.Scan(
		&a.ID,
		&a.UserID,
		&a.Provider,
		&a.ProviderAccountID,
		&a.Password,
		&a.CreatedAt,
		&a.UpdatedAt,
		&a.Version,
	); err != nil {
		return nil, mapError(err)
	}
	toUTC(&a.CreatedAt)
	toUTC(&a.UpdatedAt)
	return &a, nil
}

// CreateAccount implements auth.Store. ID, timestamps and Version are
// populated like in CreateUser. A second account for the same provider account,
// a second account of one provider for the same user, or a taken ID
// returns auth.ErrDuplicate. Because accounts reference users with a
// foreign key, an account for a user that does not exist fails with
// PostgreSQL's foreign key violation (*pgconn.PgError, SQLSTATE 23503).
func (s *Store) CreateAccount(ctx context.Context, account *auth.Account) error {
	if account == nil {
		return errNilAccount
	}
	if account.ID == "" {
		account.ID = newID()
	}
	now := nowUTC()
	account.CreatedAt = utcOrNow(account.CreatedAt, now)
	account.UpdatedAt = utcOrNow(account.UpdatedAt, now)
	account.Version = 1

	return s.execBulk(ctx, s.q.createAccount,
		account.ID,
		account.UserID,
		account.Provider,
		account.ProviderAccountID,
		account.Password,
		account.CreatedAt,
		account.UpdatedAt,
		account.Version,
	)
}

// GetAccount implements auth.Store.
func (s *Store) GetAccount(ctx context.Context, provider, providerAccountID string) (*auth.Account, error) {
	return scanAccount(s.pool.QueryRow(ctx, s.q.getAccount, provider, providerAccountID))
}

// GetAccountByUser implements auth.Store.
func (s *Store) GetAccountByUser(ctx context.Context, userID, provider string) (*auth.Account, error) {
	return scanAccount(s.pool.QueryRow(ctx, s.q.getAccountByUser, userID, provider))
}

// ListAccountsByUser implements auth.Store. Accounts are returned
// oldest first (ties broken by ID).
func (s *Store) ListAccountsByUser(ctx context.Context, userID string) ([]*auth.Account, error) {
	return queryAll(ctx, s, scanAccount, s.q.listAccountsByUser, userID)
}

// UpdateAccount implements auth.Store with optimistic concurrency,
// exactly like UpdateUser: the row is replaced (every column except
// created_at) only while its stored version equals account.Version, and
// on success Version and UpdatedAt advance in the row and on the passed
// struct. A missing account returns auth.ErrNotFound, a version
// mismatch auth.ErrConflict, and a collision with another account's
// unique pair auth.ErrDuplicate. The passed struct is only modified on
// success.
func (s *Store) UpdateAccount(ctx context.Context, account *auth.Account) error {
	if account == nil {
		return errNilAccount
	}
	updatedAt := nowUTC()

	version, err := s.updateVersioned(ctx, s.q.updateAccount, s.q.accountExists, account.ID,
		account.UserID,
		account.Provider,
		account.ProviderAccountID,
		account.Password,
		updatedAt,
		account.ID,
		account.Version,
	)
	if err != nil {
		return err
	}
	account.UpdatedAt = updatedAt
	account.Version = version
	return nil
}

// DeleteAccount implements auth.Store.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	return s.execOne(ctx, s.q.deleteAccount, id)
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// scanSession reads one sessions row from a pgx.Row or pgx.Rows.
func scanSession(row pgx.Row) (*auth.Session, error) {
	var sess auth.Session
	if err := row.Scan(
		&sess.ID,
		&sess.UserID,
		&sess.TokenHash,
		&sess.ExpiresAt,
		&sess.IPAddress,
		&sess.UserAgent,
		&sess.CreatedAt,
		&sess.UpdatedAt,
	); err != nil {
		return nil, mapError(err)
	}
	toUTC(&sess.ExpiresAt)
	toUTC(&sess.CreatedAt)
	toUTC(&sess.UpdatedAt)
	return &sess, nil
}

// CreateSession implements auth.Store. ID and timestamps are populated
// like in CreateUser, and ExpiresAt is normalized the same way. A
// token hash or ID that already exists returns auth.ErrDuplicate; a
// session for a user that does not exist fails with PostgreSQL's
// foreign key violation (SQLSTATE 23503).
func (s *Store) CreateSession(ctx context.Context, session *auth.Session) error {
	if session == nil {
		return errNilSession
	}
	if session.ID == "" {
		session.ID = newID()
	}
	now := nowUTC()
	session.CreatedAt = utcOrNow(session.CreatedAt, now)
	session.UpdatedAt = utcOrNow(session.UpdatedAt, now)
	session.ExpiresAt = normalizeTime(session.ExpiresAt)

	return s.execBulk(ctx, s.q.createSession,
		session.ID,
		session.UserID,
		session.TokenHash,
		session.ExpiresAt,
		session.IPAddress,
		session.UserAgent,
		session.CreatedAt,
		session.UpdatedAt,
	)
}

// GetSession implements auth.Store.
func (s *Store) GetSession(ctx context.Context, id string) (*auth.Session, error) {
	return scanSession(s.pool.QueryRow(ctx, s.q.getSession, id))
}

// GetSessionByTokenHash implements auth.Store. Expiry is not checked
// here; the service decides what an expired session means.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error) {
	return scanSession(s.pool.QueryRow(ctx, s.q.getSessionByTokenHash, tokenHash))
}

// ListSessionsByUser implements auth.Store. Sessions are returned
// newest first (ties broken by ID).
func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]*auth.Session, error) {
	return queryAll(ctx, s, scanSession, s.q.listSessionsByUser, userID)
}

// UpdateSession implements auth.Store. It replaces every column except
// created_at and sets UpdatedAt to the current time (also on the
// passed struct).
func (s *Store) UpdateSession(ctx context.Context, session *auth.Session) error {
	if session == nil {
		return errNilSession
	}
	session.UpdatedAt = nowUTC()
	session.ExpiresAt = normalizeTime(session.ExpiresAt)

	return s.execOne(ctx, s.q.updateSession,
		session.UserID,
		session.TokenHash,
		session.ExpiresAt,
		session.IPAddress,
		session.UserAgent,
		session.UpdatedAt,
		session.ID,
	)
}

// DeleteSession implements auth.Store.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.execOne(ctx, s.q.deleteSession, id)
}

// DeleteSessionsByUser implements auth.Store. It succeeds when nothing
// matches.
func (s *Store) DeleteSessionsByUser(ctx context.Context, userID string) error {
	return s.execBulk(ctx, s.q.deleteSessionsByUser, userID)
}

// DeleteExpiredSessions implements auth.Store. It removes every session
// whose ExpiresAt lies strictly before the given instant and succeeds
// when nothing matches.
func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	return s.execBulk(ctx, s.q.deleteExpiredSessions, expiryCutoff(before))
}

// ---------------------------------------------------------------------------
// Verification tokens
// ---------------------------------------------------------------------------

// scanVerificationToken reads one verification_tokens row from a
// pgx.Row or pgx.Rows.
func scanVerificationToken(row pgx.Row) (*auth.VerificationToken, error) {
	var tok auth.VerificationToken
	if err := row.Scan(
		&tok.ID,
		&tok.Identifier,
		&tok.ValueHash,
		&tok.ExpiresAt,
		&tok.CreatedAt,
	); err != nil {
		return nil, mapError(err)
	}
	toUTC(&tok.ExpiresAt)
	toUTC(&tok.CreatedAt)
	return &tok, nil
}

// CreateVerificationToken implements auth.Store. A zero ID becomes a
// random UUIDv4 and a zero CreatedAt the current time; the CreatedAt
// set by the service is persisted (normalized to UTC at microsecond
// precision, which preserves its order relative to the user's
// CreatedAt). The identifier is stored exactly as given. Reusing an
// (identifier, value hash) pair or an ID returns auth.ErrDuplicate.
func (s *Store) CreateVerificationToken(ctx context.Context, token *auth.VerificationToken) error {
	if token == nil {
		return errNilVerificationToken
	}
	if token.ID == "" {
		token.ID = newID()
	}
	token.CreatedAt = utcOrNow(token.CreatedAt, nowUTC())
	token.ExpiresAt = normalizeTime(token.ExpiresAt)

	return s.execBulk(ctx, s.q.createVerificationToken,
		token.ID,
		token.Identifier,
		token.ValueHash,
		token.ExpiresAt,
		token.CreatedAt,
	)
}

// GetVerificationToken implements auth.Store. Expiry is not checked
// here; the service decides what an expired token means.
func (s *Store) GetVerificationToken(ctx context.Context, identifier, valueHash string) (*auth.VerificationToken, error) {
	return scanVerificationToken(s.pool.QueryRow(ctx, s.q.getVerificationToken, identifier, valueHash))
}

// DeleteVerificationToken implements auth.Store.
func (s *Store) DeleteVerificationToken(ctx context.Context, id string) error {
	return s.execOne(ctx, s.q.deleteVerificationToken, id)
}

// DeleteVerificationTokensByIdentifier implements auth.Store. It
// removes every token whose identifier equals the given one exactly
// (no prefix or pattern matching) and succeeds when nothing matches.
func (s *Store) DeleteVerificationTokensByIdentifier(ctx context.Context, identifier string) error {
	return s.execBulk(ctx, s.q.deleteVerificationTokensByIdentifier, identifier)
}

// DeleteExpiredVerificationTokens implements auth.Store. It removes
// every token whose ExpiresAt lies strictly before the given instant
// and succeeds when nothing matches.
func (s *Store) DeleteExpiredVerificationTokens(ctx context.Context, before time.Time) error {
	return s.execBulk(ctx, s.q.deleteExpiredVerificationTokens, expiryCutoff(before))
}
