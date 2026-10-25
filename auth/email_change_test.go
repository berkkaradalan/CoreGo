package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestChangeEmailHappy(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "oldmail@example.com")
	verifyUser(t, a, mailer, out.User.ID)
	sentBefore := len(mailer.verificationCalls())

	user, err := a.ChangeEmail(testContext, out.User.ID, "  NewMail@Example.com ", testPassword)
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if user.Email != "newmail@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "newmail@example.com")
	}
	if user.EmailVerified {
		t.Error("EmailVerified = true after an email change, want re-verification")
	}
	stored, _ := a.GetUser(testContext, out.User.ID)
	if stored.Email != "newmail@example.com" || stored.EmailVerified {
		t.Errorf("stored user = %+v", stored)
	}

	// The credential account id follows the new email.
	account, err := a.store.GetAccountByUser(testContext, out.User.ID, CredentialProvider)
	if err != nil {
		t.Fatalf("GetAccountByUser: %v", err)
	}
	if account.ProviderAccountID != "newmail@example.com" {
		t.Errorf("ProviderAccountID = %q, want %q", account.ProviderAccountID, "newmail@example.com")
	}

	// Sign-in works with the new email and the existing password; the
	// old email no longer resolves.
	if _, err := a.SignIn(testContext, SignInInput{Email: "newmail@example.com", Password: testPassword}); err != nil {
		t.Errorf("SignIn with the new email: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "oldmail@example.com", Password: testPassword}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("SignIn with the old email error = %v, want ErrInvalidCredentials", err)
	}
	// Existing sessions are kept.
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Errorf("session lost after ChangeEmail: %v", err)
	}

	// A verification email for the new address went out in the
	// background, and its token verifies the new address.
	token := lastVerificationToken(t, a, mailer)
	calls := mailer.verificationCalls()
	if len(calls) != sentBefore+1 || calls[len(calls)-1].To != "newmail@example.com" {
		t.Fatalf("verification calls = %+v, want one more to the new address", calls)
	}
	if err := a.VerifyEmail(testContext, "newmail@example.com", token); err != nil {
		t.Fatalf("VerifyEmail(new address): %v", err)
	}
	if user, _ := a.GetUser(testContext, out.User.ID); !user.EmailVerified {
		t.Error("new address not verified")
	}
}

func TestChangeEmailWithoutMailer(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "quiet@example.com")

	user, err := a.ChangeEmail(testContext, out.User.ID, "quiet-new@example.com", testPassword)
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if user.Email != "quiet-new@example.com" {
		t.Errorf("Email = %q", user.Email)
	}
	if got := tokenCount(memStore(t, a)); got != 0 {
		t.Errorf("tokenCount = %d, want 0 without a mailer", got)
	}
}

