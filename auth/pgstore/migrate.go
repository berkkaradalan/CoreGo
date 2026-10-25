package pgstore

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
)

// migration is one versioned schema change. Its statements are SQL
// templates containing prefixPlaceholder.
type migration struct {
	version     int
	description string
	statements  []string
}

// migrations is the complete, ordered schema history of the store.
//
// APPEND ONLY. A released migration is never edited, reordered or
// removed: databases in the wild have already applied it and would
// never see the change. Schema changes ship as a new entry with the
// next version number. TestMigrationsAreAppendOnly pins the rendered
// SQL of every released migration and fails when one changes.
var migrations = []migration{
	{
		version:     1,
		description: "create users, accounts, sessions and verification_tokens",
		statements: []string{
			`CREATE TABLE "{p}users" (
  id TEXT NOT NULL,
  name TEXT NOT NULL,
  email TEXT NOT NULL,
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  image TEXT NOT NULL DEFAULT '',
  banned BOOLEAN NOT NULL DEFAULT FALSE,
  ban_reason TEXT NOT NULL DEFAULT '',
  ban_expires TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  CONSTRAINT "{p}users_pkey" PRIMARY KEY (id),
  CONSTRAINT "{p}users_email_key" UNIQUE (email)
)`,
			// Supports ListUsers (newest first).
			`CREATE INDEX "{p}users_created_at_idx" ON "{p}users" (created_at)`,

			// The (user_id, provider) unique index leads with user_id,
			// so it also serves ListAccountsByUser and the cascade from
			// users; a separate user_id index would be redundant.
			`CREATE TABLE "{p}accounts" (
  id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  provider_account_id TEXT NOT NULL,
  password TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  CONSTRAINT "{p}accounts_pkey" PRIMARY KEY (id),
  CONSTRAINT "{p}accounts_user_id_fkey" FOREIGN KEY (user_id) REFERENCES "{p}users" (id) ON DELETE CASCADE,
  CONSTRAINT "{p}accounts_provider_account_key" UNIQUE (provider, provider_account_id),
  CONSTRAINT "{p}accounts_user_provider_key" UNIQUE (user_id, provider)
)`,

			`CREATE TABLE "{p}sessions" (
  id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT "{p}sessions_pkey" PRIMARY KEY (id),
  CONSTRAINT "{p}sessions_user_id_fkey" FOREIGN KEY (user_id) REFERENCES "{p}users" (id) ON DELETE CASCADE,
  CONSTRAINT "{p}sessions_token_hash_key" UNIQUE (token_hash)
)`,
			// Supports ListSessionsByUser, DeleteSessionsByUser and the
			// cascade from users.
			`CREATE INDEX "{p}sessions_user_id_idx" ON "{p}sessions" (user_id)`,
			// Supports DeleteExpiredSessions.
			`CREATE INDEX "{p}sessions_expires_at_idx" ON "{p}sessions" (expires_at)`,

			// No foreign key: the identifier is a purpose-prefixed email
			// address, not a user id. The (identifier, value_hash)
			// unique index also serves
			// DeleteVerificationTokensByIdentifier.
			`CREATE TABLE "{p}verification_tokens" (
  id TEXT NOT NULL,
  identifier TEXT NOT NULL,
  value_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT "{p}verification_tokens_pkey" PRIMARY KEY (id),
  CONSTRAINT "{p}verification_tokens_lookup_key" UNIQUE (identifier, value_hash)
)`,
			// Supports DeleteExpiredVerificationTokens.
			`CREATE INDEX "{p}verification_tokens_expires_at_idx" ON "{p}verification_tokens" (expires_at)`,
		},
	},
}

// plannedMigration is a migration rendered for one table prefix.
type plannedMigration struct {
	version     int
	description string
	statements  []string
}

// planMigrations renders every migration for prefix.
func planMigrations(prefix string) []plannedMigration {
	planned := make([]plannedMigration, len(migrations))
	for i, m := range migrations {
		statements := make([]string, len(m.statements))
		for j, stmt := range m.statements {
			statements[j] = renderSQL(stmt, prefix)
		}
		planned[i] = plannedMigration{version: m.version, description: m.description, statements: statements}
	}
	return planned
}

// migrationLockNamespace scopes the advisory lock keys of this package
// so they are unlikely to collide with advisory locks the application
// takes for its own purposes.
const migrationLockNamespace = "github.com/berkkaradalan/CoreGo/auth/pgstore:migrate:"

// migrationLockKey derives the pg_advisory_xact_lock key for a table
// prefix: the 64-bit FNV-1a hash of the namespaced prefix. It is
// deterministic, so every application instance using the same prefix
// contends for the same lock, while stores with different prefixes
// migrate independently.
func migrationLockKey(prefix string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(migrationLockNamespace + prefix))
	return int64(h.Sum64())
}

// Migrate brings the store's tables up to date. Call it once at startup
// before the store is used; it is idempotent, so running it on every
// start is the intended usage.
//
// Applied versions are recorded in the {prefix}schema_migrations table.
// Migrate runs in a single transaction that first takes a
// transaction-scoped advisory lock derived from the table prefix
// (pg_advisory_xact_lock), so concurrent callers - for example several
// application instances starting at once - are serialized: one applies
// the pending migrations, the others wait and then find nothing left to
// do. All pending migrations are applied in version order; when any
// statement fails the whole transaction is rolled back and the database
// is left exactly as it was.
//
// Versions recorded in the database that this release does not know
// (written by a newer release) are left alone.
func (s *Store) Migrate(ctx context.Context) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: migrate: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			// Roll back even when ctx is already canceled, so the
			// connection does not return to the pool mid-transaction.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if _, err := tx.Exec(ctx, s.q.acquireMigrationLock, s.lockKey); err != nil {
		return fmt.Errorf("pgstore: migrate: acquire advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, s.q.createMigrationsTable); err != nil {
		return fmt.Errorf("pgstore: migrate: create schema_migrations table: %w", err)
	}
	applied, err := appliedVersions(ctx, tx, s.q.listAppliedMigrations)
	if err != nil {
		return err
	}

	for _, m := range s.migrations {
		if applied[m.version] {
			continue
		}
		for i, stmt := range m.statements {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("pgstore: migrate: version %d (%s), statement %d: %w", m.version, m.description, i+1, err)
			}
		}
		if _, err := tx.Exec(ctx, s.q.recordAppliedMigration, m.version, nowUTC()); err != nil {
			return fmt.Errorf("pgstore: migrate: record version %d: %w", m.version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgstore: migrate: commit: %w", err)
	}
	return nil
}

// appliedVersions returns the set of migration versions already
// recorded in the schema_migrations table.
func appliedVersions(ctx context.Context, tx pgx.Tx, query string) (map[int]bool, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("pgstore: migrate: read applied versions: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int32])
	if err != nil {
		return nil, fmt.Errorf("pgstore: migrate: read applied versions: %w", err)
	}
	applied := make(map[int]bool, len(versions))
	for _, v := range versions {
		applied[int(v)] = true
	}
	return applied, nil
}
