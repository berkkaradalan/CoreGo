package corego

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/mail"
)

// authReadyAttrs returns the key=value attributes of the
// "corego: auth ready" line in a text handler log, and whether the line
// was logged. The values the facade logs never contain spaces.
func authReadyAttrs(t *testing.T, logs string) (map[string]string, bool) {
	t.Helper()
	const marker = `msg="corego: auth ready"`
	var found map[string]string
	for _, line := range strings.Split(logs, "\n") {
		_, rest, ok := strings.Cut(line, marker)
		if !ok {
			continue
		}
		if found != nil {
			t.Fatalf("auth ready logged more than once:\n%s", logs)
		}
		if !strings.Contains(line, "level=INFO") {
			t.Errorf("auth ready line is not at Info level: %s", line)
		}
		found = map[string]string{}
		for _, field := range strings.Fields(rest) {
			key, value, _ := strings.Cut(field, "=")
			found[key] = value
		}
	}
	return found, found != nil
}

// mustAuthReadyAttrs is authReadyAttrs that fails the test when the line
// is missing.
func mustAuthReadyAttrs(t *testing.T, logs string) map[string]string {
	t.Helper()
	attrs, ok := authReadyAttrs(t, logs)
	if !ok {
		t.Fatalf("no auth ready line logged; logs:\n%s", logs)
	}
	return attrs
}

func TestRequireVerifiedEmailFromEnv(t *testing.T) {
	tests := []struct {
		name string
		code bool
		env  string
		want bool
	}{
		{name: "off by default"},
		{name: "environment turns it on", env: "true", want: true},
		{name: "environment accepts 1", env: "1", want: true},
		{name: "environment value is trimmed and case insensitive", env: " TRUE ", want: true},
		{name: "explicit false keeps it off", env: "false"},
		{name: "code turns it on", code: true, want: true},
		{name: "environment cannot turn the code setting off", code: true, env: "false", want: true},
		{name: "both on", code: true, env: "t", want: true},
		{name: "whitespace only counts as unset", env: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_REQUIRE_VERIFIED_EMAIL", tt.env)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{
				Auth:   &AuthOptions{Service: auth.Config{RequireVerifiedEmail: tt.code}},
				Logger: logger,
			})

			w := do(t, core.Server(), request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
				"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
			}})
			if w.Code != http.StatusCreated {
				t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
			}
			var out struct {
				Token                string `json:"token"`
				VerificationRequired bool   `json:"verification_required"`
			}
			decode(t, w, &out)
			if out.VerificationRequired != tt.want {
				t.Errorf("verification_required = %v, want %v", out.VerificationRequired, tt.want)
			}
			if gotToken := out.Token != ""; gotToken == tt.want {
				t.Errorf("signup returned a token = %v with verification required = %v", gotToken, tt.want)
			}
			if got := mustAuthReadyAttrs(t, logs.String())["require_verified_email"]; got != boolString(tt.want) {
				t.Errorf("logged require_verified_email = %q, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthCookieFromEnv(t *testing.T) {
	tests := []struct {
		name string
		code bool
		env  string
		want bool
	}{
		{name: "off by default"},
		{name: "environment turns it on", env: "true", want: true},
		{name: "explicit false keeps it off", env: "0"},
		{name: "code turns it on", code: true, want: true},
		{name: "environment cannot turn the code setting off", code: true, env: "false", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_AUTH_COOKIE", tt.env)
			logger, logs := newTestLogger()
			core := newCore(t, &Config{
				Auth:   &AuthOptions{HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: tt.code}}},
				Logger: logger,
			})

			w := do(t, core.Server(), request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
				"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
			}})
			if w.Code != http.StatusCreated {
				t.Fatalf("signup status = %d, body %s", w.Code, w.Body.String())
			}
			gotCookie := false
			for _, c := range w.Result().Cookies() {
				if c.Name == authhttp.DefaultCookieName && c.Value != "" {
					gotCookie = true
				}
			}
			if gotCookie != tt.want {
				t.Errorf("session cookie set = %v, want %v", gotCookie, tt.want)
			}
			if got := mustAuthReadyAttrs(t, logs.String())["cookie"]; got != boolString(tt.want) {
				t.Errorf("logged cookie = %q, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvCookieGetsSecureOnHTTPSAppURL(t *testing.T) {
	isolateEnv(t)
	t.Setenv("APP_URL", "https://api.example.com")
	t.Setenv("COREGO_AUTH_COOKIE", "true")
	logger, logs := newTestLogger()
	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})

	w := do(t, core.Server(), request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
	}})
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("cookies = %+v, want one Secure session cookie", cookies)
	}
	if strings.Contains(logs.String(), "session cookie is not Secure") {
		t.Errorf("insecure cookie warning logged for an https APP_URL; logs:\n%s", logs)
	}
}

