package web

import (
	"context"
	"net/http"
)

// WrapHandler adapts a standard http.Handler into a HandlerFunc, for
// mounting existing net/http handlers (file servers, metrics endpoints,
// pprof, ...) on CoreGo routes:
//
//	s.Get("/static/*path", web.WrapHandler(http.StripPrefix("/static/", http.FileServer(dir))))
//
// The handler receives the current request and the tracked response
// writer; the returned HandlerFunc always returns nil. It panics if h is
// nil.
func WrapHandler(h http.Handler) HandlerFunc {
	if h == nil {
		panic("web: WrapHandler called with a nil http.Handler")
	}
	return func(c *Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// wrapContextKey is the request-context key WrapMiddleware uses to carry
// the *Context through a standard middleware.
type wrapContextKey struct{}

// WrapMiddleware adapts a standard net/http middleware into a
// MiddlewareFunc:
//
//	// func securityHeaders(next http.Handler) http.Handler { ... }
//	s.Use(web.WrapMiddleware(securityHeaders))
//
// The standard middleware is constructed once per composed chain, not per
// request. When it calls the next handler, the request it passes becomes
// the Context's request (so values it attached with r.WithContext are
// visible to c.Value and downstream handlers) and the response writer it
// passes is used for everything written downstream. A middleware that
// does not call next short-circuits the chain; whatever it wrote is the
// response.
//
// Errors returned downstream are rendered by the error handler before
// control returns to the standard middleware, so it observes the final
// status and body; the resulting MiddlewareFunc therefore returns nil.
// WrapMiddleware panics if mw is nil.
func WrapMiddleware(mw func(http.Handler) http.Handler) MiddlewareFunc {
	if mw == nil {
		panic("web: WrapMiddleware called with a nil middleware")
	}
	return func(next HandlerFunc) HandlerFunc {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := r.Context().Value(wrapContextKey{}).(*Context)
			if !ok {
				// The middleware replaced the request context wholesale,
				// losing the framework's handle on this request.
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			c.SetRequest(r)
			if w != http.ResponseWriter(c.w) {
				previous := c.w
				c.w = newResponseWriter(w)
				defer func() { c.w = previous }()
			}
			if err := next(c); err != nil {
				c.handleError(err)
			}
		})
		wrapped := mw(inner)
		if wrapped == nil {
			panic("web: WrapMiddleware: the middleware returned a nil http.Handler")
		}
		return func(c *Context) error {
			r := c.Request()
			r = r.WithContext(context.WithValue(r.Context(), wrapContextKey{}, c))
			wrapped.ServeHTTP(c.Response(), r)
			return nil
		}
	}
}
