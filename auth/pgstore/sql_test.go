package pgstore

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// quotedIdentifier matches a double quoted SQL identifier.
var quotedIdentifier = regexp.MustCompile(`"([^"]*)"`)

// postgresMaxIdentifierLength is NAMEDATALEN-1, the longest identifier
// PostgreSQL keeps without truncating it.
const postgresMaxIdentifierLength = 63

// tableNames are the tables the store owns, without prefix.
var tableNames = []string{"users", "accounts", "sessions", "verification_tokens", "schema_migrations"}

// assertRenderedFor checks that every statement of q was rendered for
// prefix: no placeholder is left, every quoted identifier carries the
// prefix and every table reference is quoted.
func assertRenderedFor(t *testing.T, q queries, prefix string) {
	t.Helper()
	for i, stmt := range q.fields() {
		sql := *stmt
		if sql == "" {
			t.Fatalf("statement %d is empty", i)
		}
		if strings.Contains(sql, prefixPlaceholder) {
			t.Fatalf("statement %d still contains the placeholder: %s", i, sql)
		}
		if sql == q.acquireMigrationLock {
			// The only statement that references no table.
			continue
		}
		identifiers := quotedIdentifier.FindAllStringSubmatch(sql, -1)
		if len(identifiers) == 0 {
			t.Fatalf("statement %d references no quoted table: %s", i, sql)
		}
		for _, m := range identifiers {
			if !strings.HasPrefix(m[1], prefix) {
				t.Fatalf("statement %d: identifier %q lacks prefix %q: %s", i, m[1], prefix, sql)
			}
		}
	}
}

func TestQueriesAreRenderedWithPrefix(t *testing.T) {
	for _, prefix := range []string{"corego_", "myapp_", "x", "9_"} {
		t.Run(prefix, func(t *testing.T) {
			q := queryTemplates.render(prefix)
			assertRenderedFor(t, q, prefix)

			// Every table is referenced as "{prefix}{table}".
			all := ""
			for _, stmt := range q.fields() {
				all += *stmt + "\n"
			}
			for _, table := range tableNames {
				if !strings.Contains(all, `"`+prefix+table+`"`) {
					t.Fatalf("no statement references %q", prefix+table)
				}
			}
		})
	}
}

func TestQueryTemplatesAreUnchangedByRender(t *testing.T) {
	before := queryTemplates.createUser
	_ = queryTemplates.render("other_")
	if queryTemplates.createUser != before {
		t.Fatal("render mutated the shared templates")
	}
	if !strings.Contains(queryTemplates.createUser, prefixPlaceholder) {
		t.Fatal("templates must contain the placeholder")
	}
}

// TestQueryFieldsCoverStruct makes sure fields() lists every statement
// exactly once: all pointers distinct and the count equal to the
// number of struct fields.
func TestQueryFieldsCoverStruct(t *testing.T) {
	var q queries
	fields := q.fields()
	seen := make(map[*string]bool, len(fields))
	for _, f := range fields {
		if seen[f] {
			t.Fatal("fields() lists a statement twice")
		}
		seen[f] = true
	}
	if want := numStringFields(q); len(fields) != want {
		t.Fatalf("fields() lists %d statements, the struct has %d", len(fields), want)
	}
}

func TestStatementsUseParametersForValues(t *testing.T) {
	q := queryTemplates.render(DefaultTablePrefix)
	// Statements taking user input must reference parameters; none may
	// contain a string literal (values are never formatted into SQL).
	for i, stmt := range q.fields() {
		if strings.Contains(*stmt, "'") {
			t.Fatalf("statement %d contains a string literal: %s", i, *stmt)
		}
	}
	withParams := []string{
		q.createUser, q.getUser, q.getUserByEmail, q.listUsers, q.updateUser, q.userExists, q.deleteUser,
		q.createAccount, q.getAccount, q.getAccountByUser, q.listAccountsByUser, q.updateAccount, q.accountExists, q.deleteAccount,
		q.createSession, q.getSession, q.getSessionByTokenHash, q.listSessionsByUser, q.updateSession,
		q.deleteSession, q.deleteSessionsByUser, q.deleteExpiredSessions,
		q.createVerificationToken, q.getVerificationToken, q.deleteVerificationToken,
		q.deleteVerificationTokensByIdentifier, q.deleteExpiredVerificationTokens,
		q.acquireMigrationLock, q.recordAppliedMigration,
	}
	for _, sql := range withParams {
		if !strings.Contains(sql, "$1") {
			t.Fatalf("statement takes no parameters: %s", sql)
		}
	}
}

func TestIdentifiersFitPostgresLimit(t *testing.T) {
	prefix := strings.Repeat("z", MaxTablePrefixLength)
	var sql []string
	rendered := queryTemplates.render(prefix)
	for _, stmt := range rendered.fields() {
		sql = append(sql, *stmt)
	}
	for _, m := range planMigrations(prefix) {
		sql = append(sql, m.statements...)
	}
	for _, stmt := range sql {
		for _, m := range quotedIdentifier.FindAllStringSubmatch(stmt, -1) {
			if len(m[1]) > postgresMaxIdentifierLength {
				t.Fatalf("identifier %q is %d bytes, PostgreSQL truncates beyond %d", m[1], len(m[1]), postgresMaxIdentifierLength)
			}
		}
	}
}

// TestVersionedUpdates pins the optimistic concurrency shape of the
// user and account updates: guarded by id and version, incrementing the
// version in the same statement and returning the new value.
func TestVersionedUpdates(t *testing.T) {
	q := queryTemplates.render(DefaultTablePrefix)
	tests := []struct {
		name, sql, guard string
	}{
		{"updateUser", q.updateUser, "WHERE id = $9 AND version = $10"},
		{"updateAccount", q.updateAccount, "WHERE id = $6 AND version = $7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range []string{"version = version + 1", tt.guard, "RETURNING version"} {
				if !strings.Contains(tt.sql, want) {
					t.Fatalf("%s lacks %q: %s", tt.name, want, tt.sql)
				}
			}
		})
	}
	for _, sql := range []string{q.userExists, q.accountExists} {
		if !strings.HasPrefix(sql, "SELECT 1 FROM ") || !strings.HasSuffix(sql, "WHERE id = $1") {
			t.Fatalf("existence check has an unexpected shape: %s", sql)
		}
	}
}

// TestColumnListsIncludeVersion makes sure the version travels through
// inserts and selects of users and accounts.
func TestColumnListsIncludeVersion(t *testing.T) {
	for name, columns := range map[string]string{"users": userColumns, "accounts": accountColumns} {
		if !strings.HasSuffix(columns, ", version") {
			t.Fatalf("%s column list does not end with version: %s", name, columns)
		}
	}
}

// TestInsertPlaceholdersMatchColumns checks that every INSERT binds
// exactly one parameter per listed column, $1 through $n.
func TestInsertPlaceholdersMatchColumns(t *testing.T) {
	q := queryTemplates
	tests := []struct {
		name, sql, columns string
	}{
		{"createUser", q.createUser, userColumns},
		{"createAccount", q.createAccount, accountColumns},
		{"createSession", q.createSession, sessionColumns},
		{"createVerificationToken", q.createVerificationToken, verificationTokenColumns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := len(strings.Split(tt.columns, ","))
			params := make([]string, n)
			for i := range params {
				params[i] = fmt.Sprintf("$%d", i+1)
			}
			want := "VALUES (" + strings.Join(params, ", ") + ")"
			if !strings.HasSuffix(tt.sql, want) {
				t.Fatalf("%s does not end with %q: %s", tt.name, want, tt.sql)
			}
		})
	}
}
