package authhttp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestResetPassword(t *testing.T) {
	app := newTestApp(t, appOptions{})
	session := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	expectStatus(t, app.do(t, req{method: http.MethodPost, path: "/forgot-password", body: map[string]any{"email": "ada@example.com"}}), http.StatusAccepted)
	app.waitEmails(t)
	token := app.mailer.resetToken("ada@example.com")

	tests := []struct {
		name    string
		body    map[string]any
		code    int
		message string
	}{
		{"weak password keeps the token", map[string]any{"email": "ada@example.com", "token": token, "new_password": "short"}, 400, msgWeakPassword},
		{"missing password", map[string]any{"email": "ada@example.com", "token": token}, 400, msgPasswordRequired},
		{"wrong token", map[string]any{"email": "ada@example.com", "token": "wrong", "new_password": "a-new-password"}, 400, msgInvalidToken},
		{"other email", map[string]any{"email": "bob@example.com", "token": token, "new_password": "a-new-password"}, 400, msgInvalidToken},
		{"success", map[string]any{"email": "ada@example.com", "token": token, "new_password": "a-new-password"}, 204, ""},
		{"reused token", map[string]any{"email": "ada@example.com", "token": token, "new_password": "another-password"}, 400, msgInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/reset-password", body: tt.body})
			if tt.code == http.StatusNoContent {
				expectStatus(t, rec, tt.code)
				return
			}
			expectError(t, rec, tt.code, tt.message)
		})
	}

	// Every session was revoked; only the new password works.
	expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: session}), http.StatusUnauthorized, msgSessionNotFound)
	expectError(t, app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
		"email": "ada@example.com", "password": testPassword,
	}}), http.StatusUnauthorized, msgInvalidCredentials)
	app.signIn(t, "ada@example.com", "a-new-password")
}

func TestResetPasswordExpiredToken(t *testing.T) {
	app := newTestApp(t, appOptions{authCfg: func(c *auth.Config) { c.PasswordResetTokenTTL = 1 }})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.do(t, req{method: http.MethodPost, path: "/forgot-password", body: map[string]any{"email": "ada@example.com"}})
	app.waitEmails(t)
	rec := app.do(t, req{method: http.MethodPost, path: "/reset-password", body: map[string]any{
		"email": "ada@example.com", "token": app.mailer.resetToken("ada@example.com"), "new_password": "a-new-password",
	}})
	expectError(t, rec, http.StatusBadRequest, msgTokenExpired)
}

func TestChangePassword(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	other := app.signIn(t, "ada@example.com", testPassword)

	failures := []struct {
		name    string
		body    map[string]any
		code    int
		message string
	}{
		{"wrong current password", map[string]any{"current_password": "wrong-password", "new_password": "a-new-password"}, 400, msgInvalidCurrentPassword},
		{"missing current password", map[string]any{"new_password": "a-new-password"}, 400, msgInvalidCurrentPassword},
		{"weak new password", map[string]any{"current_password": testPassword, "new_password": "short"}, 400, msgWeakPassword},
		{"missing new password", map[string]any{"current_password": testPassword}, 400, msgPasswordRequired},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/change-password", token: token, body: tt.body})
			expectError(t, rec, tt.code, tt.message)
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("a wrong current password is not an authentication challenge")
			}
		})
	}

	t.Run("requires a session", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodPost, path: "/change-password", body: map[string]any{
			"current_password": testPassword, "new_password": "a-new-password",
		}})
		expectError(t, rec, http.StatusUnauthorized, msgMissingToken)
	})

	t.Run("success revokes every session", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodPost, path: "/change-password", token: token, body: map[string]any{
			"current_password": testPassword, "new_password": "a-new-password",
		}})
		expectStatus(t, rec, http.StatusNoContent)
		for _, s := range []string{token, other} {
			expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: s}), http.StatusUnauthorized, msgSessionNotFound)
		}
		app.signIn(t, "ada@example.com", "a-new-password")
	})
}

