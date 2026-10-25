package web

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startEphemeral serves s on an ephemeral loopback listener and returns its
// base URL plus a channel that receives Serve's return value.
func startEphemeral(t *testing.T, s *Server) (string, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ln) }()
	awaitStartup(t, s)
	return "http://" + ln.Addr().String(), errCh
}

// awaitStartup blocks until the server has registered its http.Server.
func awaitStartup(t *testing.T, s *Server) *http.Server {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.runMu.Lock()
		hs := s.httpServer
		s.runMu.Unlock()
		if hs != nil {
			return hs
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("server never started")
	return nil
}

// waitResult waits for a Serve/Run result.
func waitResult(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("server did not return")
		return nil
	}
}

// shutdown gracefully stops s and waits for Serve to return nil.
func shutdown(t *testing.T, s *Server, errCh <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitResult(t, errCh); err != nil {
		t.Fatalf("Serve after shutdown: %v", err)
	}
}

// freeAddr returns a loopback address that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// waitReachable polls url until it answers.
func waitReachable(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := client.Get(url); err == nil {
			resp.Body.Close()
			client.CloseIdleConnections()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never became reachable", url)
}

func TestServeAndGracefulShutdown(t *testing.T) {
	s := New()
	s.Get("/ping", text("pong"))
	baseURL, errCh := startEphemeral(t, s)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "pong" {
		t.Fatalf("status/body = %d/%q", resp.StatusCode, body)
	}
	client.CloseIdleConnections()
	shutdown(t, s, errCh)

	// Shutdown closed the listener.
	if _, err := client.Get(baseURL + "/ping"); err == nil {
		t.Error("request after shutdown succeeded, want connection error")
	}
	// The server can be started again.
	_, errCh = startEphemeral(t, s)
	shutdown(t, s, errCh)
}

func TestServerTimeoutsAppliedToHTTPServer(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *Server)
		want  [4]time.Duration // ReadHeader, Read, Write, Idle
	}{
		{name: "defaults", setup: func(s *Server) {}, want: [4]time.Duration{10 * time.Second, 30 * time.Second, 30 * time.Second, 120 * time.Second}},
		{name: "custom", setup: func(s *Server) {
			s.ReadHeaderTimeout, s.ReadTimeout, s.WriteTimeout, s.IdleTimeout = time.Second, 2*time.Second, 3*time.Second, 4*time.Second
		}, want: [4]time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}},
		{name: "zero means no timeout", setup: func(s *Server) {
			s.ReadHeaderTimeout, s.ReadTimeout, s.WriteTimeout, s.IdleTimeout = 0, 0, 0, 0
		}, want: [4]time.Duration{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			tt.setup(s)
			_, errCh := startEphemeral(t, s)
			hs := awaitStartup(t, s)
			got := [4]time.Duration{hs.ReadHeaderTimeout, hs.ReadTimeout, hs.WriteTimeout, hs.IdleTimeout}
			if got != tt.want {
				t.Errorf("http.Server timeouts = %v, want %v", got, tt.want)
			}
			if hs.Handler != s {
				t.Error("http.Server handler is not the web.Server")
			}
			if hs.ErrorLog == nil {
				t.Error("http.Server ErrorLog not set")
			}
			shutdown(t, s, errCh)
		})
	}
}

