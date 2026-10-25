package mongostore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestIntegrationUsers(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	t.Run("create stamps zero fields and keeps caller values", func(t *testing.T) {
		explicit := &auth.User{ID: "user-explicit", Name: "Explicit", Email: "explicit@example.com",
			CreatedAt: msTime(testBase, 1), UpdatedAt: msTime(testBase, 2)}
		if err := store.CreateUser(ctx, explicit); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if explicit.ID != "user-explicit" || !explicit.CreatedAt.Equal(msTime(testBase, 1)) || !explicit.UpdatedAt.Equal(msTime(testBase, 2)) {
			t.Fatalf("caller values were changed: %+v", explicit)
		}

		stamped := &auth.User{Name: "Stamped", Email: "stamped@example.com"}
		if err := store.CreateUser(ctx, stamped); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if len(stamped.ID) != 32 || stamped.CreatedAt.IsZero() || stamped.UpdatedAt.IsZero() {
			t.Fatalf("zero fields were not stamped: %+v", stamped)
		}
		got, err := store.GetUser(ctx, stamped.ID)
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		// Generated timestamps are millisecond precise, so the struct
		// written by Create matches the stored record exactly.
		if *got != *stamped {
			t.Fatalf("stored user differs from the created struct:\n got %+v\nwant %+v", *got, *stamped)
		}
	})

	t.Run("get by id and email", func(t *testing.T) {
		got, err := store.GetUserByEmail(ctx, "explicit@example.com")
		if err != nil || got.ID != "user-explicit" {
			t.Fatalf("GetUserByEmail = %+v, %v", got, err)
		}
		if _, err := store.GetUser(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("GetUser(missing) error = %v, want ErrNotFound", err)
		}
		if _, err := store.GetUserByEmail(ctx, "missing@example.com"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("GetUserByEmail(missing) error = %v, want ErrNotFound", err)
		}
		// Lookups are exact; the service normalizes addresses.
		if _, err := store.GetUserByEmail(ctx, "EXPLICIT@example.com"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("GetUserByEmail(other case) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("duplicates", func(t *testing.T) {
		dupEmail := &auth.User{Name: "Dup", Email: "explicit@example.com"}
		if err := store.CreateUser(ctx, dupEmail); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("CreateUser(duplicate email) error = %v, want ErrDuplicate", err)
		}
		dupID := &auth.User{ID: "user-explicit", Name: "Dup", Email: "other@example.com"}
		if err := store.CreateUser(ctx, dupID); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("CreateUser(duplicate id) error = %v, want ErrDuplicate", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		user, err := store.GetUser(ctx, "user-explicit")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		banExpires := time.Now().Add(time.Hour)
		user.Name = "Renamed"
		user.EmailVerified = true
		user.Banned = true
		user.BanReason = "spam"
		user.BanExpires = &banExpires
		before := time.Now().Add(-time.Second)
		if err := store.UpdateUser(ctx, user); err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if user.UpdatedAt.Before(before) {
			t.Fatalf("UpdatedAt = %v, want it bumped to now", user.UpdatedAt)
		}
		got, err := store.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if got.Name != "Renamed" || !got.EmailVerified || !got.Banned || got.BanReason != "spam" ||
			got.BanExpires == nil || !got.BanExpires.Equal(banExpires.Truncate(time.Millisecond)) {
			t.Fatalf("update not persisted: %+v", got)
		}
		if !got.UpdatedAt.Equal(user.UpdatedAt) || !got.CreatedAt.Equal(msTime(testBase, 1)) {
			t.Fatalf("timestamps after update: created %v updated %v (struct %v)", got.CreatedAt, got.UpdatedAt, user.UpdatedAt)
		}

		// Clearing the ban expiry persists nil.
		got.BanExpires = nil
		if err := store.UpdateUser(ctx, got); err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if again, _ := store.GetUser(ctx, got.ID); again.BanExpires != nil {
			t.Fatalf("BanExpires = %v, want nil", again.BanExpires)
		}

		if err := store.UpdateUser(ctx, &auth.User{ID: "missing", Email: "missing@example.com"}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("UpdateUser(missing) error = %v, want ErrNotFound", err)
		}
		got.Email = "stamped@example.com"
		if err := store.UpdateUser(ctx, got); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("UpdateUser(taken email) error = %v, want ErrDuplicate", err)
		}
	})

	t.Run("delete missing user", func(t *testing.T) {
		if err := store.DeleteUser(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("DeleteUser(missing) error = %v, want ErrNotFound", err)
		}
	})
}

func TestIntegrationListAndCountUsers(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	empty, err := store.ListUsers(ctx, auth.ListOptions{})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ListUsers(empty store) = %v, %v; want an empty non-nil slice", empty, err)
	}
	if n, err := store.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers(empty store) = %d, %v", n, err)
	}

	// u0 is the oldest; u3a and u3b share a creation instant, so the
	// ID decides (descending, like MemoryStore).
	specs := []struct {
		id string
		ms int
	}{{"u0", 0}, {"u1", 10}, {"u2", 20}, {"u3a", 30}, {"u3b", 30}, {"u4", 40}}
	for _, s := range specs {
		u := &auth.User{ID: s.id, Name: s.id, Email: s.id + "@example.com", CreatedAt: msTime(testBase, s.ms)}
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser(%s): %v", s.id, err)
		}
	}

	tests := []struct {
		name string
		opts auth.ListOptions
		want []string
	}{
		{name: "default page", opts: auth.ListOptions{}, want: []string{"u4", "u3b", "u3a", "u2", "u1", "u0"}},
		{name: "first page", opts: auth.ListOptions{Limit: 2}, want: []string{"u4", "u3b"}},
		{name: "second page", opts: auth.ListOptions{Limit: 2, Offset: 2}, want: []string{"u3a", "u2"}},
		{name: "last partial page", opts: auth.ListOptions{Limit: 4, Offset: 4}, want: []string{"u1", "u0"}},
		{name: "offset past the end", opts: auth.ListOptions{Limit: 10, Offset: 100}, want: []string{}},
		{name: "negative values are normalized", opts: auth.ListOptions{Limit: -1, Offset: -5}, want: []string{"u4", "u3b", "u3a", "u2", "u1", "u0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, err := store.ListUsers(ctx, tt.opts)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if users == nil {
				t.Fatal("ListUsers returned a nil slice")
			}
			got := make([]string, 0, len(users))
			for _, u := range users {
				got = append(got, u.ID)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("ListUsers(%+v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}

	if n, err := store.CountUsers(ctx); err != nil || n != int64(len(specs)) {
		t.Fatalf("CountUsers = %d, %v; want %d", n, err, len(specs))
	}
}

func TestIntegrationAccounts(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	cred := &auth.Account{ID: "acc-b", UserID: "user-1", Provider: "credential", ProviderAccountID: "ada@example.com",
		Password: "hash-1", CreatedAt: msTime(testBase, 10), UpdatedAt: msTime(testBase, 10)}
	github := &auth.Account{ID: "acc-a", UserID: "user-1", Provider: "github", ProviderAccountID: "gh-1",
		CreatedAt: msTime(testBase, 10)}
	google := &auth.Account{UserID: "user-1", Provider: "google", ProviderAccountID: "g-1", CreatedAt: msTime(testBase, 5)}
	other := &auth.Account{UserID: "user-2", Provider: "credential", ProviderAccountID: "bob@example.com", Password: "hash-2"}
	for _, a := range []*auth.Account{cred, github, google, other} {
		if err := store.CreateAccount(ctx, a); err != nil {
			t.Fatalf("CreateAccount(%s): %v", a.ProviderAccountID, err)
		}
		if len(a.ID) == 0 || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
			t.Fatalf("account not stamped: %+v", a)
		}
	}

	got, err := store.GetAccount(ctx, "credential", "ada@example.com")
	if err != nil || *got != *cred {
		t.Fatalf("GetAccount = %+v, %v; want %+v", got, err, cred)
	}
	got, err = store.GetAccountByUser(ctx, "user-1", "github")
	if err != nil || got.ID != "acc-a" {
		t.Fatalf("GetAccountByUser = %+v, %v", got, err)
	}
	if _, err := store.GetAccount(ctx, "credential", "nobody@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("GetAccount(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetAccountByUser(ctx, "user-2", "github"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("GetAccountByUser(missing) error = %v, want ErrNotFound", err)
	}

	duplicates := []struct {
		name    string
		account *auth.Account
	}{
		{"same provider account for another user", &auth.Account{UserID: "user-3", Provider: "github", ProviderAccountID: "gh-1"}},
		{"second account of one provider for a user", &auth.Account{UserID: "user-1", Provider: "github", ProviderAccountID: "gh-2"}},
		{"same id", &auth.Account{ID: "acc-a", UserID: "user-9", Provider: "x", ProviderAccountID: "y"}},
	}
	for _, d := range duplicates {
		if err := store.CreateAccount(ctx, d.account); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("CreateAccount(%s) error = %v, want ErrDuplicate", d.name, err)
		}
	}

	// Oldest first; equal instants ordered by ID ascending.
	list, err := store.ListAccountsByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListAccountsByUser: %v", err)
	}
	var order []string
	for _, a := range list {
		order = append(order, a.Provider)
	}
	if fmt.Sprint(order) != "[google github credential]" {
		t.Fatalf("ListAccountsByUser order = %v, want [google github credential]", order)
	}
	none, err := store.ListAccountsByUser(ctx, "user-none")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("ListAccountsByUser(none) = %v, %v; want an empty non-nil slice", none, err)
	}

	cred.Password = "hash-rotated"
	cred.ProviderAccountID = "ada.new@example.com"
	if err := store.UpdateAccount(ctx, cred); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if got, err := store.GetAccount(ctx, "credential", "ada.new@example.com"); err != nil || got.Password != "hash-rotated" || !got.UpdatedAt.Equal(cred.UpdatedAt) {
		t.Fatalf("GetAccount after update = %+v, %v", got, err)
	}
	cred.ProviderAccountID = "bob@example.com"
	if err := store.UpdateAccount(ctx, cred); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("UpdateAccount(taken pair) error = %v, want ErrDuplicate", err)
	}
	if err := store.UpdateAccount(ctx, &auth.Account{ID: "missing"}); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UpdateAccount(missing) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteAccount(ctx, "acc-a"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if err := store.DeleteAccount(ctx, "acc-a"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("DeleteAccount(again) error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationSessions(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)

	s1 := &auth.Session{ID: "s1", UserID: "user-1", TokenHash: "hash-1", ExpiresAt: future,
		IPAddress: "192.0.2.1", UserAgent: "agent", CreatedAt: msTime(testBase, 10), UpdatedAt: msTime(testBase, 10)}
	s2a := &auth.Session{ID: "s2a", UserID: "user-1", TokenHash: "hash-2a", ExpiresAt: future, CreatedAt: msTime(testBase, 20)}
	s2b := &auth.Session{ID: "s2b", UserID: "user-1", TokenHash: "hash-2b", ExpiresAt: future, CreatedAt: msTime(testBase, 20)}
	s3 := &auth.Session{UserID: "user-2", TokenHash: "hash-3", ExpiresAt: future}
	for _, s := range []*auth.Session{s1, s2a, s2b, s3} {
		if err := store.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession(%s): %v", s.TokenHash, err)
		}
	}
	if len(s3.ID) != 32 || s3.CreatedAt.IsZero() || s3.UpdatedAt.IsZero() {
		t.Fatalf("session not stamped: %+v", s3)
	}

	if got, err := store.GetSession(ctx, "s1"); err != nil || *got != *s1 {
		t.Fatalf("GetSession = %+v, %v; want %+v", got, err, s1)
	}
	if got, err := store.GetSessionByTokenHash(ctx, "hash-3"); err != nil || *got != *s3 {
		t.Fatalf("GetSessionByTokenHash = %+v, %v; want %+v", got, err, s3)
	}
	if _, err := store.GetSession(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("GetSession(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("GetSessionByTokenHash(missing) error = %v, want ErrNotFound", err)
	}
	if err := store.CreateSession(ctx, &auth.Session{UserID: "user-9", TokenHash: "hash-1", ExpiresAt: future}); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("CreateSession(duplicate token hash) error = %v, want ErrDuplicate", err)
	}
	if err := store.CreateSession(ctx, &auth.Session{ID: "s1", UserID: "user-9", TokenHash: "hash-new", ExpiresAt: future}); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("CreateSession(duplicate id) error = %v, want ErrDuplicate", err)
	}

	// Newest first; equal instants ordered by ID descending.
	list, err := store.ListSessionsByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	var ids []string
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	if fmt.Sprint(ids) != "[s2b s2a s1]" {
		t.Fatalf("ListSessionsByUser = %v, want [s2b s2a s1]", ids)
	}
	if none, err := store.ListSessionsByUser(ctx, "user-none"); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("ListSessionsByUser(none) = %v, %v; want an empty non-nil slice", none, err)
	}

	s1.ExpiresAt = future.Add(time.Hour)
	if err := store.UpdateSession(ctx, s1); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if got, _ := store.GetSession(ctx, "s1"); !got.ExpiresAt.Equal(s1.ExpiresAt) || !got.UpdatedAt.Equal(s1.UpdatedAt) {
		t.Fatalf("UpdateSession not persisted: %+v", got)
	}
	s1.TokenHash = "hash-3"
	if err := store.UpdateSession(ctx, s1); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("UpdateSession(taken token hash) error = %v, want ErrDuplicate", err)
	}
	if err := store.UpdateSession(ctx, &auth.Session{ID: "missing", TokenHash: "x"}); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UpdateSession(missing) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteSession(ctx, "s2a"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if err := store.DeleteSession(ctx, "s2a"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("DeleteSession(again) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteSessionsByUser(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteSessionsByUser: %v", err)
	}
	if left, _ := store.ListSessionsByUser(ctx, "user-1"); len(left) != 0 {
		t.Fatalf("DeleteSessionsByUser left %d sessions", len(left))
	}
	if _, err := store.GetSession(ctx, s3.ID); err != nil {
		t.Fatalf("DeleteSessionsByUser removed another user's session: %v", err)
	}
	if err := store.DeleteSessionsByUser(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteSessionsByUser(no sessions) error = %v, want nil", err)
	}
}