func TestChangeEmailErrors(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	mover := signUpUser(t, a, "mover@example.com")
	signUpUser(t, a, "keeper@example.com")
	oauth := &User{Email: "oauth@example.com", Name: "OAuth", CreatedAt: a.now()}
	if err := a.store.CreateUser(testContext, oauth); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tests := []struct {
		name      string
		userID    string
		newEmail  string
		password  string
		wantError error
	}{
		{"empty email", mover.User.ID, "", testPassword, ErrEmailRequired},
		{"whitespace email", mover.User.ID, "   ", testPassword, ErrEmailRequired},
		{"malformed email", mover.User.ID, "not-an-email", testPassword, ErrInvalidEmail},
		{"display name form", mover.User.ID, "Mover <new@example.com>", testPassword, ErrInvalidEmail},
		{"wrong password", mover.User.ID, "fresh@example.com", "wrong-password", ErrInvalidCurrentPassword},
		{"empty password", mover.User.ID, "fresh@example.com", "", ErrInvalidCurrentPassword},
		// The password is verified before the address is looked up, so
		// the method cannot be used to probe registered addresses.
		{"taken address with wrong password", mover.User.ID, "keeper@example.com", "wrong-password", ErrInvalidCurrentPassword},
		{"taken address", mover.User.ID, "keeper@example.com", testPassword, ErrUserAlreadyExists},
		{"taken address other case", mover.User.ID, " KEEPER@example.com", testPassword, ErrUserAlreadyExists},
		{"unknown user", "missing", "fresh@example.com", testPassword, ErrUserNotFound},
		{"no credential account", oauth.ID, "fresh@example.com", testPassword, ErrAccountNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := a.ChangeEmail(testContext, tt.userID, tt.newEmail, tt.password)
			if user != nil {
				t.Errorf("ChangeEmail returned a user on failure: %+v", user)
			}
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
	// The mover's email is unchanged after every rejected attempt.
	user, _ := a.GetUser(testContext, mover.User.ID)
	if user.Email != "mover@example.com" {
		t.Errorf("Email = %q after rejected changes", user.Email)
	}
}

func TestChangeEmailSameAddressKeepsVerification(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "stable@example.com")
	verifyUser(t, a, mailer, out.User.ID)
	sentBefore := len(mailer.verificationCalls())
	tokensBefore := tokenCount(memStore(t, a))

	for _, same := range []string{"stable@example.com", "  STABLE@Example.com "} {
		user, err := a.ChangeEmail(testContext, out.User.ID, same, testPassword)
		if err != nil {
			t.Fatalf("ChangeEmail(%q): %v", same, err)
		}
		if user.Email != "stable@example.com" || !user.EmailVerified {
			t.Errorf("ChangeEmail(%q) changed the user: %+v", same, user)
		}
	}
	// The password is still checked for the same address.
	if _, err := a.ChangeEmail(testContext, out.User.ID, "stable@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Errorf("same address with a wrong password error = %v, want ErrInvalidCurrentPassword", err)
	}
	waitForDeliveries(t, a)
	if got := len(mailer.verificationCalls()); got != sentBefore {
		t.Errorf("verification emails = %d, want %d (nothing changed)", got, sentBefore)
	}
	if got := tokenCount(memStore(t, a)); got != tokensBefore {
		t.Errorf("tokenCount = %d, want %d", got, tokensBefore)
	}
}

// TestChangeEmailInvalidatesOldAddressTokens is the regression test for
// the confirmed takeover: tokens issued to the old address stayed
// valid after an email change, so whoever registered that address next
// could be taken over by the previous holder of those tokens.
func TestChangeEmailInvalidatesOldAddressTokens(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	original := signUpUser(t, a, "shared@example.com")
	verificationToken := lastVerificationToken(t, a, mailer)
	if err := a.RequestPasswordReset(testContext, "shared@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	resetToken := lastResetToken(t, a, mailer)

	if _, err := a.ChangeEmail(testContext, original.User.ID, "moved@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	store := memStore(t, a)
	for _, identifier := range []string{"email-verification:shared@example.com", "password-reset:shared@example.com"} {
		if got := tokensWithIdentifier(store, identifier); got != 0 {
			t.Errorf("%d tokens with identifier %q survived the email change", got, identifier)
		}
	}

	// A new user registers the released address.
	victim := signUpUser(t, a, "shared@example.com")

	if err := a.ResetPassword(testContext, "shared@example.com", resetToken, "attacker-pass-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old reset token against the new owner: error = %v, want ErrInvalidToken", err)
	}
	if err := a.VerifyEmail(testContext, "shared@example.com", verificationToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old verification token against the new owner: error = %v, want ErrInvalidToken", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "shared@example.com", Password: testPassword}); err != nil {
		t.Errorf("new owner cannot sign in: %v", err)
	}
	if user, _ := a.GetUser(testContext, victim.User.ID); user.EmailVerified {
		t.Error("the new owner's address was verified with the old owner's token")
	}
}

// TestChangeEmailOldTokensDoNotFollowTheUser checks that a token sent
// to the old address does not verify the new one.
func TestChangeEmailOldTokensDoNotFollowTheUser(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "first@example.com")
	oldToken := lastVerificationToken(t, a, mailer)

	if _, err := a.ChangeEmail(testContext, out.User.ID, "second@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	for _, email := range []string{"first@example.com", "second@example.com"} {
		if err := a.VerifyEmail(testContext, email, oldToken); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("VerifyEmail(%q, old token) error = %v, want ErrInvalidToken", email, err)
		}
	}
}

func TestChangeEmailRequireVerifiedEmailBlocksExistingSessions(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{RequireVerifiedEmail: true})
	out := signUpUser(t, a, "strict@example.com")
	verifyUser(t, a, mailer, out.User.ID)
	signIn, err := a.SignIn(testContext, SignInInput{Email: "strict@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if _, err := a.ChangeEmail(testContext, out.User.ID, "strict-new@example.com", testPassword); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if _, _, err := a.GetSession(testContext, signIn.SessionToken); !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("GetSession after the change error = %v, want ErrEmailNotVerified", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "strict-new@example.com", Password: testPassword}); !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("SignIn after the change error = %v, want ErrEmailNotVerified", err)
	}

	if err := a.VerifyEmail(testContext, "strict-new@example.com", lastVerificationToken(t, a, mailer)); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if _, _, err := a.GetSession(testContext, signIn.SessionToken); err != nil {
		t.Fatalf("GetSession after verifying the new address: %v", err)
	}
}

