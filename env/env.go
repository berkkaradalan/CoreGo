// Package env loads application configuration from the process
// environment and an optional .env file.
//
// Load reads ./.env when it exists, merges it with the process
// environment and returns an immutable snapshot:
//
//	e, err := env.Load()
//	if err != nil {
//		log.Fatal(err) // a malformed .env file; a missing one is fine
//	}
//	fmt.Println(e.AppURL, e.Port)
//	workers := e.Int("WORKERS", 4)
//
// The process environment always wins over .env, even when a variable is
// set to the empty string. Loading never modifies the process
// environment (os.Setenv is never called), and later changes to the
// process environment or the .env file do not affect an existing Env.
package env

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultFile is the file Load reads from the current working directory.
const DefaultFile = ".env"

// DefaultPort is the value of Env.Port when PORT is unset or empty.
const DefaultPort = "8080"

// Env is an immutable snapshot of the configuration visible to the
// application: the process environment merged with an optional .env
// file. The typed fields cover the variables CoreGo itself reads; every
// other variable is available through Get and the typed getters.
//
// An Env is safe for concurrent use. The exported fields are plain
// values filled once by Load or LoadFile; Env never mutates them.
type Env struct {
	// AppURL is the public base URL of the application, used to build
	// links such as the ones in verification emails. It comes from
	// APP_URL, falls back to the legacy URL variable, and defaults to
	// "http://localhost:" + Port. Trailing slashes are removed, so
	// AppURL + "/path" is always well formed.
	AppURL string
	// Port is the HTTP port the application listens on (PORT, default
	// "8080").
	Port string
	// MongoURL is the MongoDB connection string from
	// MONGODB_CONNECTION_URL; empty when MongoDB is not configured.
	MongoURL string
	// MongoDatabase is the MongoDB database name from MONGODB_DATABASE;
	// empty when not set.
	MongoDatabase string
	// PostgresURL is the PostgreSQL connection string from
	// POSTGRES_CONNECTION_URL; empty when PostgreSQL is not configured.
	PostgresURL string

	vars map[string]string
}

// Load reads ./.env when it exists, merges it with the process
// environment and returns the resulting snapshot.
//
// A missing .env file is not an error and is not logged: production
// deployments usually configure everything through the process
// environment. A .env file that exists but cannot be read or parsed is
// an error; the error names the file and the offending line but never
// echoes the file's contents, so secrets do not end up in logs.
func Load() (*Env, error) {
	return load(DefaultFile, false)
}

// LoadFile is like Load but reads the given file instead of ./.env. The
// file is required: a missing file is an error that satisfies
// errors.Is(err, fs.ErrNotExist).
func LoadFile(path string) (*Env, error) {
	return load(path, true)
}

// load implements Load and LoadFile. When required is false a missing
// file is treated as an empty one.
func load(path string, required bool) (*Env, error) {
	fileVars, err := readDotenv(path, required)
	if err != nil {
		return nil, err
	}
	return newEnv(merge(processEnv(), fileVars)), nil
}

// processEnv snapshots the process environment. Entries without a name
// (such as the "=C:" pseudo variables on Windows) are skipped.
func processEnv() map[string]string {
	entries := os.Environ()
	vars := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		if key == "" {
			continue
		}
		vars[key] = value
	}
	return vars
}

// merge adds the .env values to the process snapshot without
// overriding it: a variable that is present in the process environment
// wins even when its value is the empty string.
func merge(process, file map[string]string) map[string]string {
	for key, value := range file {
		if _, set := process[key]; !set {
			process[key] = value
		}
	}
	return process
}

// newEnv resolves the typed fields from a merged variable map.
func newEnv(vars map[string]string) *Env {
	e := &Env{vars: vars}
	e.Port = e.GetOrDefault("PORT", DefaultPort)
	e.AppURL = firstNonEmpty(
		trimURL(e.lookup("APP_URL")),
		trimURL(e.lookup("URL")),
		"http://localhost:"+e.Port,
	)
	e.MongoURL = e.lookup("MONGODB_CONNECTION_URL")
	e.MongoDatabase = e.lookup("MONGODB_DATABASE")
	e.PostgresURL = e.lookup("POSTGRES_CONNECTION_URL")
	return e
}

// Get returns the raw value of a variable and whether it is set. A
// variable set to the empty string reports ("", true).
func (e *Env) Get(key string) (string, bool) {
	v, ok := e.vars[key]
	return v, ok
}

// GetOrDefault returns the value of a variable with surrounding
// whitespace removed, or def when the variable is unset, empty or only
// whitespace.
func (e *Env) GetOrDefault(key, def string) string {
	if v := e.lookup(key); v != "" {
		return v
	}
	return def
}

// Int returns the variable parsed as a base-10 integer, or def when it
// is unset, empty or not a valid integer.
func (e *Env) Int(key string, def int) int {
	v := e.lookup(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Bool returns the variable parsed with strconv.ParseBool (1, t, T,
// TRUE, true, True, 0, f, F, FALSE, false, False), or def when it is
// unset, empty or not a valid boolean.
func (e *Env) Bool(key string, def bool) bool {
	v := e.lookup(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// Duration returns the variable parsed with time.ParseDuration (for
// example "30s", "5m", "1h30m"), or def when it is unset, empty or not
// a valid duration. A bare number such as "30" has no unit and is
// therefore invalid; the only exception is "0".
func (e *Env) Duration(key string, def time.Duration) time.Duration {
	v := e.lookup(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// Strings splits a comma separated variable into its elements, trimming
// whitespace around each one and dropping empty elements. It returns
// nil when the variable is unset or contains no elements. The returned
// slice is a fresh copy owned by the caller.
func (e *Env) Strings(key string) []string {
	var out []string
	for _, part := range strings.Split(e.lookup(key), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// lookup returns the value of key with surrounding whitespace removed,
// or "" when the variable is unset.
func (e *Env) lookup(key string) string {
	return strings.TrimSpace(e.vars[key])
}

// trimURL removes trailing slashes from a base URL.
func trimURL(u string) string {
	return strings.TrimRight(u, "/")
}

// firstNonEmpty returns the first non-empty value.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