func TestEnvCookieInsecureWarning(t *testing.T) {
	isolateEnv(t)
	t.Setenv("APP_URL", "http://api.example.com")
	t.Setenv("COREGO_AUTH_COOKIE", "true")
	logger, logs := newTestLogger()
	newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	if !strings.Contains(logs.String(), "session cookie is not Secure") {
		t.Errorf("no insecure cookie warning for a cookie enabled through the environment; logs:\n%s", logs)
	}
}

func TestInvalidAuthEnvValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "verified email yes", key: "COREGO_REQUIRE_VERIFIED_EMAIL", value: "yes",
			wantErr: `corego: invalid COREGO_REQUIRE_VERIFIED_EMAIL "yes": valid values are true or false`},
		{name: "verified email on", key: "COREGO_REQUIRE_VERIFIED_EMAIL", value: "on",
			wantErr: `invalid COREGO_REQUIRE_VERIFIED_EMAIL "on"`},
		{name: "verified email 2", key: "COREGO_REQUIRE_VERIFIED_EMAIL", value: "2",
			wantErr: `invalid COREGO_REQUIRE_VERIFIED_EMAIL "2"`},
		{name: "cookie typo", key: "COREGO_AUTH_COOKIE", value: "ture",
			wantErr: `corego: invalid COREGO_AUTH_COOKIE "ture": valid values are true or false`},
		{name: "unknown mail mode", key: "COREGO_MAIL", value: "sendgrid",
			wantErr: `corego: invalid COREGO_MAIL "sendgrid": valid values are "smtp", "log", "none" or empty for the automatic selection`},
		{name: "mail mode with a typo", key: "COREGO_MAIL", value: "logs",
			wantErr: `invalid COREGO_MAIL "logs"`},
		{name: "mail mode auto is not a value", key: "COREGO_MAIL", value: "auto",
			wantErr: `invalid COREGO_MAIL "auto"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv(tt.key, tt.value)
			logger, logs := newTestLogger()

			core, err := New(&Config{Auth: &AuthOptions{}, Logger: logger})
			if err == nil {
				core.Close()
				t.Fatalf("New accepted %s=%q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			if _, ok := authReadyAttrs(t, logs.String()); ok {
				t.Errorf("auth ready logged although New failed")
			}
		})
	}
}

// TestInvalidAuthEnvValidatedDespiteCode checks that a code setting that
// makes a variable irrelevant does not hide a typo in it.
func TestInvalidAuthEnvValidatedDespiteCode(t *testing.T) {
	tests := []struct {
		name string
		key  string
		opts *AuthOptions
	}{
		{name: "RequireVerifiedEmail set in code", key: "COREGO_REQUIRE_VERIFIED_EMAIL",
			opts: &AuthOptions{Service: auth.Config{RequireVerifiedEmail: true}}},
		{name: "cookie enabled in code", key: "COREGO_AUTH_COOKIE",
			opts: &AuthOptions{HTTP: authhttp.Config{Cookie: authhttp.CookieConfig{Enabled: true}}}},
		{name: "MailSender set in code", key: "COREGO_MAIL",
			opts: &AuthOptions{MailSender: mail.NewCaptureSender()}},
		{name: "Service.Mailer set in code", key: "COREGO_MAIL",
			opts: &AuthOptions{Service: auth.Config{Mailer: recordingMailer{to: make(chan string, 1)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv(tt.key, "maybe")
			core, err := New(&Config{Auth: tt.opts})
			if err == nil {
				core.Close()
				t.Fatalf("New accepted %s=maybe", tt.key)
			}
			if !strings.Contains(err.Error(), "invalid "+tt.key) {
				t.Errorf("error = %q, want it to name %s", err, tt.key)
			}
		})
	}
}

func TestAuthEnvIgnoredWithoutAuth(t *testing.T) {
	isolateEnv(t)
	for _, key := range []string{"COREGO_REQUIRE_VERIFIED_EMAIL", "COREGO_AUTH_COOKIE", "COREGO_MAIL", "COREGO_AUTH_TABLE_PREFIX"} {
		t.Setenv(key, "Not Valid!")
	}
	logger, logs := newTestLogger()
	newCore(t, &Config{Logger: logger})
	if _, ok := authReadyAttrs(t, logs.String()); ok {
		t.Error("auth ready logged without auth")
	}
}

func TestTablePrefixFromEnvIgnoredByMemoryStore(t *testing.T) {
	isolateEnv(t)
	t.Setenv("COREGO_AUTH_TABLE_PREFIX", "Not Valid!")
	logger, logs := newTestLogger()
	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	if core.Auth == nil {
		t.Fatal("auth service not started")
	}
	if _, ok := mustAuthReadyAttrs(t, logs.String())["table_prefix"]; ok {
		t.Error("table_prefix logged for the memory store")
	}
}

func TestMailModeFromEnv(t *testing.T) {
	const (
		noMailerWarning = "auth emails are disabled"
		logModeWarning  = "COREGO_MAIL=log: auth emails are written to the log instead of being sent"
	)
	tests := []struct {
		name     string
		mode     string
		smtpHost string
		sender   bool
		mailer   bool
		wantKind string
		wantWarn string // a warning that must be logged; "" means neither
		wantErr  string
	}{
		{name: "automatic without SMTP", wantKind: "none", wantWarn: noMailerWarning},
		{name: "automatic with SMTP_HOST", smtpHost: "smtp.invalid", wantKind: "smtp"},
		{name: "smtp with SMTP_HOST", mode: "smtp", smtpHost: "smtp.invalid", wantKind: "smtp"},
		{name: "smtp without SMTP_HOST is an error", mode: "smtp",
			wantErr: "corego: COREGO_MAIL=smtp needs SMTP_HOST"},
		{name: "log", mode: "log", wantKind: "log", wantWarn: logModeWarning},
		{name: "log wins over SMTP_HOST", mode: "log", smtpHost: "smtp.invalid", wantKind: "log", wantWarn: logModeWarning},
		{name: "mode is case insensitive and trimmed", mode: " LOG ", wantKind: "log", wantWarn: logModeWarning},
		{name: "none is silent", mode: "none", wantKind: "none"},
		{name: "none wins over SMTP_HOST", mode: "none", smtpHost: "smtp.invalid", wantKind: "none"},
		{name: "MailSender wins over log", mode: "log", sender: true, wantKind: "custom"},
		{name: "MailSender wins over none", mode: "none", sender: true, wantKind: "custom"},
		{name: "MailSender wins over smtp without SMTP_HOST", mode: "smtp", sender: true, wantKind: "custom"},
		{name: "Service.Mailer wins over log", mode: "log", mailer: true, wantKind: "custom"},
		{name: "Service.Mailer wins over smtp without SMTP_HOST", mode: "smtp", mailer: true, wantKind: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_MAIL", tt.mode)
			t.Setenv("SMTP_HOST", tt.smtpHost)
			logger, logs := newTestLogger()
			opts := &AuthOptions{}
			if tt.sender {
				opts.MailSender = mail.NewCaptureSender()
			}
			if tt.mailer {
				opts.Service.Mailer = recordingMailer{to: make(chan string, 4)}
			}

			core, err := New(&Config{Auth: opts, Logger: logger})
			if tt.wantErr != "" {
				if err == nil {
					core.Close()
					t.Fatalf("New succeeded, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { core.Close() })

			if got := mustAuthReadyAttrs(t, logs.String())["mail"]; got != tt.wantKind {
				t.Errorf("logged mail = %q, want %q", got, tt.wantKind)
			}
			for _, warning := range []string{noMailerWarning, logModeWarning} {
				if got, want := strings.Contains(logs.String(), warning), warning == tt.wantWarn; got != want {
					t.Errorf("warning %q logged = %v, want %v; logs:\n%s", warning, got, want, logs)
				}
			}
		})
	}
}

// TestLogMailEndToEnd configures mandatory email verification and the
// development mailer through the environment alone, then reads the
// verification link from an Info level log (the default slog level),
// follows it and signs in.
func TestLogMailEndToEnd(t *testing.T) {
	isolateEnv(t)
	t.Setenv("COREGO_MAIL", "log")
	t.Setenv("COREGO_REQUIRE_VERIFIED_EMAIL", "true")
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil)) // Info level

	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	app := core.Server()

	w := do(t, app, request{method: http.MethodPost, path: "/auth/signup", body: map[string]any{
		"name": "Ada", "email": "ada@example.com", "password": "analytical-engine",
	}})
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"verification_required":true`) {
		t.Fatalf("signup = %d, body %s", w.Code, w.Body.String())
	}
	signIn := func() int {
		return do(t, app, request{method: http.MethodPost, path: "/auth/signin", body: map[string]any{
			"email": "ada@example.com", "password": "analytical-engine",
		}}).Code
	}
	if code := signIn(); code != http.StatusForbidden {
		t.Fatalf("signin before verification = %d, want 403", code)
	}
	if err := core.Auth.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	link := loggedEmailLink(t, logs.String())
	if got := link.Scheme + "://" + link.Host + link.Path; got != "http://localhost:8080/auth/verify-email" {
		t.Fatalf("logged link target = %q", got)
	}
	w = do(t, app, request{method: http.MethodGet, path: link.RequestURI()})
	if w.Code != http.StatusOK {
		t.Fatalf("GET logged link = %d, body %s", w.Code, w.Body.String())
	}
	if code := signIn(); code != http.StatusOK {
		t.Fatalf("signin after verification = %d, want 200", code)
	}
}

