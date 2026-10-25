package auth

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// jsonKeys marshals v and returns its top-level keys, sorted, together
// with the raw JSON.
func jsonKeys(t *testing.T, v any) ([]string, string) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%T): %v", v, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, string(raw)
}

func TestModelsJSONShape(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	const (
		secretHash     = "secret-hash-value"
		secretPassword = "$2a$10$bcrypt-hash-value"
	)
	tests := []struct {
		name     string
		value    any
		wantKeys []string
		secrets  []string
	}{
		{
			name: "user",
			value: &User{ID: "u1", Name: "Ada", Email: "ada@example.com", EmailVerified: true, Image: "https://img.example/a.png",
				Banned: true, BanReason: "spam", BanExpires: &expires, CreatedAt: now, UpdatedAt: now, Version: 7},
			wantKeys: []string{"ban_expires", "ban_reason", "banned", "created_at", "email", "email_verified", "id", "image", "name", "updated_at"},
		},
		{
			name:     "user without a ban expiry",
			value:    User{ID: "u1", Version: 3},
			wantKeys: []string{"ban_reason", "banned", "created_at", "email", "email_verified", "id", "image", "name", "updated_at"},
		},
		{
			name: "account",
			value: &Account{ID: "a1", UserID: "u1", Provider: CredentialProvider, ProviderAccountID: "ada@example.com",
				Password: secretPassword, CreatedAt: now, UpdatedAt: now, Version: 9},
			wantKeys: []string{"created_at", "id", "provider", "provider_account_id", "updated_at", "user_id"},
			secrets:  []string{secretPassword, "password"},
		},
		{
			name: "session",
			value: &Session{ID: "s1", UserID: "u1", TokenHash: secretHash, ExpiresAt: expires, IPAddress: "203.0.113.7",
				UserAgent: "agent/1.0", CreatedAt: now, UpdatedAt: now},
			wantKeys: []string{"created_at", "expires_at", "id", "ip_address", "updated_at", "user_agent", "user_id"},
			secrets:  []string{secretHash, "token"},
		},
		{
			name: "verification token",
			value: &VerificationToken{ID: "v1", Identifier: "email-verification:ada@example.com", ValueHash: secretHash,
				ExpiresAt: expires, CreatedAt: now},
			wantKeys: []string{"created_at", "expires_at", "id", "identifier"},
			secrets:  []string{secretHash, "value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, raw := jsonKeys(t, tt.value)
			if !reflect.DeepEqual(keys, tt.wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, tt.wantKeys)
			}
			for _, secret := range append(tt.secrets, "version", "hash") {
				if strings.Contains(strings.ToLower(raw), strings.ToLower(secret)) {
					t.Fatalf("JSON %s leaks %q", raw, secret)
				}
			}
		})
	}
}

func TestModelsJSONRoundTripKeepsPublicFields(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	in := User{ID: "u1", Name: "Ada", Email: "ada@example.com", EmailVerified: true, BanExpires: &now, CreatedAt: now, UpdatedAt: now, Version: 4}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out User
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != 0 {
		t.Fatalf("Version = %d, want it dropped", out.Version)
	}
	out.Version = in.Version
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}

	// Secrets in incoming JSON are ignored too.
	var account Account
	if err := json.Unmarshal([]byte(`{"id":"a1","Password":"x","password":"y","Version":5}`), &account); err != nil {
		t.Fatal(err)
	}
	if account.Password != "" || account.Version != 0 || account.ID != "a1" {
		t.Fatalf("decoded account = %+v", account)
	}
}

// TestEveryModelFieldHasAJSONTag guards against new fields that would
// marshal under their Go name.
func TestEveryModelFieldHasAJSONTag(t *testing.T) {
	for _, model := range []any{User{}, Account{}, Session{}, VerificationToken{}} {
		typ := reflect.TypeOf(model)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if _, ok := field.Tag.Lookup("json"); !ok {
				t.Errorf("%s.%s has no json tag", typ.Name(), field.Name)
			}
		}
	}
}