func TestChangeEmail(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	app.signUp(t, "Bob", "bob@example.com", testPassword)
	app.updateUser(t, "ada@example.com", func(u *auth.User) { u.EmailVerified = true })

	failures := []struct {
		name    string
		body    map[string]any
		code    int
		message string
	}{
		{"wrong password", map[string]any{"new_email": "new@example.com", "current_password": "wrong-password"}, 400, msgInvalidCurrentPassword},
		{"missing password", map[string]any{"new_email": "new@example.com"}, 400, msgInvalidCurrentPassword},
		{"missing email", map[string]any{"current_password": testPassword}, 400, msgEmailRequired},
		{"invalid email", map[string]any{"new_email": "nope", "current_password": testPassword}, 400, msgInvalidEmail},
		{"taken", map[string]any{"new_email": "BOB@example.com", "current_password": testPassword}, 409, msgUserAlreadyExists},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPost, path: "/change-email", token: token, body: tt.body})
			expectError(t, rec, tt.code, tt.message)
		})
	}

	t.Run("same address keeps the verification", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodPost, path: "/change-email", token: token, body: map[string]any{
			"new_email": "ADA@example.com", "current_password": testPassword,
		}})
		expectStatus(t, rec, http.StatusOK)
		var out struct {
			User userResult `json:"user"`
		}
		decode(t, rec, &out)
		if out.User.Email != "ada@example.com" || !out.User.EmailVerified {
			t.Fatalf("unexpected user %+v", out.User)
		}
	})

	t.Run("success", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodPost, path: "/change-email", token: token, body: map[string]any{
			"new_email": " New@Example.com ", "current_password": testPassword,
		}})
		expectStatus(t, rec, http.StatusOK)
		var out struct {
			User userResult `json:"user"`
		}
		decode(t, rec, &out)
		if out.User.Email != "new@example.com" || out.User.EmailVerified {
			t.Fatalf("unexpected user %+v", out.User)
		}
		// The session survives; the new address gets a verification email.
		expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusOK)
		app.waitEmails(t)
		if app.mailer.verificationToken("new@example.com") == "" {
			t.Fatal("no verification email to the new address")
		}
		app.signIn(t, "new@example.com", testPassword)
	})
}

func TestChangeEmailWithRequiredVerificationLocksTheSession(t *testing.T) {
	app := newTestApp(t, appOptions{authCfg: func(c *auth.Config) { c.RequireVerifiedEmail = true }})
	app.signUp(t, "Ada", "ada@example.com", testPassword)
	app.updateUser(t, "ada@example.com", func(u *auth.User) { u.EmailVerified = true })
	token := app.signIn(t, "ada@example.com", testPassword)

	expectStatus(t, app.do(t, req{method: http.MethodPost, path: "/change-email", token: token, body: map[string]any{
		"new_email": "new@example.com", "current_password": testPassword,
	}}), http.StatusOK)
	expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusForbidden, msgEmailNotVerified)

	app.waitEmails(t)
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: verifyLinkPath("new@example.com", app.mailer.verificationToken("new@example.com"))}), http.StatusOK)
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusOK)
}

