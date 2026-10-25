package pgstore

import (
	"errors"
	"fmt"
)

// DefaultTablePrefix is the table name prefix used when
// Options.TablePrefix is empty. With it the store creates the tables
// corego_users, corego_accounts, corego_sessions,
// corego_verification_tokens and corego_schema_migrations.
const DefaultTablePrefix = "corego_"

// MaxTablePrefixLength is the longest accepted Options.TablePrefix in
// bytes. PostgreSQL silently truncates identifiers longer than 63
// bytes; the cap guarantees that every table, constraint and index
// name the store derives from the prefix fits, so two different
// prefixes can never collapse into the same truncated name.
const MaxTablePrefixLength = 24

// Errors returned by NewStore. Compare with errors.Is.
var (
	// ErrNilPool is returned by NewStore when the pool is nil.
	ErrNilPool = errors.New("pgstore: pool must not be nil")

	// ErrInvalidTablePrefix is returned by NewStore when
	// Options.TablePrefix contains characters other than lowercase
	// ASCII letters, digits and underscores, or is longer than
	// MaxTablePrefixLength.
	ErrInvalidTablePrefix = errors.New("pgstore: invalid table prefix")
)

// Options configures a Store. The zero value is ready to use.
type Options struct {
	// TablePrefix is prepended verbatim to every table name, so
	// "myapp_" yields myapp_users, myapp_accounts and so on. It keeps
	// the auth tables apart from the application's own tables (an
	// existing "users" table is never touched) and lets several
	// independent stores share one database. Empty selects
	// DefaultTablePrefix ("corego_").
	//
	// Only lowercase ASCII letters, digits and underscores are allowed,
	// at most MaxTablePrefixLength bytes. The prefix becomes part of SQL
	// identifiers, so this validation is what keeps the generated SQL
	// safe; NewStore rejects anything else with ErrInvalidTablePrefix.
	TablePrefix string
}

// resolvePrefix returns the effective table prefix for opts, or an
// error wrapping ErrInvalidTablePrefix.
func resolvePrefix(opts Options) (string, error) {
	prefix := opts.TablePrefix
	if prefix == "" {
		return DefaultTablePrefix, nil
	}
	if len(prefix) > MaxTablePrefixLength {
		return "", fmt.Errorf("%w: %d bytes, the maximum is %d", ErrInvalidTablePrefix, len(prefix), MaxTablePrefixLength)
	}
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return "", fmt.Errorf("%w: %q may only contain lowercase letters (a-z), digits (0-9) and underscores", ErrInvalidTablePrefix, prefix)
		}
	}
	return prefix, nil
}