// loggedEmailLink returns the link of the last email mail.LogSender
// wrote to a JSON log: the "links" attribute of its Info summary.
func loggedEmailLink(t *testing.T, logs string) *url.URL {
	t.Helper()
	var raw string
	scanner := bufio.NewScanner(strings.NewReader(logs))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var record struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
			Links string `json:"links"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if record.Msg == "mail: outgoing message" {
			if record.Level != "INFO" {
				t.Errorf("email summary logged at %s, want INFO", record.Level)
			}
			raw = record.Links
		}
	}
	if raw == "" {
		t.Fatalf("no email link in the log:\n%s", logs)
	}
	if strings.Contains(raw, " ") {
		t.Fatalf("logged links = %q, want exactly one", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse link %q: %v", raw, err)
	}
	return u
}

func TestAuthReadyLog(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		opts  *AuthOptions
		want  map[string]string
		noKey string // an attribute that must not be logged
	}{
		{
			name:  "defaults",
			opts:  &AuthOptions{},
			want:  map[string]string{"store": "memory", "base_path": "/auth", "mail": "none", "require_verified_email": "false", "cookie": "false"},
			noKey: "table_prefix",
		},
		{
			name: "custom store, root base path, custom mailer",
			opts: &AuthOptions{
				CustomStore: auth.NewMemoryStore(),
				Service:     auth.Config{Mailer: recordingMailer{to: make(chan string, 1)}},
				HTTP:        authhttp.Config{BasePath: "/"},
			},
			want:  map[string]string{"store": "custom", "base_path": "/", "mail": "custom"},
			noKey: "table_prefix",
		},
		{
			name: "nested base path and MailSender",
			opts: &AuthOptions{MailSender: mail.NewCaptureSender(), HTTP: authhttp.Config{BasePath: "/api/v1/auth/"}},
			want: map[string]string{"base_path": "/api/v1/auth", "mail": "custom"},
		},
		{
			name: "everything from the environment",
			env: map[string]string{
				"COREGO_AUTH_STORE": "memory", "COREGO_MAIL": "log",
				"COREGO_REQUIRE_VERIFIED_EMAIL": "true", "COREGO_AUTH_COOKIE": "true",
			},
			opts: &AuthOptions{},
			want: map[string]string{"store": "memory", "mail": "log", "require_verified_email": "true", "cookie": "true"},
		},
		{
			name: "SMTP from the environment",
			env:  map[string]string{"SMTP_HOST": "smtp.invalid"},
			opts: &AuthOptions{},
			want: map[string]string{"mail": "smtp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			logger, logs := newTestLogger()
			newCore(t, &Config{Auth: tt.opts, Logger: logger})

			attrs := mustAuthReadyAttrs(t, logs.String())
			for key, want := range tt.want {
				if attrs[key] != want {
					t.Errorf("%s = %q, want %q (line attrs %v)", key, attrs[key], want, attrs)
				}
			}
			if tt.noKey != "" {
				if _, ok := attrs[tt.noKey]; ok {
					t.Errorf("%s logged: %v", tt.noKey, attrs)
				}
			}
		})
	}
}

// TestConfigEnvIsUsedInsteadOfDotEnv runs New in a directory whose .env
// is malformed: with Config.Env set, New must not read it and must take
// every setting from the given snapshot.
func TestConfigEnvIsUsedInsteadOfDotEnv(t *testing.T) {
	isolateEnv(t)
	if err := os.WriteFile(".env", []byte("BROKEN=\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.env")
	if err := os.WriteFile(path, []byte("PORT=4321\nCOREGO_REQUIRE_VERIFIED_EMAIL=true\nCOREGO_MAIL=none\nWORKERS=8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// isolateEnv sets these to "", which would win over the file.
	for _, key := range []string{"PORT", "COREGO_REQUIRE_VERIFIED_EMAIL", "COREGO_MAIL"} {
		os.Unsetenv(key)
	}
	e, err := env.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logger, logs := newTestLogger()

	core := newCore(t, &Config{Env: e, Auth: &AuthOptions{}, Logger: logger})
	if core.Env != e {
		t.Error("Core.Env is not the Config.Env snapshot")
	}
	if core.Env.Port != "4321" || core.Env.Int("WORKERS", 0) != 8 {
		t.Errorf("Env.Port = %q, WORKERS = %d; want the values of the given snapshot", core.Env.Port, core.Env.Int("WORKERS", 0))
	}
	attrs := mustAuthReadyAttrs(t, logs.String())
	if attrs["require_verified_email"] != "true" || attrs["mail"] != "none" {
		t.Errorf("auth settings not taken from Config.Env: %v", attrs)
	}
	if strings.Contains(logs.String(), "auth emails are disabled") {
		t.Errorf("COREGO_MAIL=none from Config.Env did not silence the warning; logs:\n%s", logs)
	}

	// Without Config.Env the same directory fails, proving the snapshot
	// above was not merged with .env.
	if core, err := New(&Config{Auth: &AuthOptions{}}); err == nil {
		core.Close()
		t.Fatal("New without Config.Env accepted the malformed .env")
	}
}

// boolString formats b like the text handler does.
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
