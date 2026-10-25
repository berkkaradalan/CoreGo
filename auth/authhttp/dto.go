package authhttp

import (
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
)

// msgEmailRequestAccepted is the body of POST /verify-email/request and
// POST /forgot-password. It is identical for registered and unknown
// addresses, so the response cannot be used to enumerate accounts.
const msgEmailRequestAccepted = "If an account exists for this email, we have sent it an email with further instructions."

// Request bodies. All JSON field names are snake_case.

// signUpRequest is the body of POST /signup.
type signUpRequest struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Image      string `json:"image"`
	RememberMe bool   `json:"remember_me"`
}

// signInRequest is the body of POST /signin.
type signInRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

// emailRequest is the body of POST /verify-email/request and
// POST /forgot-password.
type emailRequest struct {
	Email string `json:"email"`
}

// verifyEmailRequest is the body of POST /verify-email.
type verifyEmailRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

// resetPasswordRequest is the body of POST /reset-password.
type resetPasswordRequest struct {
	Email       string `json:"email"`
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// changePasswordRequest is the body of POST /change-password.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changeEmailRequest is the body of POST /change-email.
type changeEmailRequest struct {
	NewEmail        string `json:"new_email"`
	CurrentPassword string `json:"current_password"`
}

// updateMeRequest is the body of PATCH /me. Pointer fields give the
// request PATCH semantics: a field absent from the JSON stays nil and is
// left unchanged. Email is decoded only to reject it with a pointer to
// POST /change-email instead of silently ignoring it.
type updateMeRequest struct {
	Name  *string `json:"name"`
	Image *string `json:"image"`
	Email *string `json:"email"`
}

// deleteMeRequest is the body of DELETE /me.
type deleteMeRequest struct {
	Password string `json:"password"`
}

// Response bodies.

// userResponse is the sanitized projection of an auth.User used in every
// response: no password (hashes live on credential accounts and never
// leave the service) and no ban internals.
type userResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Image         string    `json:"image"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// newUserResponse projects a user record onto its response shape.
func newUserResponse(u *auth.User) userResponse {
	return userResponse{
		ID:            u.ID,
		Name:          u.Name,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		Image:         u.Image,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

// sessionResponse is the sanitized projection of an auth.Session. The
// token hash is never included. Current marks the session the request
// itself is authenticated with.
type sessionResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Current   bool      `json:"current"`
}

// newSessionResponse projects a session record onto its response shape;
// currentID is the ID of the request's own session.
func newSessionResponse(s *auth.Session, currentID string) sessionResponse {
	return sessionResponse{
		ID:        s.ID,
		UserID:    s.UserID,
		ExpiresAt: s.ExpiresAt,
		IPAddress: s.IPAddress,
		UserAgent: s.UserAgent,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Current:   s.ID == currentID,
	}
}

// signUpResponse is the body of POST /signup. Token and ExpiresAt are
// omitted when the service requires a verified email address before
// signing in; VerificationRequired then tells the client to ask the user
// to confirm the address and sign in afterwards. Token alone is omitted
// when CookieConfig.OmitTokenFromBody keeps it in the cookie only.
type signUpResponse struct {
	User                 userResponse `json:"user"`
	Token                string       `json:"token,omitempty"`
	ExpiresAt            *time.Time   `json:"expires_at,omitempty"`
	VerificationRequired bool         `json:"verification_required"`
}

// signInResponse is the body of POST /signin. Token is the raw session
// token, returned exactly once, or omitted when
// CookieConfig.OmitTokenFromBody keeps it in the cookie only.
type signInResponse struct {
	User      userResponse `json:"user"`
	Token     string       `json:"token,omitempty"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// sessionDetailResponse is the body of GET /session.
type sessionDetailResponse struct {
	User    userResponse    `json:"user"`
	Session sessionResponse `json:"session"`
}

// listSessionsResponse is the body of GET /sessions. Sessions is never
// null, so the JSON always carries an array.
type listSessionsResponse struct {
	Sessions []sessionResponse `json:"sessions"`
}

// userEnvelope is the body of GET /me, PATCH /me and POST /change-email.
type userEnvelope struct {
	User userResponse `json:"user"`
}

// messageResponse is the body of 202 responses that carry a notice
// instead of data.
type messageResponse struct {
	Message string `json:"message"`
}