func TestIntegrationDeleteExpiredSessions(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	cutoff := msTime(testBase, 1000)
	sessions := map[string]time.Time{
		"long-gone":      msTime(testBase, 0),
		"one-ms-earlier": msTime(testBase, 999),
		"at-cutoff":      cutoff,
		"later":          msTime(testBase, 1001),
	}
	for id, expires := range sessions {
		if err := store.CreateSession(ctx, &auth.Session{ID: id, UserID: "u", TokenHash: "h-" + id, ExpiresAt: expires}); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}

	// "Expired" is strictly before the bound: a session expiring exactly
	// at the cutoff survives.
	if err := store.DeleteExpiredSessions(ctx, cutoff); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	assertSessions(t, store, map[string]bool{"long-gone": false, "one-ms-earlier": false, "at-cutoff": true, "later": true})

	// A bound with sub-millisecond digits still deletes a session whose
	// stored expiry is strictly before it.
	if err := store.DeleteExpiredSessions(ctx, cutoff.Add(500*time.Microsecond)); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	assertSessions(t, store, map[string]bool{"at-cutoff": false, "later": true})

	// Zero matches is not an error.
	if err := store.DeleteExpiredSessions(ctx, testBase); err != nil {
		t.Fatalf("DeleteExpiredSessions(no matches) error = %v, want nil", err)
	}
	assertSessions(t, store, map[string]bool{"later": true})
}

