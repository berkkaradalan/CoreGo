package mongostore

import (
	"fmt"
)

// DefaultCollectionPrefix is the collection name prefix used when
// Options.CollectionPrefix is empty. It keeps the auth collections
// (corego_users, corego_accounts, ...) apart from the application's own
// collections in the same database.
const DefaultCollectionPrefix = "corego_"

// maxCollectionPrefixLength bounds Options.CollectionPrefix. MongoDB
// limits a namespace ("database.collection") to 255 bytes and a
// database name to 63 bytes; with the longest suffix
// ("verification_tokens", 19 bytes) a 64 byte prefix always fits, and
// no sensible prefix comes close to it.
const maxCollectionPrefixLength = 64

// Unprefixed collection names. The store prepends the configured
// prefix to each of them.
const (
	usersSuffix              = "users"
	accountsSuffix           = "accounts"
	sessionsSuffix           = "sessions"
	verificationTokensSuffix = "verification_tokens"
)

// Options configures a Store.
type Options struct {
	// CollectionPrefix is prepended to every collection name the store
	// uses: {prefix}users, {prefix}accounts, {prefix}sessions and
	// {prefix}verification_tokens. Empty selects DefaultCollectionPrefix
	// ("corego_").
	//
	// Only lowercase ASCII letters, digits and underscores are allowed,
	// at most 64 characters. Use different prefixes to run several
	// independent auth stores in one database (for example one per
	// tenant or per test).
	CollectionPrefix string
}

// collectionNames holds the resolved, prefixed collection names.
type collectionNames struct {
	users              string
	accounts           string
	sessions           string
	verificationTokens string
}

// resolvePrefix applies the default and validates the result.
func resolvePrefix(prefix string) (string, error) {
	if prefix == "" {
		return DefaultCollectionPrefix, nil
	}
	if err := validatePrefix(prefix); err != nil {
		return "", err
	}
	return prefix, nil
}

// validatePrefix reports whether prefix only consists of lowercase
// ASCII letters, digits and underscores and is not too long. The empty
// string is valid (it selects the default before validation matters).
func validatePrefix(prefix string) error {
	if len(prefix) > maxCollectionPrefixLength {
		return fmt.Errorf("mongostore: collection prefix is %d characters long; at most %d are allowed",
			len(prefix), maxCollectionPrefixLength)
	}
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return fmt.Errorf("mongostore: invalid collection prefix %q: only lowercase letters a-z, digits 0-9 and '_' are allowed", prefix)
	}
	return nil
}

// newCollectionNames returns the collection names for a validated
// prefix.
func newCollectionNames(prefix string) collectionNames {
	return collectionNames{
		users:              prefix + usersSuffix,
		accounts:           prefix + accountsSuffix,
		sessions:           prefix + sessionsSuffix,
		verificationTokens: prefix + verificationTokensSuffix,
	}
}
