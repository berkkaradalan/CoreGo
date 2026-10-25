package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// memStoreNow is a fixed reference time used for explicit timestamps.
var memStoreNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestMemoryStoreUserLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created := &User{Email: "alice@example.com", Name: "Alice"}
	if err := store.CreateUser(ctx, created); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == "" {
		t.Error("CreateUser did not populate ID")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("CreateUser did not populate timestamps")
	}

	got, err := store.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != "alice@example.com" || got.Name != "Alice" {
		t.Errorf("GetUser returned %+v", got)
	}

	byEmail, err := store.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if byEmail.ID != created.ID {
		t.Errorf("GetUserByEmail ID = %q, want %q", byEmail.ID, created.ID)
	}

	got.Name = "Alice Updated"
	if err := store.UpdateUser(ctx, got); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, err := store.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if updated.Name != "Alice Updated" {
		t.Errorf("Name = %q, want %q", updated.Name, "Alice Updated")
	}
	if updated.UpdatedAt.Before(got.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, older than the pre-update %v", updated.UpdatedAt, got.UpdatedAt)
	}

	if err := store.DeleteUser(ctx, created.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUser(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser after delete error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetUserByEmail(ctx, "alice@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByEmail after delete error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteUser(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteUser error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreUserDuplicateEmail(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.CreateUser(ctx, &User{Email: "dup@example.com"}); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	err := store.CreateUser(ctx, &User{Email: "dup@example.com"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second CreateUser error = %v, want ErrDuplicate", err)
	}
}

func TestMemoryStoreUserDuplicateID(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	first := &User{ID: "fixed-id", Email: "one@example.com"}
	if err := store.CreateUser(ctx, first); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	err := store.CreateUser(ctx, &User{ID: "fixed-id", Email: "two@example.com"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second CreateUser error = %v, want ErrDuplicate", err)
	}
}

func TestMemoryStoreUpdateUserEmailReindex(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "old@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateUser(ctx, &User{Email: "other@example.com"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user.Email = "new@example.com"
	if err := store.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := store.GetUserByEmail(ctx, "new@example.com"); err != nil {
		t.Errorf("GetUserByEmail(new): %v", err)
	}
	if _, err := store.GetUserByEmail(ctx, "old@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByEmail(old) error = %v, want ErrNotFound", err)
	}

	// Changing to an email owned by another user must fail.
	reloaded, _ := store.GetUserByEmail(ctx, "new@example.com")
	reloaded.Email = "other@example.com"
	if err := store.UpdateUser(ctx, reloaded); !errors.Is(err, ErrDuplicate) {
		t.Errorf("UpdateUser to taken email error = %v, want ErrDuplicate", err)
	}
}

func TestMemoryStoreGetReturnsCopies(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "copy@example.com", Name: "Original"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	first, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	banTime := memStoreNow.Add(24 * time.Hour)
	first.Name = "Mutated"
	first.Email = "mutated@example.com"
	first.Banned = true
	first.BanExpires = &banTime

	second, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if second.Name != "Original" || second.Email != "copy@example.com" {
		t.Errorf("store state mutated through returned copy: %+v", second)
	}
	if second.Banned || second.BanExpires != nil {
		t.Errorf("ban fields mutated through returned copy: %+v", second)
	}

	byEmail, err := store.GetUserByEmail(ctx, "copy@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	byEmail.Name = "Mutated Again"
	if again, _ := store.GetUser(ctx, user.ID); again.Name != "Original" {
		t.Error("GetUserByEmail result is not a copy")
	}
}

func TestMemoryStoreUpdateCopiesInput(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "input@example.com", Name: "Before"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Mutating the caller's struct after create must not leak in.
	user.Name = "AfterCreate"
	if got, _ := store.GetUser(ctx, user.ID); got.Name != "Before" {
		t.Errorf("CreateUser stored the caller's pointer: Name = %q", got.Name)
	}

	user.Name = "AfterUpdate"
	if err := store.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	user.Name = "MutatedAfterUpdate"
	if got, _ := store.GetUser(ctx, user.ID); got.Name != "AfterUpdate" {
		t.Errorf("UpdateUser stored the caller's pointer: Name = %q", got.Name)
	}
}

func TestMemoryStoreAccountUniqueness(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "owner@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	base := &Account{UserID: user.ID, Provider: "credential", ProviderAccountID: "owner@example.com", Password: "hash"}
	if err := store.CreateAccount(ctx, cloneAccount(base)); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Same provider+providerAccountID.
	sameProvider := cloneAccount(base)
	if err := store.CreateAccount(ctx, sameProvider); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate provider+accountID error = %v, want ErrDuplicate", err)
	}

	// Same user and provider with a different provider account id.
	sameUser := cloneAccount(base)
	sameUser.ProviderAccountID = "different@example.com"
	if err := store.CreateAccount(ctx, sameUser); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate userID+provider error = %v, want ErrDuplicate", err)
	}

	// A second provider for the same user is fine.
	oauth := cloneAccount(base)
	oauth.Provider = "google"
	oauth.ProviderAccountID = "google-id-1"
	oauth.Password = ""
	if err := store.CreateAccount(ctx, oauth); err != nil {
		t.Errorf("second provider rejected: %v", err)
	}
}

func TestMemoryStoreAccountLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "acct@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	account := &Account{UserID: user.ID, Provider: "credential", ProviderAccountID: "acct@example.com", Password: "hash"}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if account.ID == "" || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
		t.Error("CreateAccount did not populate ID/timestamps")
	}

	got, err := store.GetAccount(ctx, "credential", "acct@example.com")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("GetAccount UserID = %q", got.UserID)
	}

	byUser, err := store.GetAccountByUser(ctx, user.ID, "credential")
	if err != nil {
		t.Fatalf("GetAccountByUser: %v", err)
	}
	if byUser.ID != account.ID {
		t.Errorf("GetAccountByUser ID = %q, want %q", byUser.ID, account.ID)
	}

	// Updating provider keys must reindex.
	got.Provider = "google"
	got.ProviderAccountID = "google-id"
	if err := store.UpdateAccount(ctx, got); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if _, err := store.GetAccount(ctx, "google", "google-id"); err != nil {
		t.Errorf("GetAccount(new keys): %v", err)
	}
	if _, err := store.GetAccount(ctx, "credential", "acct@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccount(old keys) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteAccount(ctx, account.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := store.GetAccount(ctx, "google", "google-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccount after delete error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteAccount(ctx, account.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteAccount error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreUpdateAccountDuplicateTarget(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "a@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	first := &Account{UserID: user.ID, Provider: "credential", ProviderAccountID: "first"}
	second := &Account{UserID: user.ID, Provider: "google", ProviderAccountID: "second"}
	if err := store.CreateAccount(ctx, first); err != nil {
		t.Fatalf("CreateAccount(first): %v", err)
	}
	if err := store.CreateAccount(ctx, second); err != nil {
		t.Fatalf("CreateAccount(second): %v", err)
	}

	second.Provider = "credential" // collides with first on provider+id
	if err := store.UpdateAccount(ctx, second); !errors.Is(err, ErrDuplicate) {
		t.Errorf("UpdateAccount collision error = %v, want ErrDuplicate", err)
	}

	missing := &Account{ID: "no-such-id", UserID: user.ID, Provider: "x", ProviderAccountID: "y"}
	if err := store.UpdateAccount(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAccount(missing) error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreListAccountsOrder(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "multi@example.com"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	providers := []struct {
		name      string
		createdAt time.Time
	}{
		{"google", memStoreNow.Add(2 * time.Hour)},
		{"credential", memStoreNow.Add(1 * time.Hour)},
		{"facebook", memStoreNow.Add(3 * time.Hour)},
	}
	for _, p := range providers {
		account := &Account{
			UserID:            user.ID,
			Provider:          p.name,
			ProviderAccountID: p.name + "-id",
			CreatedAt:         p.createdAt,
			UpdatedAt:         p.createdAt,
		}
		if err := store.CreateAccount(ctx, account); err != nil {
			t.Fatalf("CreateAccount(%s): %v", p.name, err)
		}
	}

	accounts, err := store.ListAccountsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAccountsByUser: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("len(accounts) = %d, want 3", len(accounts))
	}
	wantOrder := []string{"credential", "google", "facebook"} // oldest first
	for i, want := range wantOrder {
		if accounts[i].Provider != want {
			t.Errorf("accounts[%d].Provider = %q, want %q", i, accounts[i].Provider, want)
		}
	}

	// Other users' accounts must not leak in.
	other := &User{Email: "other@example.com"}
	if err := store.CreateUser(ctx, other); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	otherAccounts, err := store.ListAccountsByUser(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListAccountsByUser: %v", err)
	}
	if len(otherAccounts) != 0 {
		t.Errorf("len(otherAccounts) = %d, want 0", len(otherAccounts))
	}
	if otherAccounts == nil {
		t.Error("ListAccountsByUser returned a nil slice, want empty non-nil")
	}
}

func TestMemoryStoreSessionLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &Session{UserID: "user-1", TokenHash: HashToken("token-one")}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == "" || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Error("CreateSession did not populate ID/timestamps")
	}

	byToken, err := store.GetSessionByTokenHash(ctx, HashToken("token-one"))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if byToken.ID != session.ID {
		t.Errorf("GetSessionByTokenHash ID = %q, want %q", byToken.ID, session.ID)
	}

	duplicate := &Session{UserID: "user-2", TokenHash: HashToken("token-one")}
	if err := store.CreateSession(ctx, duplicate); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate token hash error = %v, want ErrDuplicate", err)
	}

	// Rotating the token hash must reindex.
	byToken.TokenHash = HashToken("token-two")
	if err := store.UpdateSession(ctx, byToken); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken("token-two")); err != nil {
		t.Errorf("GetSessionByTokenHash(new): %v", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken("token-one")); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSessionByTokenHash(old) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken("token-two")); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSessionByTokenHash after delete error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteSession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteSession error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreSessionNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if _, err := store.GetSession(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSessionByTokenHash error = %v, want ErrNotFound", err)
	}
	if err := store.UpdateSession(ctx, &Session{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSession error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteSession(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteSession error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreDeleteSessionsByUser(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		session := &Session{UserID: "user-1", TokenHash: HashToken(fmt.Sprintf("t-%d", i))}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	keep := &Session{UserID: "user-2", TokenHash: HashToken("t-other")}
	if err := store.CreateSession(ctx, keep); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := store.DeleteSessionsByUser(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteSessionsByUser: %v", err)
	}
	sessions, err := store.ListSessionsByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	if len(sessions) != 0 || sessions == nil {
		t.Errorf("sessions = %v, want empty non-nil", sessions)
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken("t-other")); err != nil {
		t.Error("DeleteSessionsByUser removed another user's session")
	}

	// Deleting for a user without sessions succeeds.
	if err := store.DeleteSessionsByUser(ctx, "nobody"); err != nil {
		t.Errorf("DeleteSessionsByUser(no sessions) = %v, want nil", err)
	}
}

func TestMemoryStoreListSessionsOrder(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	times := []time.Time{
		memStoreNow.Add(time.Hour),
		memStoreNow.Add(3 * time.Hour),
		memStoreNow.Add(2 * time.Hour),
	}
	for i, at := range times {
		session := &Session{
			UserID:    "user-1",
			TokenHash: HashToken(fmt.Sprintf("ordered-%d", i)),
			CreatedAt: at,
			UpdatedAt: at,
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	sessions, err := store.ListSessionsByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(sessions))
	}
	for i := 1; i < len(sessions); i++ {
		if sessions[i].CreatedAt.After(sessions[i-1].CreatedAt) {
			t.Errorf("sessions not sorted newest first: [%d] %v > [%d] %v",
				i, sessions[i].CreatedAt, i-1, sessions[i-1].CreatedAt)
		}
	}
}

func TestMemoryStoreVerificationTokens(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	token := &VerificationToken{
		Identifier: "user@example.com",
		ValueHash:  HashToken("tok-1"),
		ExpiresAt:  memStoreNow.Add(time.Hour),
	}
	if err := store.CreateVerificationToken(ctx, token); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	if token.ID == "" || token.CreatedAt.IsZero() {
		t.Error("CreateVerificationToken did not populate ID/CreatedAt")
	}
	if !token.ExpiresAt.Equal(memStoreNow.Add(time.Hour)) {
		t.Errorf("CreateVerificationToken modified ExpiresAt: %v", token.ExpiresAt)
	}

	got, err := store.GetVerificationToken(ctx, "user@example.com", HashToken("tok-1"))
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	if got.ID != token.ID {
		t.Errorf("GetVerificationToken ID = %q, want %q", got.ID, token.ID)
	}

	if _, err := store.GetVerificationToken(ctx, "user@example.com", HashToken("tok-2")); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVerificationToken(missing) error = %v, want ErrNotFound", err)
	}

	duplicate := &VerificationToken{Identifier: "user@example.com", ValueHash: HashToken("tok-1")}
	if err := store.CreateVerificationToken(ctx, duplicate); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate identifier+valueHash error = %v, want ErrDuplicate", err)
	}

	if err := store.DeleteVerificationToken(ctx, token.ID); err != nil {
		t.Fatalf("DeleteVerificationToken: %v", err)
	}
	if _, err := store.GetVerificationToken(ctx, "user@example.com", HashToken("tok-1")); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVerificationToken after delete error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteVerificationToken(ctx, token.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteVerificationToken error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreDeleteExpiredVerificationTokens(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	cutoff := memStoreNow

	tokens := []struct {
		name  string
		value string
		exp   time.Time
		kept  bool
	}{
		{"expired", "expired-tok", cutoff.Add(-time.Second), false},
		{"exactly at cutoff", "boundary-tok", cutoff, true}, // strictly before is removed
		{"fresh", "fresh-tok", cutoff.Add(time.Hour), true},
	}
	for _, tt := range tokens {
		token := &VerificationToken{
			Identifier: "user@example.com",
			ValueHash:  HashToken(tt.value),
			ExpiresAt:  tt.exp,
		}
		if err := store.CreateVerificationToken(ctx, token); err != nil {
			t.Fatalf("CreateVerificationToken(%s): %v", tt.name, err)
		}
	}

	if err := store.DeleteExpiredVerificationTokens(ctx, cutoff); err != nil {
		t.Fatalf("DeleteExpiredVerificationTokens: %v", err)
	}
	for _, tt := range tokens {
		_, err := store.GetVerificationToken(ctx, "user@example.com", HashToken(tt.value))
		if tt.kept && err != nil {
			t.Errorf("%s: token should have been kept: %v", tt.name, err)
		}
		if !tt.kept && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: token should have been removed, error = %v", tt.name, err)
		}
	}
}

func TestMemoryStoreDeleteVerificationTokensByIdentifier(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	seed := []struct {
		identifier string
		value      string
	}{
		{"password-reset:user@example.com", "reset-1"},
		{"password-reset:user@example.com", "reset-2"},
		{"email-verification:user@example.com", "verify-1"},
		{"password-reset:other@example.com", "other-1"},
		{"password-reset:user@example.com.evil", "suffix-1"},
	}
	for _, tt := range seed {
		token := &VerificationToken{Identifier: tt.identifier, ValueHash: HashToken(tt.value), ExpiresAt: memStoreNow.Add(time.Hour)}
		if err := store.CreateVerificationToken(ctx, token); err != nil {
			t.Fatalf("CreateVerificationToken(%s): %v", tt.value, err)
		}
	}

	if err := store.DeleteVerificationTokensByIdentifier(ctx, "password-reset:user@example.com"); err != nil {
		t.Fatalf("DeleteVerificationTokensByIdentifier: %v", err)
	}
	for _, tt := range seed {
		_, err := store.GetVerificationToken(ctx, tt.identifier, HashToken(tt.value))
		removed := tt.identifier == "password-reset:user@example.com"
		if removed && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: want removed, error = %v", tt.value, err)
		}
		if !removed && err != nil {
			t.Errorf("%s: want kept (exact identifier match only), error = %v", tt.value, err)
		}
	}

	// The unique index is released: the same pair can be stored again.
	again := &VerificationToken{Identifier: "password-reset:user@example.com", ValueHash: HashToken("reset-1")}
	if err := store.CreateVerificationToken(ctx, again); err != nil {
		t.Errorf("re-creating a deleted pair: %v", err)
	}

	// Zero matches and an empty identifier succeed.
	for _, identifier := range []string{"password-reset:nobody@example.com", ""} {
		if err := store.DeleteVerificationTokensByIdentifier(ctx, identifier); err != nil {
			t.Errorf("DeleteVerificationTokensByIdentifier(%q) = %v, want nil", identifier, err)
		}
	}
	if got := tokenCount(store); got != 4 {
		t.Errorf("tokenCount = %d, want 4", got)
	}
}

func TestMemoryStoreDeleteExpiredSessions(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	cutoff := memStoreNow

	sessions := []struct {
		name string
		exp  time.Time
		kept bool
	}{
		{"expired", cutoff.Add(-time.Second), false},
		{"long expired", cutoff.Add(-30 * 24 * time.Hour), false},
		{"exactly at cutoff", cutoff, true}, // strictly before is removed
		{"fresh", cutoff.Add(time.Hour), true},
	}
	for _, tt := range sessions {
		session := &Session{UserID: "user-1", TokenHash: HashToken(tt.name), ExpiresAt: tt.exp}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession(%s): %v", tt.name, err)
		}
	}

	if err := store.DeleteExpiredSessions(ctx, cutoff); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	for _, tt := range sessions {
		_, err := store.GetSessionByTokenHash(ctx, HashToken(tt.name))
		if tt.kept && err != nil {
			t.Errorf("%s: session should have been kept: %v", tt.name, err)
		}
		if !tt.kept && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: session should have been removed, error = %v", tt.name, err)
		}
	}
	if listed, _ := store.ListSessionsByUser(ctx, "user-1"); len(listed) != 2 {
		t.Errorf("ListSessionsByUser = %d sessions, want 2", len(listed))
	}
	// Idempotent; an empty store succeeds too.
	if err := store.DeleteExpiredSessions(ctx, cutoff); err != nil {
		t.Errorf("second DeleteExpiredSessions = %v", err)
	}
	if err := NewMemoryStore().DeleteExpiredSessions(ctx, cutoff); err != nil {
		t.Errorf("DeleteExpiredSessions on an empty store = %v", err)
	}
}

