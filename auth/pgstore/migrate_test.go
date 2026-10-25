package pgstore

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// releasedMigrationChecksums pins the SHA-256 of every released
// migration's SQL templates. A released migration must never change:
// databases that already applied it would never see the edit. Add a
// new migration (and its checksum here) instead.
//
// The pin starts at the 0.2.0 release: migration 1 was last changed
// before that release (to add the version columns of users and
// accounts) and is frozen from 0.2.0 on.
var releasedMigrationChecksums = map[int]string{
	1: "43d1b9ca847b69a056ff84461e4ee7dad5941f23ee8d20b4adcb14c18f43fd0b",
}

// migrationChecksum hashes the templates of m in order.
func migrationChecksum(m migration) string {
	h := sha256.New()
	for _, stmt := range m.statements {
		h.Write([]byte(stmt))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestMigrationVersionsAreSequential(t *testing.T) {
	if len(migrations) == 0 {
		t.Fatal("no migrations defined")
	}
	for i, m := range migrations {
		if m.version != i+1 {
			t.Fatalf("migration at index %d has version %d, want %d (versions start at 1 and increase by one)", i, m.version, i+1)
		}
		if strings.TrimSpace(m.description) == "" {
			t.Fatalf("migration %d has no description", m.version)
		}
		if len(m.statements) == 0 {
			t.Fatalf("migration %d has no statements", m.version)
		}
		for j, stmt := range m.statements {
			if strings.TrimSpace(stmt) == "" {
				t.Fatalf("migration %d statement %d is empty", m.version, j+1)
			}
			if !strings.Contains(stmt, prefixPlaceholder) {
				t.Fatalf("migration %d statement %d does not use the table prefix: %s", m.version, j+1, stmt)
			}
			if strings.Contains(stmt, ";") {
				t.Fatalf("migration %d statement %d contains ';' - use one statement per entry: %s", m.version, j+1, stmt)
			}
		}
	}
}

func TestMigrationsAreAppendOnly(t *testing.T) {
	for _, m := range migrations {
		want, released := releasedMigrationChecksums[m.version]
		if !released {
			t.Fatalf("migration %d has no pinned checksum; add %q to releasedMigrationChecksums", m.version, migrationChecksum(m))
		}
		if got := migrationChecksum(m); got != want {
			t.Fatalf("released migration %d was modified (checksum %s, pinned %s); add a new migration instead", m.version, got, want)
		}
	}
	if len(releasedMigrationChecksums) > len(migrations) {
		t.Fatalf("%d checksums are pinned but only %d migrations exist: a released migration was removed", len(releasedMigrationChecksums), len(migrations))
	}
}

func TestMigrationOneCreatesSchema(t *testing.T) {
	all := strings.Join(planMigrations("corego_")[0].statements, "\n")
	for _, want := range []string{
		`CREATE TABLE "corego_users"`,
		`CREATE TABLE "corego_accounts"`,
		`CREATE TABLE "corego_sessions"`,
		`CREATE TABLE "corego_verification_tokens"`,
		`REFERENCES "corego_users" (id) ON DELETE CASCADE`,
		`ON "corego_users" (created_at)`,
		`ON "corego_sessions" (user_id)`,
		`ON "corego_sessions" (expires_at)`,
		`ON "corego_verification_tokens" (expires_at)`,
		`UNIQUE (email)`,
		`UNIQUE (provider, provider_account_id)`,
		`UNIQUE (user_id, provider)`,
		`UNIQUE (token_hash)`,
		`UNIQUE (identifier, value_hash)`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("migration 1 lacks %q", want)
		}
	}
	if got := strings.Count(all, "version BIGINT NOT NULL DEFAULT 1,"); got != 2 {
		t.Errorf("migration 1 has %d version columns, want 2 (users and accounts)", got)
	}
	if got := strings.Count(all, "ON DELETE CASCADE"); got != 2 {
		t.Errorf("migration 1 has %d cascading foreign keys, want 2 (accounts and sessions)", got)
	}
}

func TestPlanMigrationsRendersEveryStatement(t *testing.T) {
	planned := planMigrations("app_")
	if len(planned) != len(migrations) {
		t.Fatalf("planned %d migrations, want %d", len(planned), len(migrations))
	}
	for i, p := range planned {
		if p.version != migrations[i].version || p.description != migrations[i].description {
			t.Fatalf("planned migration %d does not match its source", i)
		}
		for _, stmt := range p.statements {
			if strings.Contains(stmt, prefixPlaceholder) {
				t.Fatalf("unrendered statement: %s", stmt)
			}
			for _, m := range quotedIdentifier.FindAllStringSubmatch(stmt, -1) {
				if !strings.HasPrefix(m[1], "app_") {
					t.Fatalf("identifier %q lacks the prefix", m[1])
				}
			}
		}
	}
	// Rendering must not touch the shared templates.
	if !strings.Contains(migrations[0].statements[0], prefixPlaceholder) {
		t.Fatal("planMigrations mutated the migration templates")
	}
}

func TestMigrationLockKey(t *testing.T) {
	if migrationLockKey("corego_") != migrationLockKey("corego_") {
		t.Fatal("lock key is not deterministic")
	}
	prefixes := []string{"corego_", "corego", "app_", "app", "a", "b", "", "1_", "tenant_1_", "tenant_2_"}
	seen := make(map[int64]string, len(prefixes))
	for _, p := range prefixes {
		key := migrationLockKey(p)
		if other, dup := seen[key]; dup {
			t.Fatalf("prefixes %q and %q share lock key %d", p, other, key)
		}
		seen[key] = p
	}
}

// numStringFields counts the string fields of a struct value.
func numStringFields(v any) int {
	rt := reflect.TypeOf(v)
	n := 0
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).Type.Kind() == reflect.String {
			n++
		}
	}
	return n
}
