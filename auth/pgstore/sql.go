package pgstore

import "strings"

// prefixPlaceholder marks where the validated table prefix goes in the
// SQL templates of this package. Every identifier that contains it is
// double quoted, so a prefix starting with a digit is still a valid
// identifier.
const prefixPlaceholder = "{p}"

// renderSQL substitutes the table prefix into a SQL template. It is
// only ever called with prefixes that passed resolvePrefix, and only
// while a Store is being constructed: no SQL text is assembled at
// query time, and user supplied values always travel as parameters.
func renderSQL(template, prefix string) string {
	return strings.ReplaceAll(template, prefixPlaceholder, prefix)
}

// Column lists shared by the SELECT statements and the scan functions;
// their order must match the scan order exactly.
const (
	userColumns              = "id, name, email, email_verified, image, banned, ban_reason, ban_expires, created_at, updated_at, version"
	accountColumns           = "id, user_id, provider, provider_account_id, password, created_at, updated_at, version"
	sessionColumns           = "id, user_id, token_hash, expires_at, ip_address, user_agent, created_at, updated_at"
	verificationTokenColumns = "id, identifier, value_hash, expires_at, created_at"
)

// queries holds every statement the Store issues, rendered once for the
// store's table prefix.
type queries struct {
	createUser     string
	getUser        string
	getUserByEmail string
	listUsers      string
	countUsers     string
	updateUser     string
	userExists     string
	deleteUser     string

	createAccount      string
	getAccount         string
	getAccountByUser   string
	listAccountsByUser string
	updateAccount      string
	accountExists      string
	deleteAccount      string

	createSession         string
	getSession            string
	getSessionByTokenHash string
	listSessionsByUser    string
	updateSession         string
	deleteSession         string
	deleteSessionsByUser  string
	deleteExpiredSessions string

	createVerificationToken              string
	getVerificationToken                 string
	deleteVerificationToken              string
	deleteVerificationTokensByIdentifier string
	deleteExpiredVerificationTokens      string

	createMigrationsTable  string
	acquireMigrationLock   string
	listAppliedMigrations  string
	recordAppliedMigration string
}