// assertSessions checks which session IDs exist.
func assertSessions(t *testing.T, store *Store, want map[string]bool) {
	t.Helper()
	for id, exists := range want {
		_, err := store.GetSession(context.Background(), id)
		switch {
		case exists && err != nil:
			t.Fatalf("session %q: %v, want it to exist", id, err)
		case !exists && !errors.Is(err, auth.ErrNotFound):
			t.Fatalf("session %q: error %v, want ErrNotFound", id, err)
		}
	}
}

func TestIntegrationVerificationTokens(t *testing.T) {
	store, db := newIntegrationStore(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)

	verifyID := "email-verification:ada@example.com"
	resetID := "password-reset:ada@example.com"

	explicit := &auth.VerificationToken{ID: "tok-1", Identifier: verifyID, ValueHash: "v1",
		ExpiresAt: future, CreatedAt: msTime(testBase, 7)}
	stamped := &auth.VerificationToken{Identifier: verifyID, ValueHash: "v2", ExpiresAt: future}
	reset := &auth.VerificationToken{Identifier: resetID, ValueHash: "v1", ExpiresAt: future}
	bare := &auth.VerificationToken{Identifier: "ada@example.com", ValueHash: "v3", ExpiresAt: future}
	for _, tok := range []*auth.VerificationToken{explicit, stamped, reset, bare} {
		if err := store.CreateVerificationToken(ctx, tok); err != nil {
			t.Fatalf("CreateVerificationToken(%s/%s): %v", tok.Identifier, tok.ValueHash, err)
		}
	}
	if len(stamped.ID) != 32 || stamped.CreatedAt.IsZero() {
		t.Fatalf("token not stamped: %+v", stamped)
	}

	// CreatedAt is persisted and returned faithfully.
	if got, err := store.GetVerificationToken(ctx, verifyID, "v1"); err != nil || *got != *explicit {
		t.Fatalf("GetVerificationToken = %+v, %v; want %+v", got, err, explicit)
	}
	if got, err := store.GetVerificationToken(ctx, verifyID, "v2"); err != nil || *got != *stamped {
		t.Fatalf("GetVerificationToken(stamped) = %+v, %v; want %+v", got, err, stamped)
	}
	// The same hash under another identifier is a different token.
	if got, err := store.GetVerificationToken(ctx, resetID, "v1"); err != nil || got.ID != reset.ID {
		t.Fatalf("GetVerificationToken(reset) = %+v, %v", got, err)
	}
	for _, miss := range [][2]string{{verifyID, "nope"}, {"email-verification:other@example.com", "v1"}, {"", ""}} {
		if _, err := store.GetVerificationToken(ctx, miss[0], miss[1]); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("GetVerificationToken(%q, %q) error = %v, want ErrNotFound", miss[0], miss[1], err)
		}
	}

	if err := store.CreateVerificationToken(ctx, &auth.VerificationToken{Identifier: verifyID, ValueHash: "v1", ExpiresAt: future}); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("CreateVerificationToken(duplicate pair) error = %v, want ErrDuplicate", err)
	}
	if err := store.CreateVerificationToken(ctx, &auth.VerificationToken{ID: "tok-1", Identifier: "x", ValueHash: "y", ExpiresAt: future}); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("CreateVerificationToken(duplicate id) error = %v, want ErrDuplicate", err)
	}

	t.Run("delete by identifier matches exactly", func(t *testing.T) {
		if err := store.DeleteVerificationTokensByIdentifier(ctx, verifyID); err != nil {
			t.Fatalf("DeleteVerificationTokensByIdentifier: %v", err)
		}
		if n := countDocs(t, db, "corego_verification_tokens", bson.D{{Key: "identifier", Value: verifyID}}); n != 0 {
			t.Fatalf("%d tokens left for %q", n, verifyID)
		}
		// Neither the other purpose nor a bare address with the same
		// suffix is touched.
		if _, err := store.GetVerificationToken(ctx, resetID, "v1"); err != nil {
			t.Fatalf("reset token removed: %v", err)
		}
		if _, err := store.GetVerificationToken(ctx, "ada@example.com", "v3"); err != nil {
			t.Fatalf("bare identifier token removed: %v", err)
		}
		if err := store.DeleteVerificationTokensByIdentifier(ctx, verifyID); err != nil {
			t.Fatalf("DeleteVerificationTokensByIdentifier(no matches) error = %v, want nil", err)
		}
		if err := store.DeleteVerificationTokensByIdentifier(ctx, "email-verification:"); err != nil {
			t.Fatalf("DeleteVerificationTokensByIdentifier(prefix only): %v", err)
		}
		if _, err := store.GetVerificationToken(ctx, resetID, "v1"); err != nil {
			t.Fatalf("a bare purpose prefix must not match as a prefix: %v", err)
		}
	})

	t.Run("delete by id", func(t *testing.T) {
		if err := store.DeleteVerificationToken(ctx, reset.ID); err != nil {
			t.Fatalf("DeleteVerificationToken: %v", err)
		}
		if err := store.DeleteVerificationToken(ctx, reset.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("DeleteVerificationToken(again) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("delete expired is strict", func(t *testing.T) {
		cutoff := msTime(testBase, 5000)
		for id, expires := range map[string]time.Time{
			"exp-before": msTime(testBase, 4999),
			"exp-at":     cutoff,
			"exp-after":  msTime(testBase, 5001),
		} {
			if err := store.CreateVerificationToken(ctx, &auth.VerificationToken{ID: id, Identifier: "expiry", ValueHash: id, ExpiresAt: expires}); err != nil {
				t.Fatalf("CreateVerificationToken(%s): %v", id, err)
			}
		}
		if err := store.DeleteExpiredVerificationTokens(ctx, cutoff); err != nil {
			t.Fatalf("DeleteExpiredVerificationTokens: %v", err)
		}
		for hash, exists := range map[string]bool{"exp-before": false, "exp-at": true, "exp-after": true} {
			_, err := store.GetVerificationToken(ctx, "expiry", hash)
			if exists != (err == nil) {
				t.Fatalf("token %s: error %v, want exists=%v", hash, err, exists)
			}
		}
		if err := store.DeleteExpiredVerificationTokens(ctx, cutoff.Add(time.Nanosecond)); err != nil {
			t.Fatalf("DeleteExpiredVerificationTokens: %v", err)
		}
		if _, err := store.GetVerificationToken(ctx, "expiry", "exp-at"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("token expiring 1ns before the bound survived: %v", err)
		}
		if _, err := store.GetVerificationToken(ctx, "ada@example.com", "v3"); err != nil {
			t.Fatalf("unexpired token removed: %v", err)
		}
	})
}

func TestIntegrationDeleteUserCascade(t *testing.T) {
	store, db := newIntegrationStore(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour)

	for _, id := range []string{"victim", "bystander"} {
		email := id + "@example.com"
		if err := store.CreateUser(ctx, &auth.User{ID: id, Name: id, Email: email}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		for _, provider := range []string{"credential", "github"} {
			if err := store.CreateAccount(ctx, &auth.Account{UserID: id, Provider: provider, ProviderAccountID: provider + "-" + id}); err != nil {
				t.Fatalf("CreateAccount: %v", err)
			}
		}
		for i := 0; i < 3; i++ {
			if err := store.CreateSession(ctx, &auth.Session{UserID: id, TokenHash: fmt.Sprintf("%s-%d", id, i), ExpiresAt: future}); err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
		}
		if err := store.CreateVerificationToken(ctx, &auth.VerificationToken{Identifier: "email-verification:" + email, ValueHash: id, ExpiresAt: future}); err != nil {
			t.Fatalf("CreateVerificationToken: %v", err)
		}
	}

	if err := store.DeleteUser(ctx, "victim"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUser(ctx, "victim"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("GetUser(deleted) error = %v, want ErrNotFound", err)
	}
	owned := bson.D{{Key: "user_id", Value: "victim"}}
	if n := countDocs(t, db, "corego_accounts", owned); n != 0 {
		t.Fatalf("cascade left %d accounts", n)
	}
	if n := countDocs(t, db, "corego_sessions", owned); n != 0 {
		t.Fatalf("cascade left %d sessions", n)
	}
	// Verification tokens are the service's responsibility.
	if _, err := store.GetVerificationToken(ctx, "email-verification:victim@example.com", "victim"); err != nil {
		t.Fatalf("DeleteUser removed a verification token: %v", err)
	}
	// The other user is untouched.
	others := bson.D{{Key: "user_id", Value: "bystander"}}
	if n := countDocs(t, db, "corego_accounts", others); n != 2 {
		t.Fatalf("bystander has %d accounts, want 2", n)
	}
	if n := countDocs(t, db, "corego_sessions", others); n != 3 {
		t.Fatalf("bystander has %d sessions, want 3", n)
	}

	// Deleting a missing user reports ErrNotFound and touches nothing,
	// even when orphaned records reference the ID.
	if err := store.CreateSession(ctx, &auth.Session{UserID: "ghost", TokenHash: "ghost", ExpiresAt: future}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.DeleteUser(ctx, "ghost"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("DeleteUser(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, "ghost"); err != nil {
		t.Fatalf("DeleteUser(missing) touched other collections: %v", err)
	}
	if err := store.DeleteUser(ctx, "victim"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("DeleteUser(again) error = %v, want ErrNotFound", err)
	}
}

// TestIntegrationTimestampPrecision checks the millisecond storage
// precision end to end: caller supplied nanosecond, non-UTC values
// read back floored to the millisecond in UTC, and the service's token
// age check works for a token created in the same millisecond as its
// user.
func TestIntegrationTimestampPrecision(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	zone := time.FixedZone("UTC-5", -5*60*60)
	created := time.Date(2026, 3, 4, 5, 6, 7, 891_234_567, zone)
	banExpires := time.Date(2030, 1, 1, 0, 0, 0, 999_999_999, zone)
	user := &auth.User{Name: "Precise", Email: "precise@example.com", CreatedAt: created, UpdatedAt: created, BanExpires: &banExpires}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.CreatedAt != created {
		t.Fatal("CreateUser modified a caller supplied CreatedAt")
	}
	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	wantCreated := created.UTC().Truncate(time.Millisecond)
	if got.CreatedAt != wantCreated || got.UpdatedAt != wantCreated {
		t.Fatalf("CreatedAt/UpdatedAt = %#v/%#v, want %#v", got.CreatedAt, got.UpdatedAt, wantCreated)
	}
	if want := banExpires.UTC().Truncate(time.Millisecond); *got.BanExpires != want {
		t.Fatalf("BanExpires = %#v, want %#v", *got.BanExpires, want)
	}
}

// TestIntegrationTokenCreatedInSameMillisecondAsUser is the regression
// test for timestamp truncation: the auth service rejects a token whose
// CreatedAt is before its user's CreatedAt. A token issued right after
// signup, in the same millisecond but a few hundred microseconds later,
// must never be rejected after the round trip through MongoDB.
func TestIntegrationTokenCreatedInSameMillisecondAsUser(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()
	svc, err := auth.New(auth.Config{}, store)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	millisecond := time.Now().Add(-time.Minute).UTC().Truncate(time.Millisecond)
	tests := []struct {
		name         string
		userCreated  time.Time
		tokenCreated time.Time
		wantErr      error
	}{
		{name: "token later in the same millisecond", userCreated: millisecond.Add(100 * time.Microsecond),
			tokenCreated: millisecond.Add(900 * time.Microsecond), wantErr: nil},
		{name: "token at the identical instant", userCreated: millisecond.Add(999_999 * time.Nanosecond),
			tokenCreated: millisecond.Add(999_999 * time.Nanosecond), wantErr: nil},
		{name: "token in the next millisecond", userCreated: millisecond.Add(999 * time.Microsecond),
			tokenCreated: millisecond.Add(time.Millisecond), wantErr: nil},
		{name: "token from a previous owner one millisecond earlier", userCreated: millisecond.Add(100 * time.Microsecond),
			tokenCreated: millisecond.Add(-time.Microsecond), wantErr: auth.ErrInvalidToken},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := fmt.Sprintf("same-ms-%d@example.com", i)
			user := &auth.User{Name: "Same", Email: email, CreatedAt: tt.userCreated, UpdatedAt: tt.userCreated}
			if err := store.CreateUser(ctx, user); err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			raw, err := auth.NewToken()
			if err != nil {
				t.Fatalf("NewToken: %v", err)
			}
			token := &auth.VerificationToken{
				Identifier: "email-verification:" + email,
				ValueHash:  auth.HashToken(raw),
				ExpiresAt:  time.Now().Add(time.Hour),
				CreatedAt:  tt.tokenCreated,
			}
			if err := store.CreateVerificationToken(ctx, token); err != nil {
				t.Fatalf("CreateVerificationToken: %v", err)
			}

			storedUser, _ := store.GetUser(ctx, user.ID)
			storedToken, _ := store.GetVerificationToken(ctx, token.Identifier, token.ValueHash)
			if tt.wantErr == nil && storedToken.CreatedAt.Before(storedUser.CreatedAt) {
				t.Fatalf("stored token CreatedAt %v is before stored user CreatedAt %v", storedToken.CreatedAt, storedUser.CreatedAt)
			}

			err = svc.VerifyEmail(ctx, email, raw)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyEmail error = %v, want %v", err, tt.wantErr)
			}
			verified, _ := store.GetUser(ctx, user.ID)
			if verified.EmailVerified != (tt.wantErr == nil) {
				t.Fatalf("EmailVerified = %v after VerifyEmail error %v", verified.EmailVerified, err)
			}
		})
	}
}

func TestIntegrationConcurrentCreateUser(t *testing.T) {
	store, _ := newIntegrationStore(t)
	ctx := context.Background()

	const workers = 16
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		ok, dup    int
		unexpected []error
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := store.CreateUser(ctx, &auth.User{Name: fmt.Sprint(i), Email: "race@example.com"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, auth.ErrDuplicate):
				dup++
			default:
				unexpected = append(unexpected, err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 || dup != workers-1 || len(unexpected) != 0 {
		t.Fatalf("concurrent CreateUser: %d ok, %d duplicates, unexpected errors %v; want 1 ok and %d duplicates",
			ok, dup, unexpected, workers-1)
	}
}

func TestIntegrationContextHandling(t *testing.T) {
	store, _ := newIntegrationStore(t)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GetUser(canceled, "any"); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUser(canceled ctx) error = %v, want context.Canceled", err)
	}
	if err := store.CreateUser(canceled, &auth.User{Email: "ctx@example.com"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateUser(canceled ctx) error = %v, want context.Canceled", err)
	}
	if err := store.DeleteExpiredSessions(canceled, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteExpiredSessions(canceled ctx) error = %v, want context.Canceled", err)
	}
	if err := store.EnsureIndexes(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureIndexes(canceled ctx) error = %v, want context.Canceled", err)
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if _, err := store.CountUsers(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CountUsers(expired ctx) error = %v, want context.DeadlineExceeded", err)
	}
}
