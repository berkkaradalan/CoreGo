package auth

import "errors"

// Sentinel errors returned by the auth service. Callers must compare
// with errors.Is instead of matching error strings.
var (
	// ErrInvalidConfig is returned by New when the configuration or the
	// store is not usable.
	ErrInvalidConfig = errors.New("auth: invalid configuration")

	// ErrNameRequired is returned when a signup omits the display name.
	ErrNameRequired = errors.New("auth: name is required")

	// ErrNameTooLong is returned by SignUp and UpdateUser when the
	// display name is longer than 256 characters (Unicode code points,
	// counted after trimming surrounding whitespace).
	ErrNameTooLong = errors.New("auth: name is too long")

	// ErrInvalidImageURL is returned by SignUp and UpdateUser when the
	// image is neither empty nor an absolute http or https URL (with a
	// host) of at most 2048 bytes, measured after trimming surrounding
	// whitespace.
	ErrInvalidImageURL = errors.New("auth: image must be an absolute http or https URL")

	// ErrEmailRequired is returned when an email address is required but
	// missing.
	ErrEmailRequired = errors.New("auth: email is required")

	// ErrInvalidEmail is returned when an email address is malformed.
	ErrInvalidEmail = errors.New("auth: invalid email address")

	// ErrPasswordRequired is returned when a password is required but
	// missing.
	ErrPasswordRequired = errors.New("auth: password is required")

	// ErrWeakPassword is returned when a password does not satisfy the
	// configured password policy.
	ErrWeakPassword = errors.New("auth: password does not meet the requirements")

	// ErrUserAlreadyExists is returned when a user with the same email
	// already exists.
	ErrUserAlreadyExists = errors.New("auth: user with this email already exists")

	// ErrUserNotFound is returned when the requested user does not exist.
	ErrUserNotFound = errors.New("auth: user not found")

	// ErrAccountNotFound is returned when the requested account does not
	// exist.
	ErrAccountNotFound = errors.New("auth: account not found")

	// ErrInvalidCredentials is returned when email or password do not
	// match an account. The error never reveals which one was wrong.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")

	// ErrInvalidCurrentPassword is returned by ChangePassword,
	// ChangeEmail and CheckPassword when the supplied password does not
	// match the user's credential account.
	ErrInvalidCurrentPassword = errors.New("auth: current password is incorrect")

	// ErrEmailNotVerified is returned by SignIn and GetSession when
	// Config.RequireVerifiedEmail is enabled and the user's email address
	// has not been verified yet.
	ErrEmailNotVerified = errors.New("auth: email address is not verified")

	// ErrEmailAlreadyVerified is returned by SendVerificationEmail when
	// the user's address is already verified.
	ErrEmailAlreadyVerified = errors.New("auth: email address is already verified")

	// ErrUserBanned is returned when the account is currently banned.
	ErrUserBanned = errors.New("auth: this account has been banned")

	// ErrSessionNotFound is returned when the requested session does not
	// exist.
	ErrSessionNotFound = errors.New("auth: session not found")

	// ErrSessionExpired is returned when a session token is valid but the
	// session has expired.
	ErrSessionExpired = errors.New("auth: session has expired")

	// ErrInvalidToken is returned when a verification or reset token is
	// unknown or malformed.
	ErrInvalidToken = errors.New("auth: invalid token")

	// ErrTokenExpired is returned when a verification or reset token is
	// recognized but has expired.
	ErrTokenExpired = errors.New("auth: token has expired")

	// ErrMailerNotConfigured is returned by SendVerificationEmail when no
	// mailer is configured. The public, unauthenticated flows
	// (RequestEmailVerification, RequestPasswordReset) never return it,
	// so they cannot be used to enumerate registered addresses.
	ErrMailerNotConfigured = errors.New("auth: no mailer configured")

	// ErrConflict (declared with the store errors in store.go) is also
	// returned by the service when a user or account update kept
	// losing against concurrent modifications after several retries;
	// the caller may simply retry the operation.

	// ErrEmailDeliveryFailed wraps mailer errors returned by the
	// synchronous SendVerificationEmail so callers can detect delivery
	// failures with errors.Is while still seeing the cause. Background
	// deliveries never return it; their failures are logged.
	ErrEmailDeliveryFailed = errors.New("auth: email delivery failed")
)
