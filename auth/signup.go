package auth

import (
	"context"
	"errors"
)

// CredentialProvider is the provider name used for email and password
// (credential) accounts.
const CredentialProvider = "credential"

// SignUpInput holds the data required to register a new user with an
// email address and a password.
type SignUpInput struct {
	// Name is the display name. Required; surrounding whitespace is
	// trimmed and at most 256 characters may remain.
	Name string
	// Email is the login address. Required; it is normalized to
	// lowercase and validated.
	Email string
	// Password must satisfy the configured password policy. Required.
	Password string
	// Image is an optional avatar URL stored on the user. Surrounding
	// whitespace is trimmed; a non-empty value must be an absolute
	// http or https URL of at most 2048 bytes.
	Image string
	// RememberMe extends the created session's lifetime from
	// SessionTTL to RememberMeTTL.
	RememberMe bool
	// IPAddress and UserAgent are recorded on the session for
	// auditing.
	IPAddress string
	UserAgent string
}

// SignUpOutput is the result of a successful registration.
type SignUpOutput struct {
	// User is the created user record.
	User *User
	// SessionToken is the raw opaque session token. It is returned
	// exactly once; only its SHA-256 hash is stored, so the caller must
	// persist it client side (for example in a cookie). It is empty
	// when Config.RequireVerifiedEmail is enabled: the user signs in
	// after verifying the address.
	SessionToken string
	// Session is the stored session record; it does not contain the
	// raw token. It is nil when Config.RequireVerifiedEmail is enabled.
	Session *Session
}

// SignUp registers a user with a credential account and, unless
// Config.RequireVerifiedEmail is enabled, starts a session.
//
// Input validation failures (ErrNameRequired, ErrNameTooLong,
// ErrInvalidImageURL, ErrEmailRequired, ErrInvalidEmail,
// ErrPasswordRequired, ErrWeakPassword) are returned before anything
// is persisted. An already registered email yields
// ErrUserAlreadyExists, including when a concurrent registration wins
// the race (the store's ErrDuplicate is mapped). When the credential
// account cannot be created the half-created user is removed again.
//
// With RequireVerifiedEmail enabled no session is created:
// SignUpOutput.SessionToken is empty and Session is nil, and the user
// signs in after verifying the address.
//
// When a mailer is configured, a single use verification token is
// persisted and the verification email is delivered in the
// background: SignUp neither waits for nor reports the delivery.
// Failures are logged through Config.Logger and the user can ask for
// a new email with RequestEmailVerification or SendVerificationEmail.
// When Config.MaxPendingEmails emails are already in flight, the email
// (and its token) is skipped and the drop is logged at Warn level; the
// registration itself still succeeds.
func (a *Auth) SignUp(ctx context.Context, in SignUpInput) (*SignUpOutput, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return nil, err
	}
	image, err := normalizeImage(in.Image)
	if err != nil {
		return nil, err
	}
	email, err := normalizeAndValidateEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := a.validatePassword(in.Password); err != nil {
		return nil, err
	}

	// Fast duplicate check; the create below still maps ErrDuplicate so
	// concurrent registrations are handled correctly.
	if _, err := a.store.GetUserByEmail(ctx, email); err == nil {
		return nil, ErrUserAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	passwordHash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	now := a.now()
	user := &User{
		Name:      name,
		Email:     email,
		Image:     image,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.store.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	account := &Account{
		UserID:            user.ID,
		Provider:          CredentialProvider,
		ProviderAccountID: email,
		Password:          passwordHash,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := a.store.CreateAccount(ctx, account); err != nil {
		// Never leave a user without credentials behind: it would block
		// the address for good. The removal is best effort; the
		// original error is what the caller needs to see.
		_ = a.store.DeleteUser(ctx, user.ID)
		if errors.Is(err, ErrDuplicate) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	out := &SignUpOutput{User: user}
	if !a.cfg.RequireVerifiedEmail {
		token, session, err := a.createSession(ctx, user.ID, in.RememberMe, in.IPAddress, in.UserAgent)
		if err != nil {
			return nil, err
		}
		out.SessionToken = token
		out.Session = session
	}

	if a.cfg.Mailer != nil {
		// The account exists at this point, so a failure to queue the
		// email must not fail the registration; the user can request a
		// new email later.
		if err := a.queueVerificationEmail(ctx, user); err != nil {
			a.logger().ErrorContext(ctx, "auth: could not issue the sign-up verification token",
				"user_id", user.ID, "error", err.Error())
		}
	}
	return out, nil
}
