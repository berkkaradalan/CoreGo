package authhttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// Stable, human-readable messages rendered for the mapped errors. They
// are safe to show to end users: they never echo store errors, token
// values or other internals. ErrInvalidCredentials deliberately does not
// reveal which part of the credential pair was wrong.
const (
	msgInvalidCredentials      = "Invalid email or password."
	msgMissingToken            = "Missing session token."
	msgSessionNotFound         = "Invalid session token."
	msgSessionExpired          = "Session has expired. Please sign in again."
	msgUserBanned              = "This account has been banned."
	msgEmailNotVerified        = "Email address is not verified."
	msgUserNotFound            = "User not found."
	msgAccountNotFound         = "This account has no password."
	msgUserAlreadyExists       = "A user with this email already exists."
	msgEmailAlreadyVerified    = "Email address is already verified."
	msgNameRequired            = "Name is required."
	msgEmailRequired           = "Email is required."
	msgInvalidEmail            = "Invalid email address."
	msgPasswordRequired        = "Password is required."
	msgWeakPassword            = "Password does not meet the requirements."
	msgInvalidCurrentPassword  = "Current password is incorrect."
	msgInvalidToken            = "Invalid token."
	msgTokenExpired            = "Token has expired."
	msgMailerNotConfigured     = "Email delivery is not configured."
	msgEmailDeliveryFailed     = "Email delivery failed."
	msgSessionResourceNotFound = "Session not found."
	msgEmailNotPatchable       = "The email address cannot be changed here; use POST /change-email."
	msgConflict                = "The account was modified concurrently. Please retry."
	msgNameTooLong             = "Name must be at most 256 characters."
	msgInvalidImageURL         = "Image must be empty or an absolute http or https URL of at most 2048 bytes."
	msgCrossSiteBlocked        = "Cross-site request blocked."
)

// errorMapping pairs one sentinel error with the HTTP status code and
// message rendered for it.
type errorMapping struct {
	sentinel error
	code     int
	message  string
}

// errorMappings is the sentinel-to-HTTP table applied to every error
// returned by the auth service. errors.Is is used for matching, so
// wrapped sentinels map identically. The web body-parsing sentinels are
// not listed: httpErrorFor resolves them through web.AsHTTPError, so the
// auth routes render them exactly like any other web route does.
//
// Two choices deserve a note. ErrInvalidCurrentPassword is 400, not
// 401: the request is authenticated, only a field is wrong, and a 401
// would make many SPA clients sign the user out. ErrAccountNotFound
// (a user without a password, for example one who signed up through a
// social provider) is 409: the request conflicts with the state of the
// account. ErrConflict (concurrent modifications kept winning the race
// even after the service's retries) is 409 too; repeating the request
// is safe.
var errorMappings = []errorMapping{
	{auth.ErrInvalidCredentials, http.StatusUnauthorized, msgInvalidCredentials},
	{auth.ErrSessionNotFound, http.StatusUnauthorized, msgSessionNotFound},
	{auth.ErrSessionExpired, http.StatusUnauthorized, msgSessionExpired},
	{auth.ErrUserBanned, http.StatusForbidden, msgUserBanned},
	{auth.ErrEmailNotVerified, http.StatusForbidden, msgEmailNotVerified},
	{auth.ErrUserNotFound, http.StatusNotFound, msgUserNotFound},
	{auth.ErrAccountNotFound, http.StatusConflict, msgAccountNotFound},
	{auth.ErrUserAlreadyExists, http.StatusConflict, msgUserAlreadyExists},
	{auth.ErrEmailAlreadyVerified, http.StatusConflict, msgEmailAlreadyVerified},
	{auth.ErrConflict, http.StatusConflict, msgConflict},
	{auth.ErrNameRequired, http.StatusBadRequest, msgNameRequired},
	{auth.ErrNameTooLong, http.StatusBadRequest, msgNameTooLong},
	{auth.ErrInvalidImageURL, http.StatusBadRequest, msgInvalidImageURL},
	{auth.ErrEmailRequired, http.StatusBadRequest, msgEmailRequired},
	{auth.ErrInvalidEmail, http.StatusBadRequest, msgInvalidEmail},
	{auth.ErrPasswordRequired, http.StatusBadRequest, msgPasswordRequired},
	{auth.ErrWeakPassword, http.StatusBadRequest, msgWeakPassword},
	{auth.ErrInvalidCurrentPassword, http.StatusBadRequest, msgInvalidCurrentPassword},
	{auth.ErrInvalidToken, http.StatusBadRequest, msgInvalidToken},
	{auth.ErrTokenExpired, http.StatusBadRequest, msgTokenExpired},
	{auth.ErrMailerNotConfigured, http.StatusServiceUnavailable, msgMailerNotConfigured},
	{auth.ErrEmailDeliveryFailed, http.StatusBadGateway, msgEmailDeliveryFailed},
}

// httpErrorFor maps err onto a *web.HTTPError according to
// errorMappings, falling back to web.AsHTTPError for the web
// body-parsing sentinels (ErrInvalidJSON, ErrUnsupportedMediaType,
// ErrBodyTooLarge) and for errors that already are, or wrap, a
// *web.HTTPError. The original error is kept as the cause, so errors.Is
// still matches the sentinel on the returned value (useful in a custom
// web error handler). It returns nil for unknown errors.
func httpErrorFor(err error) *web.HTTPError {
	for _, m := range errorMappings {
		if errors.Is(err, m.sentinel) {
			return &web.HTTPError{Code: m.code, Message: m.message, Err: err}
		}
	}
	if he, ok := web.AsHTTPError(err); ok {
		return he
	}
	return nil
}

// mapError converts err into the most accurate web error: a mapped
// *web.HTTPError for known sentinels and the error itself otherwise, so
// unknown errors reach the web error handler untouched and are logged
// and answered with a 500 instead of being swallowed.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if he := httpErrorFor(err); he != nil {
		return he
	}
	return err
}

// errMissingToken is the error of a protected request that carries no
// session token at all.
func errMissingToken() *web.HTTPError {
	return web.Unauthorized(msgMissingToken)
}

// errCrossSite is the error of a cookie-authenticated, state-changing
// request that fails the cross-site request forgery check.
func errCrossSite() *web.HTTPError {
	return web.Forbidden(msgCrossSiteBlocked)
}

// errorBody is the JSON shape of every error response: {"error": "..."},
// identical to the web framework's default error handler.
type errorBody struct {
	Error string `json:"error"`
}

// writeHTTPError renders err for plain net/http handlers exactly like
// the web framework's default error handler renders it: a mapped
// *web.HTTPError with its code and message, anything else as a logged
// 500 "Internal Server Error".
func writeHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	code, message := http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)
	var he *web.HTTPError
	if errors.As(mapError(err), &he) {
		code, message = he.Code, he.Message
		if message == "" {
			message = http.StatusText(code)
		}
	} else {
		slog.Default().ErrorContext(r.Context(), "authhttp: request handling error",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
	}
	if code == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	payload, _ := json.Marshal(errorBody{Error: message})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(payload)
}
