package database

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DefaultMongoDatabase is the database name used when neither
// MongoConfig.Database nor the connection URL names one.
const DefaultMongoDatabase = "corego"

// MongoConfig configures a MongoDB connection.
type MongoConfig struct {
	// URL is the MongoDB connection string, for example
	// "mongodb://localhost:27017/myapp" or
	// "mongodb+srv://user:pass@cluster.example.net/myapp?retryWrites=true".
	// It is required.
	URL string

	// Database names the database the helpers operate on. When empty, the
	// database in the URL path is used when present, otherwise
	// DefaultMongoDatabase ("corego").
	Database string
}

// PostgresConfig configures a PostgreSQL connection.
type PostgresConfig struct {
	// URL is the PostgreSQL connection string, for example
	// "postgres://user:pass@localhost:5432/myapp?sslmode=disable". The
	// database is part of the URL. It is required; an empty URL is
	// rejected instead of silently falling back to libpq environment
	// defaults.
	URL string
}

// errEmptyMongoURL and errEmptyPostgresURL are returned by the
// constructors for a missing connection string.
var (
	errEmptyMongoURL    = errors.New("database: MongoConfig.URL is empty")
	errEmptyPostgresURL = errors.New("database: PostgresConfig.URL is empty")
)

// resolveMongoDatabase picks the database name for cfg: the explicit
// Database field, then the database in the URL path, then
// DefaultMongoDatabase. The result is validated.
func resolveMongoDatabase(cfg MongoConfig) (string, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return "", errEmptyMongoURL
	}
	fromURL, err := mongoURLDatabase(cfg.URL)
	if err != nil {
		return "", err
	}

	name := cfg.Database
	if name == "" {
		name = fromURL
	}
	if name == "" {
		name = DefaultMongoDatabase
	}
	if err := validateMongoDatabaseName(name); err != nil {
		return "", err
	}
	return name, nil
}

// mongoURLDatabase extracts the (percent-decoded) database name from a
// mongodb:// or mongodb+srv:// connection string. It returns "" when the
// URL has no database path. Error messages never include the URL,
// because it usually carries credentials.
//
// The connection string format is
//
//	scheme://[user[:password]@]host[:port][,host[:port]...][/[database]][?options]
//
// where user and password must percent-encode '@', ':' and '/'. The
// first '/' after the host list therefore starts the database path, and
// the first '?' after it starts the options.
func mongoURLDatabase(raw string) (string, error) {
	var rest string
	switch {
	case strings.HasPrefix(raw, "mongodb://"):
		rest = strings.TrimPrefix(raw, "mongodb://")
	case strings.HasPrefix(raw, "mongodb+srv://"):
		rest = strings.TrimPrefix(raw, "mongodb+srv://")
	default:
		return "", errors.New(`database: invalid MongoDB URL: scheme must be "mongodb://" or "mongodb+srv://"`)
	}

	// Drop the credentials. The driver rejects an unescaped '@' anywhere
	// else, so splitting at the first one is unambiguous for every URL
	// the driver accepts.
	if _, afterUser, ok := strings.Cut(rest, "@"); ok {
		rest = afterUser
	}

	// The host list ends at the first '/' or '?'. Without a '/' there is
	// no database path.
	end := strings.IndexAny(rest, "/?")
	if end == -1 || rest[end] == '?' {
		return "", nil
	}

	path := rest[end+1:]
	if before, _, ok := strings.Cut(path, "?"); ok {
		path = before
	}
	if path == "" {
		return "", nil
	}
	// Decode like the driver does (query unescaping, so '+' is a space),
	// keeping this name identical to the one the driver derives its
	// default authSource from.
	name, err := url.QueryUnescape(path)
	if err != nil {
		return "", errors.New("database: invalid MongoDB URL: malformed percent-encoding in the database name")
	}
	return name, nil
}

// mongoForbiddenDatabaseChars lists the characters MongoDB rejects in
// database names on every platform, plus NUL.
const mongoForbiddenDatabaseChars = "/\\. \"$\x00"

// mongoMaxDatabaseNameBytes is MongoDB's database name length limit.
const mongoMaxDatabaseNameBytes = 63

// validateMongoDatabaseName rejects names MongoDB would refuse, so a
// typo surfaces at startup instead of on the first query.
func validateMongoDatabaseName(name string) error {
	if name == "" {
		return errors.New("database: MongoDB database name is empty")
	}
	if len(name) > mongoMaxDatabaseNameBytes {
		return fmt.Errorf("database: MongoDB database name %q is longer than %d bytes", name, mongoMaxDatabaseNameBytes)
	}
	if i := strings.IndexAny(name, mongoForbiddenDatabaseChars); i != -1 {
		return fmt.Errorf("database: MongoDB database name %q contains the forbidden character %q", name, name[i])
	}
	return nil
}