func TestMemoryStoreListUsersPagination(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Five users with strictly increasing creation times.
	emails := make([]string, 5)
	for i := 0; i < 5; i++ {
		emails[i] = fmt.Sprintf("user%d@example.com", i)
		user := &User{
			Email:     emails[i],
			CreatedAt: memStoreNow.Add(time.Duration(i) * time.Hour),
			UpdatedAt: memStoreNow.Add(time.Duration(i) * time.Hour),
		}
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatalf("CreateUser(%d): %v", i, err)
		}
	}

	tests := []struct {
		name    string
		opts    ListOptions
		wantLen int
		wantTop string // first email of the page
	}{
		{"default", ListOptions{}, 5, "user4@example.com"},
		{"limit 2", ListOptions{Limit: 2}, 2, "user4@example.com"},
		{"offset 2", ListOptions{Offset: 2}, 3, "user2@example.com"},
		{"limit 2 offset 2", ListOptions{Limit: 2, Offset: 2}, 2, "user2@example.com"},
		{"offset beyond end", ListOptions{Offset: 5}, 0, ""},
		{"limit beyond end", ListOptions{Limit: 100}, 5, "user4@example.com"},
		{"negative offset", ListOptions{Limit: 1, Offset: -3}, 1, "user4@example.com"},
		{"negative limit uses default", ListOptions{Limit: -1}, 5, "user4@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, err := store.ListUsers(ctx, tt.opts)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if len(users) != tt.wantLen {
				t.Fatalf("len(users) = %d, want %d", len(users), tt.wantLen)
			}
			if tt.wantTop != "" && users[0].Email != tt.wantTop {
				t.Errorf("users[0].Email = %q, want %q", users[0].Email, tt.wantTop)
			}
		})
	}
}

