// Package web is CoreGo's HTTP framework, built only on the standard
// library. A Server is an http.Handler; handlers receive a *Context and
// return an error, and one configurable error handler turns errors into
// JSON responses.
//
//	s := web.New()
//	s.Use(web.RequestLogger(nil))
//
//	api := s.Group("/api")
//	api.Get("/users/:id", func(c *web.Context) error {
//		user, err := users.Find(c, c.Param("id")) // c is a context.Context
//		if errors.Is(err, users.ErrNotFound) {
//			return web.NotFound("user not found")
//		}
//		if err != nil {
//			return err // logged and answered with a generic 500
//		}
//		return c.JSON(http.StatusOK, user)
//	})
//
//	if err := s.Run(":8080"); err != nil {
//		log.Fatal(err)
//	}
//
// # Registration
//
// Registration methods never return errors. Invalid registrations (empty
// or malformed paths, nil handlers or middleware, duplicate routes,
// conflicting parameter names, a wildcard that is not last, an invalid
// group prefix) are programmer errors and panic immediately with a message
// starting with "web: ", like net/http.ServeMux.
//
// # Routing
//
// Routes are stored in one tree per HTTP method. Path segments are matched
// with the following precedence: static segments first, then ":name"
// parameters, then "*name" wildcards, at every level of the tree. A
// parameter matches exactly one non-empty segment; a wildcard must be the
// last segment of a pattern and captures the remaining path (possibly the
// empty string). Trailing slashes are normalized ("/a/" matches "/a") but
// interior double slashes never match. Request path segments are
// percent-decoded exactly once, both for static matching and for parameter
// capture.
//
// Requests for a known path with an unregistered method receive 405 with an
// Allow header listing the registered methods. OPTIONS requests for a known
// path without an explicit OPTIONS route are answered automatically with
// 204 and an Allow header. HEAD requests fall back to the GET route when no
// explicit HEAD route exists; the body is discarded and a Content-Length
// header matching the suppressed body is sent instead.
//
// # Middleware
//
// Middleware runs in this order, outermost first: global middleware
// (Server.Use, first registered first), group middleware (parent groups
// before child groups), per-route middleware, the built-in panic recovery,
// and finally the handler. Global middleware also wraps the framework's
// 404, 405 and automatic OPTIONS responses. The package ships CORS,
// RateLimit, RequestLogger and Recovery, and adapts net/http handlers and
// middleware with WrapHandler and WrapMiddleware.
//
// # Errors
//
// Handlers return errors instead of writing error responses. By default,
// *HTTPError values (see NewHTTPError, BadRequest, NotFound, ...) produce
// their status code with a JSON body {"error": "..."}; the sentinel errors
// ErrInvalidJSON, ErrUnsupportedMediaType and ErrBodyTooLarge map to 400,
// 415 and 413 with descriptive messages even when wrapped; any other error
// is logged and answered with a generic 500. Server.OnError replaces the
// mapping, and AsHTTPError lets a custom handler reuse it.
//
// # Running
//
// Run serves until SIGINT or SIGTERM and then shuts down gracefully. The
// underlying http.Server carries the ReadHeaderTimeout, ReadTimeout,
// WriteTimeout and IdleTimeout fields of the Server, so the defaults
// protect against slow clients out of the box.
package web
