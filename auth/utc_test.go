package auth

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// assertUTC fails the test when any of the named timestamps is not in
// the time.UTC location. time.Now() reports time.Local even when the
// machine's zone is UTC, so the check is meaningful on every machine.
func assertUTC(t *testing.T, stamps map[string]time.Time) {
	t.Helper()
	for name, ts := range stamps {
		if ts.IsZero() {
			t.Errorf("%s is zero", name)
			continue
		}
		if ts.Location() != time.UTC {
			t.Errorf("%s location = %v, want UTC", name, ts.Location())
		}
	}
}

func TestDefaultClockIsUTC(t *testing.T) {
	a, err := New(Config{Logger: slog.New(slog.DiscardHandler)}, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if loc := a.now().Location(); loc != time.UTC {
		t.Fatalf("default clock location = %v, want UTC", loc)
	}
}

func TestServiceTimestampsAreUTC(t *testing.T) {
	ctx := context.Background()
	a, err := New(Config{Logger: slog.New(slog.DiscardHandler)}, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := a.SignUp(ctx, SignUpInput{Name: "Ada", Email: "ada@example.com", Password: "analytical-engine"})
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	assertUTC(t, map[string]time.Time{
		"SignUp User.CreatedAt": out.User.CreatedAt,
		"SignUp User.UpdatedAt": out.User.UpdatedAt,
	})

	session, user, err := a.GetSession(ctx, out.SessionToken)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	assertUTC(t, map[string]time.Time{
		"GetSession Session.ExpiresAt": session.ExpiresAt,
		"GetSession Session.CreatedAt": session.CreatedAt,
		"GetSession Session.UpdatedAt": session.UpdatedAt,
		"GetSession User.CreatedAt":    user.CreatedAt,
		"GetSession User.UpdatedAt":    user.UpdatedAt,
	})

	in, err := a.SignIn(ctx, SignInInput{Email: "ada@example.com", Password: "analytical-engine", RememberMe: true})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	assertUTC(t, map[string]time.Time{
		"SignIn Session.ExpiresAt": in.Session.ExpiresAt,
		"SignIn Session.CreatedAt": in.Session.CreatedAt,
		"SignIn Session.UpdatedAt": in.Session.UpdatedAt,
	})

	name := "Ada Lovelace"
	updated, err := a.UpdateUser(ctx, out.User.ID, UpdateUserInput{Name: &name})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	assertUTC(t, map[string]time.Time{
		"UpdateUser User.CreatedAt": updated.CreatedAt,
		"UpdateUser User.UpdatedAt": updated.UpdatedAt,
	})
}

func TestMemoryStoreStampsAreUTC(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	user := &User{Email: "ada@example.com", Name: "Ada"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	account := &Account{UserID: user.ID, Provider: CredentialProvider, ProviderAccountID: user.ID}
	if err := store.CreateAccount(ctx, account); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	session := &Session{UserID: user.ID, TokenHash: "hash", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token := &VerificationToken{Identifier: "id", ValueHash: "value", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.CreateVerificationToken(ctx, token); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	assertUTC(t, map[string]time.Time{
		"created User.CreatedAt":              user.CreatedAt,
		"created User.UpdatedAt":              user.UpdatedAt,
		"created Account.CreatedAt":           account.CreatedAt,
		"created Account.UpdatedAt":           account.UpdatedAt,
		"created Session.CreatedAt":           session.CreatedAt,
		"created Session.UpdatedAt":           session.UpdatedAt,
		"created VerificationToken.CreatedAt": token.CreatedAt,
	})

	// Update methods force UpdatedAt even when the caller passes a local
	// time.
	local := time.Now().In(time.FixedZone("UTC+3", 3*60*60))
	user.UpdatedAt, account.UpdatedAt, session.UpdatedAt = local, local, local
	if err := store.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if err := store.UpdateAccount(ctx, account); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if err := store.UpdateSession(ctx, session); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	gotUser, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	gotSession, err := store.GetSessionByTokenHash(ctx, "hash")
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	gotToken, err := store.GetVerificationToken(ctx, "id", "value")
	if err != nil {
		t.Fatalf("GetVerificationToken: %v", err)
	}
	stamps := map[string]time.Time{
		"read User.CreatedAt":              gotUser.CreatedAt,
		"read User.UpdatedAt":              gotUser.UpdatedAt,
		"read Session.CreatedAt":           gotSession.CreatedAt,
		"read Session.UpdatedAt":           gotSession.UpdatedAt,
		"read VerificationToken.CreatedAt": gotToken.CreatedAt,
	}
	accounts, err := store.ListAccountsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAccountsByUser: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ListAccountsByUser returned %d accounts, want 1", len(accounts))
	}
	stamps["read Account.CreatedAt"] = accounts[0].CreatedAt
	stamps["read Account.UpdatedAt"] = accounts[0].UpdatedAt
	assertUTC(t, stamps)
}
