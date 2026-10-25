package web

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// errAlreadyRunning and errNotRunning report lifecycle misuse.
var (
	errAlreadyRunning = errors.New("web: server is already running")
	errNotRunning     = errors.New("web: server is not running")
)

// newHTTPServer builds the http.Server used to serve s, carrying the
// timeout fields exactly as configured (0 = no timeout) and an ErrorLog
// bridged to the server's Logger at Error level.
func (s *Server) newHTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: s.ReadHeaderTimeout,
		ReadTimeout:       s.ReadTimeout,
		WriteTimeout:      s.WriteTimeout,
		IdleTimeout:       s.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(s.logger().Handler(), slog.LevelError),
	}
}

// Serve accepts connections on ln and serves requests until Shutdown is
// called, then returns nil. Shutdown closes ln. Serve returns an error if
// the server is already running.
func (s *Server) Serve(ln net.Listener) error {
	hs := s.newHTTPServer("")
	if !s.setHTTPServer(hs) {
		return errAlreadyRunning
	}
	defer s.clearHTTPServer(hs)
	return ignoreClosed(hs.Serve(ln))
}

// ListenAndServe listens on the TCP address addr (for example ":8080") and
// serves requests until Shutdown is called, then returns nil. It returns an
// error if listening fails or the server is already running.
func (s *Server) ListenAndServe(addr string) error {
	hs := s.newHTTPServer(addr)
	if !s.setHTTPServer(hs) {
		return errAlreadyRunning
	}
	defer s.clearHTTPServer(hs)
	return ignoreClosed(hs.ListenAndServe())
}

// Run listens on addr and serves requests until the process receives
// SIGINT or SIGTERM, then shuts down gracefully: it stops accepting
// connections and waits up to ShutdownTimeout for in-flight requests
// before closing whatever is left. It returns nil after a clean shutdown,
// the listen error if the server cannot start, or the shutdown error if
// in-flight requests did not finish in time. A Shutdown call from another
// goroutine also makes Run return.
func (s *Server) Run(addr string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	hs := s.newHTTPServer(addr)
	if !s.setHTTPServer(hs) {
		return errAlreadyRunning
	}
	defer s.clearHTTPServer(hs)

	errCh := make(chan error, 1)
	go func() { errCh <- ignoreClosed(hs.ListenAndServe()) }()

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		s.logger().Info("web: shutting down", "signal", sig.String(), "timeout", s.ShutdownTimeout)
	}

	ctx := context.Background()
	if s.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.ShutdownTimeout)
		defer cancel()
	}
	err := hs.Shutdown(ctx)
	if err != nil {
		// Out of time: drop the connections that are still busy.
		_ = hs.Close()
	}
	if serveErr := <-errCh; err == nil {
		err = serveErr
	}
	return err
}

// Shutdown gracefully shuts down the running server: it stops accepting
// new connections, closes idle keep-alive connections and waits for
// in-flight requests to complete or ctx to end, in which case ctx's error
// is returned. It returns an error when the server is not running.
func (s *Server) Shutdown(ctx context.Context) error {
	s.runMu.Lock()
	hs := s.httpServer
	s.runMu.Unlock()
	if hs == nil {
		return errNotRunning
	}
	return hs.Shutdown(ctx)
}

// setHTTPServer installs hs unless a server is already running, reporting
// whether it took effect.
func (s *Server) setHTTPServer(hs *http.Server) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.httpServer != nil {
		return false
	}
	s.httpServer = hs
	return true
}

// clearHTTPServer removes hs so the server can be started again later.
func (s *Server) clearHTTPServer(hs *http.Server) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.httpServer == hs {
		s.httpServer = nil
	}
}

// ignoreClosed maps http.ErrServerClosed, the normal result of a graceful
// shutdown, to nil.
func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