func TestGetAndUpdateMe(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token

	type meBody struct {
		User userResult `json:"user"`
	}
	rec := app.do(t, req{method: http.MethodGet, path: "/me", token: token})
	expectStatus(t, rec, http.StatusOK)
	var me meBody
	decode(t, rec, &me)
	if me.User.Name != "Ada" || me.User.Email != "ada@example.com" {
		t.Fatalf("unexpected /me %+v", me.User)
	}

	tests := []struct {
		name      string
		raw       string
		code      int
		message   string
		wantName  string
		wantImage string
	}{
		{"name only", `{"name":" Ada Lovelace "}`, 200, "", "Ada Lovelace", ""},
		{"image only keeps the name", `{"image":"https://img.example/a.png"}`, 200, "", "Ada Lovelace", "https://img.example/a.png"},
		{"empty object changes nothing", `{}`, 200, "", "Ada Lovelace", "https://img.example/a.png"},
		{"clear the image", `{"image":""}`, 200, "", "Ada Lovelace", ""},
		{"blank name", `{"name":"  "}`, 400, msgNameRequired, "", ""},
		{"name too long", `{"name":"` + strings.Repeat("n", 257) + `"}`, 400, msgNameTooLong, "", ""},
		{"javascript image", `{"image":"javascript:alert(1)"}`, 400, msgInvalidImageURL, "", ""},
		{"relative image", `{"image":"/avatar.png"}`, 400, msgInvalidImageURL, "", ""},
		{"email is rejected", `{"name":"X","email":"x@example.com"}`, 400, msgEmailNotPatchable, "", ""},
		{"malformed JSON", `{"name":`, 400, msgInvalidJSON, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := app.do(t, req{method: http.MethodPatch, path: "/me", token: token, raw: tt.raw, ctype: "application/json"})
			if tt.code != http.StatusOK {
				expectError(t, rec, tt.code, tt.message)
				return
			}
			expectStatus(t, rec, http.StatusOK)
			var out meBody
			decode(t, rec, &out)
			if out.User.Name != tt.wantName || out.User.Image != tt.wantImage || out.User.Email != "ada@example.com" {
				t.Fatalf("got %+v", out.User)
			}
		})
	}

	expectError(t, app.do(t, req{method: http.MethodGet, path: "/me"}), http.StatusUnauthorized, msgMissingToken)
	expectError(t, app.do(t, req{method: http.MethodPatch, path: "/me", raw: `{"name":"x"}`, ctype: "application/json"}), http.StatusUnauthorized, msgMissingToken)
}

func TestDeleteMe(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	other := app.signIn(t, "ada@example.com", testPassword)

	failures := []struct {
		name    string
		r       req
		code    int
		message string
	}{
		{"no session", req{body: map[string]any{"password": testPassword}}, 401, msgMissingToken},
		{"wrong password", req{token: token, body: map[string]any{"password": "wrong-password"}}, 400, msgInvalidCurrentPassword},
		{"missing password", req{token: token, body: map[string]any{}}, 400, msgPasswordRequired},
		{"no body", req{token: token}, 415, msgUnsupportedMediaType},
		{"empty JSON body", req{token: token, ctype: "application/json"}, 400, msgInvalidJSON},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.method, tt.r.path = http.MethodDelete, "/me"
			expectError(t, app.do(t, tt.r), tt.code, tt.message)
		})
	}
	// The failures deleted nothing.
	expectStatus(t, app.do(t, req{method: http.MethodGet, path: "/me", token: token}), http.StatusOK)

	t.Run("success", func(t *testing.T) {
		rec := app.do(t, req{method: http.MethodDelete, path: "/me", token: token, body: map[string]any{"password": testPassword}})
		expectStatus(t, rec, http.StatusNoContent)
		for _, s := range []string{token, other} {
			expectError(t, app.do(t, req{method: http.MethodGet, path: "/me", token: s}), http.StatusUnauthorized, msgSessionNotFound)
		}
		expectError(t, app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
			"email": "ada@example.com", "password": testPassword,
		}}), http.StatusUnauthorized, msgInvalidCredentials)
		// The address is free again.
		app.signUp(t, "Ada", "ada@example.com", testPassword)
	})
}

func TestDeleteMeWithoutCredentialAccount(t *testing.T) {
	app := newTestApp(t, appOptions{})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	// Simulate a user who signed up through a social provider: no
	// password on file.
	accounts, err := app.store.ListAccountsByUser(t.Context(), app.userIDByEmail(t, "ada@example.com"))
	if err != nil || len(accounts) != 1 {
		t.Fatalf("ListAccountsByUser: %v, %d", err, len(accounts))
	}
	if err := app.store.DeleteAccount(t.Context(), accounts[0].ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	rec := app.do(t, req{method: http.MethodDelete, path: "/me", token: token, body: map[string]any{"password": testPassword}})
	expectError(t, rec, http.StatusConflict, msgAccountNotFound)
	rec = app.do(t, req{method: http.MethodPost, path: "/change-password", token: token, body: map[string]any{
		"current_password": testPassword, "new_password": "a-new-password",
	}})
	expectError(t, rec, http.StatusConflict, msgAccountNotFound)
}
