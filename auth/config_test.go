package auth

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewRejectsNilStore(t *testing.T) {
	a, err := New(Config{}, nil)
	if a != nil {
		t.Fatal("New(Config{}, nil) returned a non-nil Auth")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	a, err := New(Config{}, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := Config{
		SessionTTL:                DefaultSessionTTL,
		RememberMeTTL:             DefaultRememberMeTTL,
		EmailVerificationTokenTTL: DefaultEmailVerificationTokenTTL,
		PasswordResetTokenTTL:     DefaultPasswordResetTokenTTL,
		MinPasswordLength:         DefaultMinPasswordLength,
	}
	got := a.cfg
	if got.SessionTTL != want.SessionTTL {
		t.Errorf("SessionTTL = %v, want %v", got.SessionTTL, want.SessionTTL)
	}
	if got.RememberMeTTL != want.RememberMeTTL {
		t.Errorf("RememberMeTTL = %v, want %v", got.RememberMeTTL, want.RememberMeTTL)
	}
	if got.EmailVerificationTokenTTL != want.EmailVerificationTokenTTL {
		t.Errorf("EmailVerificationTokenTTL = %v, want %v", got.EmailVerificationTokenTTL, want.EmailVerificationTokenTTL)
	}
	if got.PasswordResetTokenTTL != want.PasswordResetTokenTTL {
		t.Errorf("PasswordResetTokenTTL = %v, want %v", got.PasswordResetTokenTTL, want.PasswordResetTokenTTL)
	}
	if got.MinPasswordLength != want.MinPasswordLength {
		t.Errorf("MinPasswordLength = %d, want %d", got.MinPasswordLength, want.MinPasswordLength)
	}
	if got.MaxPendingEmails != DefaultMaxPendingEmails || DefaultMaxPendingEmails != 100 {
		t.Errorf("MaxPendingEmails = %d, want %d (100)", got.MaxPendingEmails, DefaultMaxPendingEmails)
	}
	if got.RequireVerifiedEmail {
		t.Error("RequireVerifiedEmail default = true, want false")
	}
	if got.Mailer != nil {
		t.Error("Mailer default = non-nil, want nil")
	}
	if got.Logger != nil {
		t.Error("Logger default = non-nil, want nil (slog.Default is resolved at use time)")
	}
	if a.now == nil {
		t.Error("now clock was not installed")
	}
}

func TestNewPreservesExplicitValues(t *testing.T) {
	cfg := Config{
		SessionTTL:                time.Hour,
		RememberMeTTL:             2 * time.Hour,
		EmailVerificationTokenTTL: 3 * time.Hour,
		PasswordResetTokenTTL:     4 * time.Hour,
		RequireVerifiedEmail:      true,
		MinPasswordLength:         12,
		MaxPendingEmails:          7,
		Mailer:                    &recordingMailer{},
		Logger:                    slog.New(slog.DiscardHandler),
	}
	a, err := New(cfg, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg != cfg {
		t.Errorf("cfg = %+v, want %+v", a.cfg, cfg)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"negative session ttl", Config{SessionTTL: -time.Second}},
		{"negative remember me ttl", Config{RememberMeTTL: -time.Second}},
		{"negative max pending emails", Config{MaxPendingEmails: -1}},
		{"negative verification ttl", Config{EmailVerificationTokenTTL: -time.Second}},
		{"negative reset ttl", Config{PasswordResetTokenTTL: -time.Second}},
		{"negative min password length", Config{MinPasswordLength: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := New(tt.cfg, NewMemoryStore())
			if a != nil {
				t.Fatal("New returned a non-nil Auth for invalid config")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestConfigWithDefaultsZeroValuesOnly(t *testing.T) {
	cfg := Config{SessionTTL: time.Hour}.withDefaults()
	if cfg.SessionTTL != time.Hour {
		t.Errorf("SessionTTL = %v, want the explicit 1h", cfg.SessionTTL)
	}
	if cfg.RememberMeTTL != DefaultRememberMeTTL {
		t.Errorf("RememberMeTTL = %v, want %v", cfg.RememberMeTTL, DefaultRememberMeTTL)
	}
}

func TestLoggerFallsBackToDefaultAtUseTime(t *testing.T) {
	a, err := New(Config{}, NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	original := slog.Default()
	defer slog.SetDefault(original)

	replacement, logs := newLogCapture()
	slog.SetDefault(replacement)
	a.logger().Info("probe")
	if !strings.Contains(logs.String(), "probe") {
		t.Errorf("logger() did not use the default installed after New:\n%s", logs.String())
	}

	own, ownLogs := newLogCapture()
	a.cfg.Logger = own
	a.logger().Info("own")
	if !strings.Contains(ownLogs.String(), "own") || strings.Contains(logs.String(), "own") {
		t.Error("logger() did not prefer Config.Logger")
	}
}