func TestChangeEmailStoreFailures(t *testing.T) {
	t.Run("token cleanup failure aborts before any change", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "abort@example.com")
		store := a.store
		a.store = &faultyStore{Store: store, err: errStoreFailure, deleteTokensByIdent: true}

		if _, err := a.ChangeEmail(testContext, out.User.ID, "abort-new@example.com", testPassword); !errors.Is(err, errStoreFailure) {
			t.Fatalf("error = %v, want the store failure", err)
		}
		if user, _ := store.GetUser(testContext, out.User.ID); user.Email != "abort@example.com" {
			t.Errorf("Email = %q, want unchanged", user.Email)
		}
	})
	t.Run("duplicate on update maps to ErrUserAlreadyExists", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		out := signUpUser(t, a, "race-loser@example.com")
		a.store = &faultyStore{Store: a.store, err: ErrDuplicate, updateUser: true}

		if _, err := a.ChangeEmail(testContext, out.User.ID, "race-target@example.com", testPassword); !errors.Is(err, ErrUserAlreadyExists) {
			t.Fatalf("error = %v, want ErrUserAlreadyExists", err)
		}
	})
	t.Run("account sync failure rolls the user back", func(t *testing.T) {
		for _, syncErr := range []error{errStoreFailure, ErrDuplicate} {
			a, _ := newTestAuth(t, Config{})
			out := signUpUser(t, a, "rollback@example.com")
			store := a.store
			a.store = &faultyStore{Store: store, err: syncErr, updateAccount: true}

			_, err := a.ChangeEmail(testContext, out.User.ID, "rollback-new@example.com", testPassword)
			want := syncErr
			if errors.Is(syncErr, ErrDuplicate) {
				want = ErrUserAlreadyExists
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			user, _ := store.GetUser(testContext, out.User.ID)
			if user.Email != "rollback@example.com" {
				t.Errorf("Email = %q after a failed sync, want the old address", user.Email)
			}
			if _, err := store.GetUserByEmail(testContext, "rollback-new@example.com"); !errors.Is(err, ErrNotFound) {
				t.Errorf("new address still reserved after rollback: %v", err)
			}
		}
	})
	t.Run("verification token failure is logged, not returned", func(t *testing.T) {
		logger, logs := newLogCapture()
		a, _, _ := newTestAuthWithMailer(t, Config{Logger: logger})
		out := signUpUser(t, a, "logonly@example.com")
		a.store = &faultyStore{Store: a.store, err: errStoreFailure, createToken: true}

		user, err := a.ChangeEmail(testContext, out.User.ID, "logonly-new@example.com", testPassword)
		if err != nil {
			t.Fatalf("ChangeEmail = %v, want nil (the change succeeded)", err)
		}
		if user.Email != "logonly-new@example.com" {
			t.Errorf("Email = %q", user.Email)
		}
		if out := logs.String(); !containsAll(out, "level=ERROR", "store backend failure") {
			t.Errorf("token failure not logged:\n%s", out)
		}
	})
}

// TestConcurrentChangeEmailAndSignUp races an email change against a
// registration of the same address: exactly one must win and the
// data must stay consistent.
func TestConcurrentChangeEmailAndSignUp(t *testing.T) {
	for round := 0; round < 10; round++ {
		a, _ := newTestAuth(t, Config{})
		mover := signUpUser(t, a, fmt.Sprintf("mover%d@example.com", round))
		target := fmt.Sprintf("contested%d@example.com", round)

		var wg sync.WaitGroup
		var changeErr, signUpErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, changeErr = a.ChangeEmail(testContext, mover.User.ID, target, testPassword)
		}()
		go func() {
			defer wg.Done()
			_, signUpErr = a.SignUp(testContext, SignUpInput{Name: "Racer", Email: target, Password: testPassword})
		}()
		wg.Wait()

		switch {
		case changeErr == nil && errors.Is(signUpErr, ErrUserAlreadyExists):
		case signUpErr == nil && errors.Is(changeErr, ErrUserAlreadyExists):
		default:
			t.Fatalf("round %d: ChangeEmail = %v, SignUp = %v; want exactly one winner", round, changeErr, signUpErr)
		}

		owner, err := a.store.GetUserByEmail(testContext, target)
		if err != nil {
			t.Fatalf("round %d: contested address has no owner: %v", round, err)
		}
		account, err := a.store.GetAccount(testContext, CredentialProvider, target)
		if err != nil {
			t.Fatalf("round %d: no credential account for the contested address: %v", round, err)
		}
		if account.UserID != owner.ID {
			t.Fatalf("round %d: credential account belongs to %q, user to %q", round, account.UserID, owner.ID)
		}
		if n, _ := a.CountUsers(testContext); (changeErr == nil && n != 1) || (signUpErr == nil && n != 2) {
			t.Fatalf("round %d: CountUsers = %d (ChangeEmail err %v, SignUp err %v)", round, n, changeErr, signUpErr)
		}
	}
}

// containsAll reports whether s contains every substring.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