func TestMemoryStoreListUsersDefaultLimit(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const total = defaultListLimit + 5
	for i := 0; i < total; i++ {
		user := &User{
			Email:     fmt.Sprintf("bulk%d@example.com", i),
			CreatedAt: memStoreNow.Add(time.Duration(i) * time.Second),
		}
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatalf("CreateUser(%d): %v", i, err)
		}
	}
	users, err := store.ListUsers(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != defaultListLimit {
		t.Errorf("len(users) = %d, want the default %d", len(users), defaultListLimit)
	}
	if users[0].Email != fmt.Sprintf("bulk%d@example.com", total-1) {
		t.Errorf("users[0].Email = %q, want the newest user", users[0].Email)
	}
}

func TestMemoryStoreListUsersTieBreaker(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	same := memStoreNow
	for i := 0; i < 3; i++ {
		if err := store.CreateUser(ctx, &User{Email: fmt.Sprintf("tie%d@example.com", i), CreatedAt: same}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}
	users, err := store.ListUsers(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	for i := 1; i < len(users); i++ {
		if users[i-1].CreatedAt.Equal(users[i].CreatedAt) && users[i-1].ID < users[i].ID {
			t.Errorf("equal CreatedAt not ordered by descending ID: %q before %q",
				users[i-1].ID, users[i].ID)
		}
	}
}

func TestMemoryStoreListUsersEmpty(t *testing.T) {
	store := NewMemoryStore()
	users, err := store.ListUsers(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if users == nil {
		t.Fatal("ListUsers returned nil, want empty non-nil")
	}
	if len(users) != 0 {
		t.Fatalf("len(users) = %d, want 0", len(users))
	}
}

func TestMemoryStoreCountUsers(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if n, err := store.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers on empty store = (%d, %v), want (0, nil)", n, err)
	}
	for i := 0; i < 3; i++ {
		if err := store.CreateUser(ctx, &User{Email: fmt.Sprintf("c%d@example.com", i)}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}
	if n, err := store.CountUsers(ctx); err != nil || n != 3 {
		t.Fatalf("CountUsers = (%d, %v), want (3, nil)", n, err)
	}
}

func TestMemoryStoreUserNotFoundErrors(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if _, err := store.GetUser(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetUserByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByEmail error = %v, want ErrNotFound", err)
	}
	if err := store.UpdateUser(ctx, &User{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateUser error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetAccount(ctx, "p", "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccount error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetAccountByUser(ctx, "missing", "p"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccountByUser error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteAccount(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAccount error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetVerificationToken(ctx, "missing", "hash"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVerificationToken error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteVerificationToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteVerificationToken error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreCascadeDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	user := &User{Email: "cascade@example.com", Name: "Cascade"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	credential := &Account{UserID: user.ID, Provider: "credential", ProviderAccountID: user.Email, Password: "h"}
	oauth := &Account{UserID: user.ID, Provider: "google", ProviderAccountID: "google-cascade"}
	if err := store.CreateAccount(ctx, credential); err != nil {
		t.Fatalf("CreateAccount(credential): %v", err)
	}
	if err := store.CreateAccount(ctx, oauth); err != nil {
		t.Fatalf("CreateAccount(google): %v", err)
	}
	for i := 0; i < 2; i++ {
		session := &Session{UserID: user.ID, TokenHash: HashToken(fmt.Sprintf("cascade-%d", i))}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	currentEmailToken := &VerificationToken{Identifier: user.Email, ValueHash: HashToken("cascade-tok")}
	oldEmailToken := &VerificationToken{Identifier: "old@example.com", ValueHash: HashToken("cascade-old")}
	if err := store.CreateVerificationToken(ctx, currentEmailToken); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	if err := store.CreateVerificationToken(ctx, oldEmailToken); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if accounts, _ := store.ListAccountsByUser(ctx, user.ID); len(accounts) != 0 {
		t.Errorf("accounts survived the cascade: %v", accounts)
	}
	if _, err := store.GetAccountByUser(ctx, user.ID, "credential"); !errors.Is(err, ErrNotFound) {
		t.Error("credential account survived the cascade")
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken("cascade-0")); !errors.Is(err, ErrNotFound) {
		t.Error("session survived the cascade")
	}
	if sessions, _ := store.ListSessionsByUser(ctx, user.ID); len(sessions) != 0 {
		t.Errorf("sessions survived the cascade: %v", sessions)
	}
	// Verification tokens are not the store's responsibility: the
	// service deletes them (their identifiers are purpose prefixed).
	if _, err := store.GetVerificationToken(ctx, user.Email, HashToken("cascade-tok")); err != nil {
		t.Errorf("DeleteUser removed a verification token: %v", err)
	}
	if _, err := store.GetVerificationToken(ctx, "old@example.com", HashToken("cascade-old")); err != nil {
		t.Errorf("DeleteUser removed a verification token: %v", err)
	}

	// The freed email can be reused.
	if err := store.CreateUser(ctx, &User{Email: user.Email}); err != nil {
		t.Errorf("email not freed after cascade: %v", err)
	}
}

func TestMemoryStoreConcurrentDuplicateAccountCreate(t *testing.T) {
	for round := 0; round < 3; round++ {
		store := NewMemoryStore()
		ctx := context.Background()
		user := &User{Email: fmt.Sprintf("race%d@example.com", round)}
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		results := make([]error, 4)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i] = store.CreateAccount(ctx, &Account{
					UserID:            user.ID,
					Provider:          "credential",
					ProviderAccountID: user.Email,
					Password:          "hash",
				})
			}(i)
		}
		wg.Wait()

		var successes, duplicates int
		for _, err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrDuplicate):
				duplicates++
			default:
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if successes != 1 || duplicates != len(results)-1 {
			t.Fatalf("round %d: successes = %d, duplicates = %d, want 1 success", round, successes, duplicates)
		}
	}
}
