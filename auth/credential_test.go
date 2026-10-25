package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckPassword(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "check@example.com")
	oauth := &User{Email: "oauth-check@example.com", Name: "OAuth", CreatedAt: a.now()}
	if err := a.store.CreateUser(testContext, oauth); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tests := []struct {
		name      string
		userID    string
		password  string
		wantError error
	}{
		{"correct password", out.User.ID, testPassword, nil},
		{"wrong password", out.User.ID, "wrong-password", ErrInvalidCurrentPassword},
		{"empty password", out.User.ID, "", ErrInvalidCurrentPassword},
		{"password with extra whitespace", out.User.ID, testPassword + " ", ErrInvalidCurrentPassword},
		{"unknown user", "missing", testPassword, ErrUserNotFound},
		{"no credential account", oauth.ID, testPassword, ErrAccountNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.CheckPassword(testContext, tt.userID, tt.password); !errors.Is(err, tt.wantError) {
				t.Fatalf("CheckPassword error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestCheckPasswordHasNoSideEffects(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "noside@example.com")
	if err := a.CheckPassword(testContext, out.User.ID, "wrong-password"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("error = %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); err != nil {
		t.Errorf("a failed CheckPassword affected the session: %v", err)
	}
}

func TestChangePasswordInvalidatesResetTokens(t *testing.T) {
	a, _, mailer := newTestAuthWithMailer(t, Config{})
	out := signUpUser(t, a, "pending-reset@example.com")
	if err := a.RequestPasswordReset(testContext, "pending-reset@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	token := lastResetToken(t, a, mailer)

	if err := a.ChangePassword(testContext, out.User.ID, testPassword, resetNewPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if err := a.ResetPassword(testContext, "pending-reset@example.com", token, "attacker-pass-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reset token after ChangePassword: error = %v, want ErrInvalidToken", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "pending-reset@example.com", Password: resetNewPassword}); err != nil {
		t.Errorf("SignIn with the changed password: %v", err)
	}
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "change@example.com")

	if err := a.ChangePassword(testContext, out.User.ID, "wrong-current-1", resetNewPassword); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("error = %v, want ErrInvalidCurrentPassword", err)
	}
	// The password is unchanged.
	if _, err := a.SignIn(testContext, SignInInput{Email: "change@example.com", Password: testPassword}); err != nil {
		t.Errorf("password changed despite a wrong current password: %v", err)
	}
}

func TestChangePasswordWeakNewCheckedAfterCurrent(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "order@example.com")

	// A wrong current password wins over the new-password policy.
	err := a.ChangePassword(testContext, out.User.ID, "wrong-current-1", "short")
	if !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("error = %v, want ErrInvalidCurrentPassword", err)
	}
	// A correct current password with a weak new password is rejected
	// by the policy.
	err = a.ChangePassword(testContext, out.User.ID, testPassword, "short")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("error = %v, want ErrWeakPassword", err)
	}
}

func TestChangePasswordHappy(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "changer@example.com")
	extra, err := a.SignIn(testContext, SignInInput{Email: "changer@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := a.ChangePassword(testContext, out.User.ID, testPassword, resetNewPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if _, err := a.SignIn(testContext, SignInInput{Email: "changer@example.com", Password: testPassword}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password still works after ChangePassword: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "changer@example.com", Password: resetNewPassword}); err != nil {
		t.Errorf("SignIn with the new password: %v", err)
	}
	if _, _, err := a.GetSession(testContext, out.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("signup session not revoked: %v", err)
	}
	if _, _, err := a.GetSession(testContext, extra.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("sign-in session not revoked: %v", err)
	}
}

func TestChangePasswordSamePasswordAllowed(t *testing.T) {
	// Changing to the identical password is allowed by design: bcrypt
	// salts every hash so the stored value changes anyway.
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "samepass@example.com")

	if err := a.ChangePassword(testContext, out.User.ID, testPassword, testPassword); err != nil {
		t.Fatalf("ChangePassword with the same password: %v", err)
	}
	if _, err := a.SignIn(testContext, SignInInput{Email: "samepass@example.com", Password: testPassword}); err != nil {
		t.Errorf("SignIn after a same-password change: %v", err)
	}
}

func TestChangePasswordErrors(t *testing.T) {
	a, _, _ := newTestAuthWithMailer(t, Config{})

	if err := a.ChangePassword(testContext, "missing", testPassword, resetNewPassword); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user error = %v, want ErrUserNotFound", err)
	}

	// User without a credential account.
	user := &User{Email: "oauth-change@example.com", Name: "OAuth"}
	if err := a.store.CreateUser(testContext, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := a.ChangePassword(testContext, user.ID, testPassword, resetNewPassword); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("no credential account error = %v, want ErrAccountNotFound", err)
	}

	// Empty new password hits the policy.
	out := signUpUser(t, a, "policy@example.com")
	if err := a.ChangePassword(testContext, out.User.ID, testPassword, ""); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("empty new password error = %v, want ErrPasswordRequired", err)
	}
	if err := a.ChangePassword(testContext, out.User.ID, testPassword, strings.Repeat("x", 73)); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("oversized new password error = %v, want ErrWeakPassword", err)
	}
}