func TestReadHeaderTimeoutClosesSlowClients(t *testing.T) {
	s := New()
	s.ReadHeaderTimeout = 100 * time.Millisecond
	s.Get("/", text("ok"))
	baseURL, errCh := startEphemeral(t, s)
	defer shutdown(t, s, errCh)

	conn, err := net.Dial("tcp", strings.TrimPrefix(baseURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Slowloris: start a request but never finish the headers.
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	_, err = bufio.NewReader(conn).ReadByte()
	if err == nil {
		// net/http may answer 408 before closing; the connection still ends.
		_, err = io.ReadAll(conn)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatal("connection still open after 3s; ReadHeaderTimeout not applied")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("connection closed after %v, want about 100ms", elapsed)
	}
}

func TestErrorLogBridgedToLogger(t *testing.T) {
	logger, logs := bufferLogger(slog.LevelDebug)
	s := New()
	s.Logger = logger
	_, errCh := startEphemeral(t, s)
	hs := awaitStartup(t, s)
	hs.ErrorLog.Printf("http: TLS handshake error from 1.2.3.4: EOF")
	shutdown(t, s, errCh)
	out := logs.String()
	if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, "TLS handshake error") {
		t.Errorf("ErrorLog output not bridged at Error level: %s", out)
	}
}

func TestShutdownWaitsForInFlightRequest(t *testing.T) {
	handlerStarted := make(chan struct{})
	s := New()
	s.Get("/slow", func(c *Context) error {
		close(handlerStarted)
		select {
		case <-c.Done():
			return c.String(499, "canceled")
		case <-time.After(400 * time.Millisecond):
		}
		return c.String(200, "slow-done")
	})
	baseURL, errCh := startEphemeral(t, s)

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(baseURL + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resCh <- result{body: string(body)}
	}()
	select {
	case <-handlerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}

	started := time.Now()
	shutdown(t, s, errCh)
	if elapsed := time.Since(started); elapsed < 300*time.Millisecond {
		t.Errorf("Shutdown returned after %v; it should wait for the in-flight request", elapsed)
	}
	select {
	case res := <-resCh:
		if res.err != nil || res.body != "slow-done" {
			t.Errorf("in-flight request = %q, %v; want slow-done", res.body, res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not complete")
	}
}

func TestShutdownNotRunning(t *testing.T) {
	err := New().Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("Shutdown on a non-running server = %v, want 'not running'", err)
	}
}

func TestServeRejectsDoubleStart(t *testing.T) {
	s := New()
	_, errCh := startEphemeral(t, s)
	defer shutdown(t, s, errCh)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	starts := map[string]func() error{
		"Serve":          func() error { return s.Serve(ln) },
		"ListenAndServe": func() error { return s.ListenAndServe("127.0.0.1:0") },
		"Run":            func() error { return s.Run("127.0.0.1:0") },
	}
	for name, start := range starts {
		if err := start(); err == nil || !strings.Contains(err.Error(), "already running") {
			t.Errorf("second %s error = %v, want 'already running'", name, err)
		}
	}
}

func TestListenAndServeInvalidAddressAndRestart(t *testing.T) {
	s := New()
	// Port -1 is not a valid TCP port, so listening fails immediately.
	if err := s.ListenAndServe("127.0.0.1:-1"); err == nil {
		t.Fatal("ListenAndServe with invalid address = nil, want error")
	}
	// The failed start must have cleared the running marker.
	_, errCh := startEphemeral(t, s)
	shutdown(t, s, errCh)
}

func TestListenAndServeServesAndShutsDown(t *testing.T) {
	s := New()
	s.Get("/ping", text("pong"))
	addr := freeAddr(t)
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe(addr) }()
	waitReachable(t, "http://"+addr+"/ping")
	shutdown(t, s, errCh)
}

func TestRunInvalidAddress(t *testing.T) {
	s := New()
	if err := s.Run("127.0.0.1:-1"); err == nil {
		t.Fatal("Run with invalid address = nil, want error")
	}
	// Run cleaned up: the server can start again.
	_, errCh := startEphemeral(t, s)
	shutdown(t, s, errCh)
}

func TestRunStopsOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			logger, logs := bufferLogger(slog.LevelInfo)
			s := New()
			s.Logger = logger
			s.Get("/ping", text("pong"))
			addr := freeAddr(t)
			errCh := make(chan error, 1)
			go func() { errCh <- s.Run(addr) }()
			awaitStartup(t, s) // signal.Notify is active from here on
			waitReachable(t, "http://"+addr+"/ping")

			if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			if err := waitResult(t, errCh); err != nil {
				t.Errorf("Run after %v = %v, want nil", sig, err)
			}
			if !strings.Contains(logs.String(), "shutting down") {
				t.Errorf("shutdown not logged: %s", logs.String())
			}
		})
	}
}

func TestRunShutdownTimeoutClosesStuckRequests(t *testing.T) {
	s := New()
	s.ShutdownTimeout = 100 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	s.Get("/stuck", func(c *Context) error {
		close(started)
		<-release
		return nil
	})
	s.Get("/ping", text("pong"))
	addr := freeAddr(t)
	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(addr) }()
	awaitStartup(t, s)
	waitReachable(t, "http://"+addr+"/ping")

	clientErr := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/stuck")
		if err == nil {
			resp.Body.Close()
		}
		clientErr <- err
	}()
	<-started
	begin := time.Now()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := waitResult(t, errCh)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Errorf("Run took %v to give up, want about ShutdownTimeout", elapsed)
	}
	select {
	case err := <-clientErr:
		if err == nil {
			t.Error("stuck request completed; its connection should have been closed")
		}
	case <-time.After(3 * time.Second):
		t.Error("stuck connection was not closed after the shutdown timeout")
	}
}

func TestRunReturnsWhenShutdownCalled(t *testing.T) {
	s := New()
	s.Get("/ping", text("pong"))
	addr := freeAddr(t)
	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(addr) }()
	awaitStartup(t, s)
	waitReachable(t, "http://"+addr+"/ping")
	shutdown(t, s, errCh)
}

func TestServeConcurrentLoad(t *testing.T) {
	// End-to-end load through a real listener to complement the -race run.
	s := New()
	s.Get("/hello/:name", func(c *Context) error {
		return c.String(200, fmt.Sprintf("hi %s", c.Param("name")))
	})
	baseURL, errCh := startEphemeral(t, s)

	// A dedicated transport with enough per-host idle slots avoids the
	// client dialing speculative connections that never carry a request.
	transport := &http.Transport{MaxIdleConnsPerHost: 100}
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport}

	const n = 30
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			resp, err := client.Get(fmt.Sprintf("%s/hello/user%d", baseURL, i))
			if err != nil {
				errs <- err
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != fmt.Sprintf("hi user%d", i) {
				errs <- fmt.Errorf("body = %q", body)
				return
			}
			errs <- nil
		}(i)
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent load timed out")
		}
	}
	transport.CloseIdleConnections()
	shutdown(t, s, errCh)
}
