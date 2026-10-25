package web

import (
	"errors"
	"net/http"
	"runtime/debug"
)

// HandlerFunc is a single request handler. Returning a non-nil error hands
// the error to the server's error handler (see Server.OnError), so handlers
// usually end with one of the Context writers:
//
//	return c.JSON(http.StatusOK, user)
type HandlerFunc func(*Context) error

// MiddlewareFunc wraps a HandlerFunc with additional behavior. Middleware
// may short-circuit the chain by writing a response and returning nil (or
// by returning the writer's result, e.g. `return c.NoContent()`), or abort
// it by returning an error, which propagates to the server's error handler.
//
// The outer function runs once, when the chain is composed (at route
// registration for group and per-route middleware, at Use for global
// middleware); the returned HandlerFunc runs once per request.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// ErrorHandler turns an error returned by a handler or middleware into a
// response. Install one with Server.OnError.
type ErrorHandler func(*Context, error)

// chain composes mw around h so that the first middleware in mw is the
// outermost: chain(h, A, B) behaves as A(B(h)).
func chain(h HandlerFunc, mw ...MiddlewareFunc) HandlerFunc {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Recovery returns a middleware that converts panics raised downstream into
// a logged 500 response and keeps the server running. The panic value and
// stack trace are logged through the server's Logger; clients only ever see
// a generic "Internal Server Error".
//
// Every route registered on a Server is already wrapped in an equivalent,
// innermost recovery, and panics in global middleware are caught by the
// server as a last resort, so most applications never need Recovery. It is
// exported for custom stacks that want recovery at a specific position.
//
// A panic with http.ErrAbortHandler is re-raised untouched so that net/http
// aborts the response silently, as documented for that sentinel.
func Recovery() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					if isAbortPanic(r) {
						panic(r)
					}
					c.logger().Error("web: panic recovered",
						"method", c.Method(),
						"path", c.Path(),
						"panic", r,
						"stack", string(debug.Stack()),
					)
					err = NewHTTPError(http.StatusInternalServerError, "")
				}
			}()
			return next(c)
		}
	}
}

// isAbortPanic reports whether a recovered panic value is
// http.ErrAbortHandler, which must propagate to net/http.
func isAbortPanic(r any) bool {
	err, ok := r.(error)
	return ok && errors.Is(err, http.ErrAbortHandler)
}
