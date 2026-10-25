package auth

import (
	"context"
	"errors"
)

// SignInInput holds the data required to authenticate with an email
// address and a password.
type SignInInput struct {
	// Email is the login address; it is normalized before lookup.
	Email string
	// Password is the account password.
	Password string
	// RememberMe extends the created session's lifetime from
	// SessionTTL to RememberMeTTL.
	RememberMe bool
	// IPAddress and UserAgent are recorded on the session for
	// auditing.
	IPAddress string
	UserAgent string
}

// SignInOutput is the result of a successful sign-in. See
// SignUpOutput for the field semantics.
type SignInOutput struct {
	// User is the authenticated user record.
	User *User
	// SessionToken is the raw opaque session token, returned exactly
	// once.
	SessionToken string
	// Session is the stored session record.
	Session *Session
}

// SignIn authenticates a user with an email address and a password and
// starts a new session.
//
// Every credential failure - unknown email, missing credential account
// or wrong password - returns ErrInvalidCredentials and never reveals
// which check failed. When the email is unknown a bcrypt comparison
// against a precomputed dummy hash still runs so response timing does
// not reveal whether the address is registered.
//
// Only after the password matched: with RequireVerifiedEmail enabled,
// unverified users are rejected with ErrEmailNotVerified, and banned
// users are rejected with ErrUserBanned (a ban whose BanExpires lies
// in the past no longer blocks sign-in). Running these checks after
// the password check means they never reveal anything about an
// account to someone who does not know its password.
func (a *Auth) SignIn(ctx context.Context, in SignInInput) (*SignInOutput, error) {
	email := normalizeEmail(in.Email)

	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			dummyCompare(in.Password)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// The account is resolved through the user so that a provider
	// account id lagging behind a recent email change cannot lock the
	// user out.
	account, err := a.store.GetAccountByUser(ctx, user.ID, CredentialProvider)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			dummyCompare(in.Password)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if account.Password == "" {
		// Credential accounts always hold a bcrypt hash; treat anything
		// else as a credential mismatch with timing equalized.
		dummyCompare(in.Password)
		return nil, ErrInvalidCredentials
	}
	if !VerifyPassword(account.Password, in.Password) {
		return nil, ErrInvalidCredentials
	}

	if a.cfg.RequireVerifiedEmail && !user.EmailVerified {
		return nil, ErrEmailNotVerified
	}
	if user.IsBannedAt(a.now()) {
		return nil, ErrUserBanned
	}

	sessionToken, session, err := a.createSession(ctx, user.ID, in.RememberMe, in.IPAddress, in.UserAgent)
	if err != nil {
		return nil, err
	}
	return &SignInOutput{
		User:         user,
		SessionToken: sessionToken,
		Session:      session,
	}, nil
}
