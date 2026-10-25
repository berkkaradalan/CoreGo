package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGetUser(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "profile@example.com")

	user, err := a.GetUser(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Email != "profile@example.com" {
		t.Errorf("Email = %q", user.Email)
	}
	if _, err := a.GetUser(testContext, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser(missing) error = %v, want ErrUserNotFound", err)
	}
}

func TestUpdateUserNameAndImage(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "update@example.com")

	newName := "  Alice Renamed "
	newImage := "https://cdn.example.com/new.png"
	user, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{
		Name:  &newName,
		Image: &newImage,
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if user.Name != "Alice Renamed" {
		t.Errorf("Name = %q, want trimmed %q", user.Name, "Alice Renamed")
	}
	if user.Image != newImage {
		t.Errorf("Image = %q, want %q", user.Image, newImage)
	}
	if user.Email != "update@example.com" {
		t.Errorf("Email = %q, unchanged email was modified", user.Email)
	}

	// Persisted.
	reloaded, _ := a.GetUser(testContext, out.User.ID)
	if reloaded.Name != "Alice Renamed" || reloaded.Image != newImage {
		t.Errorf("reloaded = %+v", reloaded)
	}
}

func TestUpdateUserNilFieldsChangeNothing(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "nils@example.com")

	user, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if user.Name != out.User.Name || user.Image != out.User.Image || user.Email != out.User.Email {
		t.Errorf("no-op update changed the user: %+v", user)
	}
}

func TestUpdateUserEmptyName(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "emptyname@example.com")

	empty := "   "
	if _, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: &empty}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("error = %v, want ErrNameRequired", err)
	}
}

func TestUpdateUserUnknownUser(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	name := "Ghost"
	if _, err := a.UpdateUser(testContext, "missing", UpdateUserInput{Name: &name}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "deleteme@example.com")
	other := signUpUser(t, a, "bystander@example.com")

	// Outstanding tokens of both purposes and an extra session.
	if err := a.SendVerificationEmail(testContext, out.User.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}
	if err := a.RequestPasswordReset(testContext, "deleteme@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	waitForDeliveries(t, a)
	if calls := mailer.verificationCalls(); len(calls) != 3 { // two signups + explicit
		t.Fatalf("verification calls = %d, want 3", len(calls))
	}
	extra, err := a.SignIn(testContext, SignInInput{Email: "deleteme@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := a.DeleteUser(testContext, out.User.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := a.GetUser(testContext, out.User.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetUser after delete error = %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("session survived DeleteUser: %v", err)
	}
	if _, _, err := a.GetSession(testContext, extra.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("extra session survived DeleteUser: %v", err)
	}
	accounts, err := a.ListAccounts(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("accounts survived DeleteUser: %v", accounts)
	}
	store := memStore(t, a)
	for _, identifier := range []string{"email-verification:deleteme@example.com", "password-reset:deleteme@example.com"} {
		if got := tokensWithIdentifier(store, identifier); got != 0 {
			t.Errorf("%d tokens with identifier %q survived DeleteUser", got, identifier)
		}
	}
	// Other users are untouched.
	if got := tokensWithIdentifier(store, "email-verification:bystander@example.com"); got != 1 {
		t.Errorf("bystander tokens = %d, want 1", got)
	}
	if _, _, err := a.GetSession(testContext, other.SessionToken); err != nil {
		t.Errorf("bystander session affected: %v", err)
	}
	if err := a.DeleteUser(testContext, out.User.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("second DeleteUser error = %v, want ErrUserNotFound", err)
	}
}

func TestDeleteUserTokenCleanupFailureKeepsUser(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "keep@example.com")
	store := a.store
	a.store = &faultyStore{Store: store, err: errStoreFailure, deleteTokensByIdent: true}

	if err := a.DeleteUser(testContext, out.User.ID); !errors.Is(err, errStoreFailure) {
		t.Fatalf("error = %v, want the store failure", err)
	}
	// Nothing was deleted, so the operation can simply be retried.
	if _, err := store.GetUser(testContext, out.User.ID); err != nil {
		t.Errorf("user deleted although the token cleanup failed: %v", err)
	}
}

func TestDeleteUserUnknown(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	if err := a.DeleteUser(testContext, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}

func TestUpdateUserDoesNotTouchEmail(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "profile-only@example.com")
	verifyUser(t, a, mailer, out.User.ID)

	name := "Renamed"
	user, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: &name})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if user.Email != "profile-only@example.com" || !user.EmailVerified {
		t.Errorf("UpdateUser changed email state: %+v", user)
	}
}

func TestListAccounts(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "accounts@example.com")

	// Add an older OAuth account directly in the store so the ordering
	// across providers can be checked.
	old := &Account{
		UserID:            out.User.ID,
		Provider:          "google",
		ProviderAccountID: "google-accounts",
		CreatedAt:         out.User.CreatedAt.Add(-time.Hour),
		UpdatedAt:         out.User.CreatedAt.Add(-time.Hour),
	}
	if err := a.store.CreateAccount(context.Background(), old); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	accounts, err := a.ListAccounts(testContext, out.User.ID)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	if accounts[0].Provider != "google" || accounts[1].Provider != "credential" {
		t.Errorf("order = [%s, %s], want oldest first (google, credential)",
			accounts[0].Provider, accounts[1].Provider)
	}

	// Unknown users get an empty non-nil slice.
	empty, err := a.ListAccounts(testContext, "nobody")
	if err != nil {
		t.Fatalf("ListAccounts(nobody): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("ListAccounts(nobody) = %v, want empty non-nil", empty)
	}
}
