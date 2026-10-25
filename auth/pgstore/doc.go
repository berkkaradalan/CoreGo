// Package pgstore implements auth.Store on PostgreSQL using the pgx
// driver and its connection pool.
//
// # Setup
//
// Wrap a connected pool - for example the one of a database.PostgresDB -
// in a Store, run the migrations once at startup and hand the store to
// the auth service:
//
//	db, err := database.NewPostgresDB(database.PostgresConfig{URL: url})
//	if err != nil {
//		return err
//	}
//	defer db.Close()
//
//	store, err := pgstore.NewStore(db.Pool(), pgstore.Options{})
//	if err != nil {
//		return err
//	}
//	if err := store.Migrate(ctx); err != nil {
//		return err
//	}
//	service, err := auth.New(auth.Config{}, store)
//
// The corego facade does all of this automatically when PostgreSQL is
// the selected auth store.
//
// # Tables
//
// Every table name starts with Options.TablePrefix (default "corego_"),
// so the store never collides with the application's own tables:
// corego_users, corego_accounts, corego_sessions,
// corego_verification_tokens and corego_schema_migrations. Constraint
// and index names carry the same prefix. Accounts and sessions
// reference users with ON DELETE CASCADE foreign keys; verification
// tokens are keyed by a purpose-prefixed email address and have no
// foreign key.
//
// # Migrations
//
// Migrate is a small versioned migration runner. The applied versions
// are recorded in {prefix}schema_migrations; pending migrations run in
// order inside one transaction guarded by a transaction-scoped
// advisory lock derived from the prefix, so it is safe to call from
// many application instances at once and idempotent on every start.
// Upgrading the package and calling Migrate again applies whatever new
// versions the release brings.
//
// # Behavior notes
//
//   - All SQL is built once in NewStore from the validated prefix; user
//     supplied values always travel as query parameters.
//   - PostgreSQL errors are mapped to the auth sentinel errors:
//     pgx.ErrNoRows becomes auth.ErrNotFound and SQLSTATE 23505 (unique
//     violation) becomes auth.ErrDuplicate. Every other error, such as
//     a canceled context or a foreign key violation, is returned
//     unchanged.
//   - Create methods populate ID (a random UUIDv4), CreatedAt and
//     UpdatedAt when the caller left them zero and keep caller supplied
//     values otherwise; update methods replace the full row and set
//     UpdatedAt to the current time.
//   - Users and accounts carry an optimistic concurrency counter (the
//     version column, auth.User.Version and auth.Account.Version).
//     Create sets it to 1; UpdateUser and UpdateAccount write only when
//     the stored version equals the passed one and then increment it,
//     returning auth.ErrConflict on a mismatch and auth.ErrNotFound for
//     a missing row.
//   - Timestamps are normalized to UTC at microsecond precision (the
//     resolution of timestamptz) on the passed structs, so they equal
//     what is stored and read back. Sub-microsecond digits are
//     truncated, which never changes the order of two instants.
//   - Values are stored verbatim: emails and token identifiers are
//     matched exactly (the auth service normalizes them first).
//   - List methods return empty, non-nil slices when nothing matches.
package pgstore
