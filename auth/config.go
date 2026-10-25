package auth

import (
	"fmt"
	"log/slog"
	"time"
)

// Default configuration values applied by New when the corresponding
// Config field is left zero.
const (
	// DefaultSessionTTL is the lifetime of a regular session.
	DefaultSessionTTL = 7 * 24 * time.Hour

	// DefaultRememberMeTTL is the lifetime of a session created with the
	// remember me flag set.
	DefaultRememberMeTTL = 30 * 24 * time.Hour

	// DefaultEmailVerificationTokenTTL is the lifetime of an email
	// verification token.
	DefaultEmailVerificationTokenTTL = 24 * time.Hour

	// DefaultPasswordResetTokenTTL is the lifetime of a password reset
	// token.
	DefaultPasswordResetTokenTTL = time.Hour

	// DefaultMinPasswordLength is the minimum password length in bytes
	// enforced by the default password policy.
	DefaultMinPasswordLength = 8

	// DefaultMaxPendingEmails is the maximum number of background
	// email jobs (token creation and delivery) in flight at once.
	DefaultMaxPendingEmails = 100
)

// Config configures an Auth service. Every field is optional: New
// replaces zero values with the package defaults listed above.
type Config struct {
	// SessionTTL is how long a session stays valid. Default 7 days.
	SessionTTL time.Duration

	// RememberMeTTL is how long a session created with RememberMe set
	// stays valid. Default 30 days.
	RememberMeTTL time.Duration

	// EmailVerificationTokenTTL is how long an email verification token
	// stays valid. Default 24 hours.
	EmailVerificationTokenTTL time.Duration

	// PasswordResetTokenTTL is how long a password reset token stays
	// valid. Default 1 hour.
	PasswordResetTokenTTL time.Duration

	// RequireVerifiedEmail makes a verified email address mandatory for
	// being signed in: SignUp creates no session, and SignIn and
	// GetSession return ErrEmailNotVerified for unverified users (this
	// also covers sessions that existed before an email change).
	// Default false. Enable it together with a Mailer, otherwise users
	// have no way to verify their address.
	RequireVerifiedEmail bool

	// MinPasswordLength is the minimum password length in bytes.
	// Default 8. bcrypt additionally caps passwords at 72 bytes; longer
	// passwords are rejected with ErrWeakPassword.
	MinPasswordLength int

	// Mailer sends verification and password reset emails. When nil,
	// no email is ever sent: SignUp and ChangeEmail skip the
	// verification email, RequestEmailVerification and
	// RequestPasswordReset return nil without doing anything, and only
	// the explicit SendVerificationEmail reports ErrMailerNotConfigured.
	Mailer Mailer

	// MaxPendingEmails bounds the background email jobs in flight at
	// once (verification and password reset emails, including the
	// token creation of the public request flows). When the limit is
	// reached, a new job is dropped instead of queued: the request
	// that triggered it is never blocked or failed, the drop is logged
	// at Warn level, and the user can simply ask for the email again.
	// This keeps a hanging or slow mail backend from piling up one
	// goroutine per request. Default 100; negative values are
	// rejected.
	MaxPendingEmails int

	// Logger receives background email delivery failures and
	// configuration warnings. nil selects slog.Default() at the time of
	// logging.
	Logger *slog.Logger
}

// validate reports whether the explicitly set fields are usable.
func (cfg Config) validate() error {
	if cfg.SessionTTL < 0 || cfg.RememberMeTTL < 0 ||
		cfg.EmailVerificationTokenTTL < 0 || cfg.PasswordResetTokenTTL < 0 {
		return fmt.Errorf("%w: durations must not be negative", ErrInvalidConfig)
	}
	if cfg.MinPasswordLength < 0 {
		return fmt.Errorf("%w: MinPasswordLength must not be negative", ErrInvalidConfig)
	}
	if cfg.MaxPendingEmails < 0 {
		return fmt.Errorf("%w: MaxPendingEmails must not be negative", ErrInvalidConfig)
	}
	return nil
}

// withDefaults returns a copy of cfg with zero values replaced by the
// package defaults.
func (cfg Config) withDefaults() Config {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.RememberMeTTL == 0 {
		cfg.RememberMeTTL = DefaultRememberMeTTL
	}
	if cfg.EmailVerificationTokenTTL == 0 {
		cfg.EmailVerificationTokenTTL = DefaultEmailVerificationTokenTTL
	}
	if cfg.PasswordResetTokenTTL == 0 {
		cfg.PasswordResetTokenTTL = DefaultPasswordResetTokenTTL
	}
	if cfg.MinPasswordLength == 0 {
		cfg.MinPasswordLength = DefaultMinPasswordLength
	}
	if cfg.MaxPendingEmails == 0 {
		cfg.MaxPendingEmails = DefaultMaxPendingEmails
	}
	return cfg
}
