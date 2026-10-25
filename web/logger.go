package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger returns a middleware that logs one line per request with
// the attributes method, path, status, duration and remote_ip. Responses
// with a 5xx status are logged at Error level, everything else at Info. A
// nil logger means slog.Default(), resolved for every request.
//
// Install it first with Server.Use so it is outermost and also sees the
// framework's 404, 405 and OPTIONS responses:
//
//	s.Use(web.RequestLogger(nil))
//
// To log the final status, the middleware renders errors returned by the
// rest of the chain through the error handler itself and then returns nil;
// middleware registered before it therefore sees a nil error. The query
// string is deliberately not logged because it may carry secrets such as
// tokens.
func RequestLogger(logger *slog.Logger) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			start := time.Now()
			method, path, remoteIP := c.Method(), c.Path(), c.RemoteIP()
			completed := false
			defer func() {
				if completed {
					return
				}
				// A panic escaped the chain; the server's safety net will
				// answer 500. Log the request as such and keep unwinding.
				logRequest(c, logger, method, path, remoteIP, http.StatusInternalServerError, time.Since(start))
			}()
			if err := next(c); err != nil {
				c.handleError(err)
			}
			completed = true
			logRequest(c, logger, method, path, remoteIP, c.w.statusCode(), time.Since(start))
			return nil
		}
	}
}

// logRequest writes the access log line for one request.
func logRequest(ctx context.Context, logger *slog.Logger, method, path, remoteIP string, status int, duration time.Duration) {
	if logger == nil {
		logger = slog.Default()
	}
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	}
	logger.LogAttrs(ctx, level, "http request",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", status),
		slog.Duration("duration", duration),
		slog.String("remote_ip", remoteIP),
	)
}
