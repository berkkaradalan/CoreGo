package authhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
)

// TestOmitTokenFromBody covers CookieConfig.OmitTokenFromBody on both
// routes that create a session: the token stays out of the JSON body
// (and therefore out of reach of page scripts) while the cookie, which
// carries it, keeps working.
func TestOmitTokenFromBody(t *testing.T) {
	tests := []struct {
		name       string
		cookie     CookieConfig
		wantInBody bool
		wantCookie bool
	}{
		{name: "bearer only", cookie: CookieConfig{}, wantInBody: true},
		{name: "cookie, token also in body", cookie: CookieConfig{Enabled: true}, wantInBody: true, wantCookie: true},
		{name: "cookie only", cookie: CookieConfig{Enabled: true, OmitTokenFromBody: true}, wantCookie: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{httpCfg: Config{Cookie: tt.cookie}})

			signup := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
				"name": "Ada", "email": "ada@example.com", "password": testPassword,
			}})
			expectStatus(t, signup, http.StatusCreated)
			checkSessionBody(t, signup, tt.wantInBody, false)

			signin := app.do(t, req{method: http.MethodPost, path: "/signin", body: map[string]any{
				"email": "ada@example.com", "password": testPassword,
			}})
			expectStatus(t, signin, http.StatusOK)
			checkSessionBody(t, signin, tt.wantInBody, true)

			for name, rec := range map[string]*httptest.ResponseRecorder{"signup": signup, "signin": signin} {
				cookie := responseCookie(rec, DefaultCookieName)
				if (cookie != nil) != tt.wantCookie {
					t.Fatalf("%s: cookie present = %v, want %v", name, cookie != nil, tt.wantCookie)
				}
				if cookie == nil {
					continue
				}
				// The cookie alone authenticates.
				me := app.do(t, req{method: http.MethodGet, path: "/me", cookie: &http.Cookie{Name: cookie.Name, Value: cookie.Value}})
				expectStatus(t, me, http.StatusOK)
			}
		})
	}
}

// TestOmitTokenFromBodyWithRequiredVerification checks that the sign-up
// response without a session is unchanged by the option.
func TestOmitTokenFromBodyWithRequiredVerification(t *testing.T) {
	app := newTestApp(t, appOptions{
		authCfg: func(c *auth.Config) { c.RequireVerifiedEmail = true },
		httpCfg: Config{Cookie: CookieConfig{Enabled: true, OmitTokenFromBody: true}},
	})
	rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": testPassword,
	}})
	expectStatus(t, rec, http.StatusCreated)
	var top map[string]json.RawMessage
	decode(t, rec, &top)
	for _, absent := range []string{"token", "expires_at"} {
		if _, ok := top[absent]; ok {
			t.Fatalf("%q must be omitted: %s", absent, rec.Body)
		}
	}
	if string(top["verification_required"]) != "true" || top["user"] == nil {
		t.Fatalf("unexpected body: %s", rec.Body)
	}
	if len(rec.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("no cookie may be set without a session")
	}
}

// checkSessionBody asserts the session fields of a sign-up or sign-in
// body: token present or absent as wanted, expires_at and user always
// present, verification_required false on sign-up.
func checkSessionBody(t *testing.T, rec *httptest.ResponseRecorder, wantToken, signIn bool) {
	t.Helper()
	var top map[string]json.RawMessage
	decode(t, rec, &top)
	if _, ok := top["token"]; ok != wantToken {
		t.Fatalf("token in body = %v, want %v: %v", ok, wantToken, top)
	}
	if wantToken {
		var token string
		if err := json.Unmarshal(top["token"], &token); err != nil || token == "" {
			t.Fatalf("token = %s, want a non-empty string", top["token"])
		}
	}
	for _, field := range []string{"expires_at", "user"} {
		if _, ok := top[field]; !ok {
			t.Fatalf("%q missing from the body: %v", field, top)
		}
	}
	if !signIn && string(top["verification_required"]) != "false" {
		t.Fatalf("verification_required = %s, want false", top["verification_required"])
	}
}