// queryTemplates are the statements of the queries struct before the
// prefix is substituted.
var queryTemplates = queries{
	createUser: `INSERT INTO "{p}users" (` + userColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
	getUser:        `SELECT ` + userColumns + ` FROM "{p}users" WHERE id = $1`,
	getUserByEmail: `SELECT ` + userColumns + ` FROM "{p}users" WHERE email = $1`,
	// Newest first; id is a deterministic tie-breaker for rows sharing
	// a timestamp.
	listUsers:  `SELECT ` + userColumns + ` FROM "{p}users" ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
	countUsers: `SELECT COUNT(*) FROM "{p}users"`,
	// Optimistic concurrency: the row is written only when its version
	// still equals the one the caller read; zero rows returned means the
	// row is missing or was modified concurrently (userExists tells which).
	updateUser: `UPDATE "{p}users"
SET name = $1, email = $2, email_verified = $3, image = $4, banned = $5, ban_reason = $6, ban_expires = $7, updated_at = $8, version = version + 1
WHERE id = $9 AND version = $10
RETURNING version`,
	userExists: `SELECT 1 FROM "{p}users" WHERE id = $1`,
	// Accounts and sessions follow through ON DELETE CASCADE.
	deleteUser: `DELETE FROM "{p}users" WHERE id = $1`,

	createAccount: `INSERT INTO "{p}accounts" (` + accountColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
	getAccount:       `SELECT ` + accountColumns + ` FROM "{p}accounts" WHERE provider = $1 AND provider_account_id = $2`,
	getAccountByUser: `SELECT ` + accountColumns + ` FROM "{p}accounts" WHERE user_id = $1 AND provider = $2`,
	// Oldest first; id is a deterministic tie-breaker.
	listAccountsByUser: `SELECT ` + accountColumns + ` FROM "{p}accounts" WHERE user_id = $1 ORDER BY created_at ASC, id ASC`,
	// Optimistic concurrency like updateUser.
	updateAccount: `UPDATE "{p}accounts"
SET user_id = $1, provider = $2, provider_account_id = $3, password = $4, updated_at = $5, version = version + 1
WHERE id = $6 AND version = $7
RETURNING version`,
	accountExists: `SELECT 1 FROM "{p}accounts" WHERE id = $1`,
	deleteAccount: `DELETE FROM "{p}accounts" WHERE id = $1`,

	createSession: `INSERT INTO "{p}sessions" (` + sessionColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
	getSession:            `SELECT ` + sessionColumns + ` FROM "{p}sessions" WHERE id = $1`,
	getSessionByTokenHash: `SELECT ` + sessionColumns + ` FROM "{p}sessions" WHERE token_hash = $1`,
	// Newest first; id is a deterministic tie-breaker.
	listSessionsByUser: `SELECT ` + sessionColumns + ` FROM "{p}sessions" WHERE user_id = $1 ORDER BY created_at DESC, id DESC`,
	updateSession: `UPDATE "{p}sessions"
SET user_id = $1, token_hash = $2, expires_at = $3, ip_address = $4, user_agent = $5, updated_at = $6
WHERE id = $7`,
	deleteSession:         `DELETE FROM "{p}sessions" WHERE id = $1`,
	deleteSessionsByUser:  `DELETE FROM "{p}sessions" WHERE user_id = $1`,
	deleteExpiredSessions: `DELETE FROM "{p}sessions" WHERE expires_at < $1`,

	createVerificationToken: `INSERT INTO "{p}verification_tokens" (` + verificationTokenColumns + `)
VALUES ($1, $2, $3, $4, $5)`,
	getVerificationToken:                 `SELECT ` + verificationTokenColumns + ` FROM "{p}verification_tokens" WHERE identifier = $1 AND value_hash = $2`,
	deleteVerificationToken:              `DELETE FROM "{p}verification_tokens" WHERE id = $1`,
	deleteVerificationTokensByIdentifier: `DELETE FROM "{p}verification_tokens" WHERE identifier = $1`,
	deleteExpiredVerificationTokens:      `DELETE FROM "{p}verification_tokens" WHERE expires_at < $1`,

	createMigrationsTable: `CREATE TABLE IF NOT EXISTS "{p}schema_migrations" (
  version INTEGER NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT "{p}schema_migrations_pkey" PRIMARY KEY (version)
)`,
	acquireMigrationLock:   `SELECT pg_advisory_xact_lock($1)`,
	listAppliedMigrations:  `SELECT version FROM "{p}schema_migrations" ORDER BY version`,
	recordAppliedMigration: `INSERT INTO "{p}schema_migrations" (version, applied_at) VALUES ($1, $2)`,
}

// render returns a copy of q with the prefix substituted into every
// statement.
func (q queries) render(prefix string) queries {
	for _, field := range q.fields() {
		*field = renderSQL(*field, prefix)
	}
	return q
}

// fields returns pointers to every statement of q, so rendering and the
// tests can treat them uniformly. Keep it in sync with the struct; the
// unit tests fail when a statement is missing.
func (q *queries) fields() []*string {
	return []*string{
		&q.createUser, &q.getUser, &q.getUserByEmail, &q.listUsers, &q.countUsers, &q.updateUser, &q.userExists, &q.deleteUser,
		&q.createAccount, &q.getAccount, &q.getAccountByUser, &q.listAccountsByUser, &q.updateAccount, &q.accountExists, &q.deleteAccount,
		&q.createSession, &q.getSession, &q.getSessionByTokenHash, &q.listSessionsByUser, &q.updateSession,
		&q.deleteSession, &q.deleteSessionsByUser, &q.deleteExpiredSessions,
		&q.createVerificationToken, &q.getVerificationToken, &q.deleteVerificationToken,
		&q.deleteVerificationTokensByIdentifier, &q.deleteExpiredVerificationTokens,
		&q.createMigrationsTable, &q.acquireMigrationLock, &q.listAppliedMigrations, &q.recordAppliedMigration,
	}
}
