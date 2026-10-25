package web

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError is an error carrying an HTTP status code and a user-presentable
// message. Handlers and middleware return it to make the server's error
// handler respond with the given code and a JSON body of the form
// {"error": Message}. Err optionally wraps an underlying cause, which is
// preserved for errors.As and errors.Is inspection but is never sent to
// clients.
type HTTPError struct {
	Code    int
	Message string
	Err     error
}

// NewHTTPError returns an *HTTPError with the given status code and
// message. An empty message falls back to the standard status text for the
// code when the error is rendered.
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

// BadRequest returns a 400 *HTTPError. An empty message renders as
// "Bad Request".
func BadRequest(message string) *HTTPError {
	return NewHTTPError(http.StatusBadRequest, message)
}

// Unauthorized returns a 401 *HTTPError. An empty message renders as
// "Unauthorized".
func Unauthorized(message string) *HTTPError {
	return NewHTTPError(http.StatusUnauthorized, message)
}

// Forbidden returns a 403 *HTTPError. An empty message renders as
// "Forbidden".
func Forbidden(message string) *HTTPError {
	return NewHTTPError(http.StatusForbidden, message)
}

// NotFound returns a 404 *HTTPError. An empty message renders as
// "Not Found".
func NotFound(message string) *HTTPError {
	return NewHTTPError(http.StatusNotFound, message)
}

// Conflict returns a 409 *HTTPError. An empty message renders as
// "Conflict".
func Conflict(message string) *HTTPError {
	return NewHTTPError(http.StatusConflict, message)
}

// TooManyRequests returns a 429 *HTTPError. An empty message renders as
// "Too Many Requests".
func TooManyRequests(message string) *HTTPError {
	return NewHTTPError(http.StatusTooManyRequests, message)
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("web: %d %s: %v", e.Code, statusText(e.Code, e.Message), e.Err)
	}
	return fmt.Sprintf("web: %d %s", e.Code, statusText(e.Code, e.Message))
}

// Unwrap exposes the wrapped cause, if any.
func (e *HTTPError) Unwrap() error { return e.Err }

// Sentinel errors returned by Context methods. The default error handler
// (see AsHTTPError) maps each of them to a specific HTTP status and a
// descriptive, user-presentable message even when they are returned bare
// or wrapped in more descriptive errors:
//
//	ErrBodyTooLarge         413 "Request body exceeds the configured size limit."
//	ErrUnsupportedMediaType 415 "Content-Type must be application/json."
//	ErrInvalidJSON          400 "Request body is not valid JSON."
//
// The wrapped details (the offending Content-Type, the JSON syntax error,
// the unknown field) stay in the error value for logging and are never
// sent to clients.
var (
	ErrBodyTooLarge         = errors.New("web: request body exceeds the configured limit")
	ErrUnsupportedMediaType = errors.New("web: unsupported media type")
	ErrInvalidJSON          = errors.New("web: invalid JSON body")
)

// Messages rendered for the body sentinels. They are part of the public
// contract of the default error handler and of the auth routes, which
// reuse them through AsHTTPError.
const (
	msgBodyTooLarge         = "Request body exceeds the configured size limit."
	msgUnsupportedMediaType = "Content-Type must be application/json."
	msgInvalidJSON          = "Request body is not valid JSON."
)

// sentinelMappings pairs each body sentinel with the status code and
// message it renders as.
var sentinelMappings = []struct {
	sentinel error
	code     int
	message  string
}{
	{ErrBodyTooLarge, http.StatusRequestEntityTooLarge, msgBodyTooLarge},
	{ErrUnsupportedMediaType, http.StatusUnsupportedMediaType, msgUnsupportedMediaType},
	{ErrInvalidJSON, http.StatusBadRequest, msgInvalidJSON},
}

// AsHTTPError returns the *HTTPError the default error handler renders
// for err, and reports whether there is one:
//
//   - when err is, or wraps, an *HTTPError, that error is returned as-is;
//   - when err is, or wraps, ErrBodyTooLarge, ErrUnsupportedMediaType or
//     ErrInvalidJSON, a new *HTTPError carrying the matching status code
//     and message (see the sentinels) is returned, with err as its cause;
//   - otherwise (including a nil err) it returns nil and false; the
//     default error handler logs such errors and answers 500.
//
// Custom error handlers installed with Server.OnError can call it to
// render the framework's errors exactly like the default handler does:
//
//	server.OnError(func(c *web.Context, err error) {
//		if he, ok := web.AsHTTPError(err); ok {
//			_ = c.JSON(he.Code, map[string]string{"message": he.Message})
//			return
//		}
//		_ = c.JSON(http.StatusInternalServerError, map[string]string{"message": "oops"})
//	})
//
// The returned error's Message may be empty, in which case the standard
// status text for its Code is what the default handler renders.
func AsHTTPError(err error) (*HTTPError, bool) {
	if err == nil {
		return nil, false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr, true
	}
	for _, m := range sentinelMappings {
		if errors.Is(err, m.sentinel) {
			return &HTTPError{Code: m.code, Message: m.message, Err: err}, true
		}
	}
	return nil, false
}

// statusText returns message when set, the standard status text for code
// otherwise, and a generic placeholder for unknown codes.
func statusText(code int, message string) string {
	if message != "" {
		return message
	}
	if text := http.StatusText(code); text != "" {
		return text
	}
	return fmt.Sprintf("status %d", code)
}
