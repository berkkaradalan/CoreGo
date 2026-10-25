package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSession records everything one client connection said to the
// fake SMTP server. It is written only by the server handler goroutine
// and published to tests through a channel, which establishes the
// happens-before edge for race-detector-clean reads.
type fakeSession struct {
	commands []string // raw command lines in order
	authArg  string   // AUTH argument when AUTH was attempted
	hadAUTH  bool
	mailFrom string   // parsed MAIL FROM address
	rcpts    []string // parsed RCPT TO addresses in order
	data     []byte   // raw DATA payload (without the terminating dot line semantics applied)
	startTLS bool     // whether the session upgraded to TLS
	quit     bool     // whether the client ended the dialog with QUIT
}

// fakeServerOptions configures the protocol behavior of the fake SMTP
// server.
type fakeServerOptions struct {
	advertiseSTARTTLS bool              // advertise STARTTLS in EHLO replies
	implicitTLS       bool              // serve TLS from the first byte (requires tlsConfig)
	tlsConfig         *tls.Config       // server TLS config for STARTTLS upgrade / implicit TLS
	acceptAUTH        bool              // reply 235 to AUTH instead of 535
	failCommands      map[string]string // command verb -> error reply (e.g. "MAIL" -> "550 nope")
	rcptReplies       map[int]string    // zero-based RCPT command index -> error reply
}

// fakeSMTPServer is a minimal deterministic SMTP server running on a
// real localhost net.Listener. It implements the protocol state
// machine line-by-line (greeting, EHLO, STARTTLS, AUTH, MAIL, RCPT,
// DATA, QUIT) and records the dialog for assertions. There are no
// arbitrary sleeps: tests wait for a completed session through a
// channel with a timeout guard.
type fakeSMTPServer struct {
	opts fakeServerOptions
	ln   net.Listener
	done chan *fakeSession // completed sessions
}

// startFakeSMTPServer starts the fake server on 127.0.0.1:0 and closes
// its listener when the test finishes.
func startFakeSMTPServer(t *testing.T, opts fakeServerOptions) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake smtp server: listen: %v", err)
	}
	if opts.implicitTLS {
		if opts.tlsConfig == nil {
			t.Fatal("fake smtp server: implicitTLS requires tlsConfig")
		}
		ln = tls.NewListener(ln, opts.tlsConfig)
	}
	s := &fakeSMTPServer{opts: opts, ln: ln, done: make(chan *fakeSession, 8)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

// addr returns the host:port the server listens on.
func (s *fakeSMTPServer) addr() string { return s.ln.Addr().String() }

// config returns an SMTPConfig pointing at the server with a test
// timeout. TLSMode is left at the zero value (TLSStartTLS) for the
// caller to override.
func (s *fakeSMTPServer) config(t *testing.T) SMTPConfig {
	t.Helper()
	host, portStr, err := net.SplitHostPort(s.addr())
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return SMTPConfig{Host: host, Port: port, Timeout: 5 * time.Second}
}

// waitSession blocks until a session completes or the timeout guard
// fires, and returns the recorded session.
func (s *fakeSMTPServer) waitSession(t *testing.T) *fakeSession {
	t.Helper()
	select {
	case sess := <-s.done:
		return sess
	case <-time.After(5 * time.Second):
		t.Fatal("fake smtp server: timed out waiting for a completed session")
		return nil
	}
}

// handle runs the SMTP dialog for a single connection.
func (s *fakeSMTPServer) handle(conn net.Conn) {
	session := &fakeSession{}
	defer func() {
		conn.Close()
		select {
		case s.done <- session:
		default:
		}
	}()
	// Absolute deadline so a stuck client can never hang the test.
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(line string) {
		w.WriteString(line)
		w.WriteString("\r\n")
		w.Flush()
	}

	writeLine("220 fake.test ESMTP ready")
	tlsReady := false

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		verb := strings.ToUpper(fields[0])
		session.commands = append(session.commands, line)

		if failReply, ok := s.opts.failCommands[verb]; ok {
			writeLine(failReply)
			continue
		}

		switch verb {
		case "EHLO", "HELO":
			if s.opts.advertiseSTARTTLS && !tlsReady {
				writeLine("250-fake.test greets you")
				writeLine("250-STARTTLS")
				writeLine("250 8BITMIME")
			} else {
				writeLine("250-fake.test greets you")
				writeLine("250 8BITMIME")
			}
		case "STARTTLS":
			if s.opts.tlsConfig == nil || !s.opts.advertiseSTARTTLS {
				writeLine("454 TLS not available")
				continue
			}
			writeLine("220 Ready to start TLS")
			tlsConn := tls.Server(conn, s.opts.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			tlsReady = true
			session.startTLS = true
		case "AUTH":
			if s.opts.acceptAUTH {
				session.hadAUTH = true
				session.authArg = strings.TrimSpace(strings.TrimPrefix(line, "AUTH "))
				writeLine("235 2.7.0 Authentication successful")
			} else {
				writeLine("535 5.7.8 Authentication credentials invalid")
			}
		case "MAIL":
			session.mailFrom = parseEnvelopeArg(line)
			writeLine("250 2.1.0 Sender OK")
		case "RCPT":
			index := len(session.rcpts)
			session.rcpts = append(session.rcpts, parseEnvelopeArg(line))
			if reply, ok := s.opts.rcptReplies[index]; ok {
				writeLine(reply)
				continue
			}
			writeLine("250 2.1.5 Recipient OK")
		case "DATA":
			writeLine("354 End data with <CR><LF>.<CR><LF>")
			var data []byte
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				data = append(data, dataLine...)
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
			}
			session.data = data
			writeLine("250 2.0.0 OK: queued as FAKE123")
		case "RSET", "NOOP":
			writeLine("250 OK")
		case "QUIT":
			session.quit = true
			writeLine("221 2.0.0 Bye")
			return
		default:
			writeLine("500 5.5.2 Command not recognized")
		}
	}
}

// parseEnvelopeArg extracts the bare address from a MAIL FROM:<addr>
// or RCPT TO:<addr> command line, tolerating trailing parameters such
// as "BODY=8BITMIME" that net/smtp appends when advertised.
func parseEnvelopeArg(line string) string {
	start := strings.Index(line, "<")
	end := strings.LastIndex(line, ">")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	return ""
}

// selfSignedCert generates a throwaway self-signed certificate valid
// for 127.0.0.1, together with a pool trusting it.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// withTLSClientConfig swaps the tlsConfigFor seam to trust the test
// certificate for the duration of the test.
func withTLSClientConfig(t *testing.T, pool *x509.CertPool) {
	t.Helper()
	orig := tlsConfigFor
	tlsConfigFor = func(host string) *tls.Config {
		return &tls.Config{ServerName: host, RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	t.Cleanup(func() { tlsConfigFor = orig })
}

// withDialRecorder swaps the dialSMTPConn seam so every dial attempt is
// counted and always fails. Returns a function reporting the count.
func withDialRecorder(t *testing.T) func() int {
	t.Helper()
	orig := dialSMTPConn
	var mu sync.Mutex
	dials := 0
	dialSMTPConn = func(ctx context.Context, timeout time.Duration, network, addr string) (net.Conn, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		return nil, errors.New("unexpected dial in test")
	}
	t.Cleanup(func() { dialSMTPConn = orig })
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return dials
	}
}

func TestNewSMTPSenderDefaults(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "smtp.example.test"})
	if s.cfg.Port != 587 {
		t.Errorf("Port = %d, want default 587", s.cfg.Port)
	}
	if s.cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want default 10s", s.cfg.Timeout)
	}
	if s.cfg.TLSMode != TLSStartTLS {
		t.Errorf("TLSMode = %v, want default TLSStartTLS", s.cfg.TLSMode)
	}
}

// Case (a): happy path in plaintext. The server advertises STARTTLS
// but the config disables it; the envelope must carry To+Cc+Bcc while
// the DATA payload must contain the headers but no Bcc trace.
func TestSMTPSenderPlaintextEnvelopeAndData(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{advertiseSTARTTLS: true})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	cfg.From = "envelope@example.test"
	sender := NewSMTPSender(cfg)

	msg := Message{
		// From is filled from the config when empty.
		To:      []string{"bob@example.test", "carol@example.test"},
		Cc:      []string{"dave@example.test"},
		Bcc:     []string{"hidden@example.test"},
		Subject: "Quarterly report",
		Text:    "Hello Bob",
	}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)

	if got, want := sess.mailFrom, "envelope@example.test"; got != want {
		t.Errorf("MAIL FROM = %q, want %q", got, want)
	}
	wantRcpts := []string{
		"bob@example.test",
		"carol@example.test",
		"dave@example.test",
		"hidden@example.test",
	}
	if !reflect.DeepEqual(sess.rcpts, wantRcpts) {
		t.Errorf("RCPT TO list = %v, want %v", sess.rcpts, wantRcpts)
	}

	data := string(sess.data)
	for _, want := range []string{
		"From: envelope@example.test",
		"To: bob@example.test, carol@example.test",
		"Cc: dave@example.test",
		"Subject: Quarterly report",
		"Date: ",
		"Message-ID: <",
		"MIME-Version: 1.0",
		"Hello Bob",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("DATA payload missing %q\nDATA:\n%s", want, data)
		}
	}
	if strings.Contains(data, "hidden@example.test") {
		t.Errorf("DATA payload leaked a Bcc recipient:\n%s", data)
	}
	if strings.Contains(data, "Bcc:") {
		t.Errorf("DATA payload leaked a Bcc header:\n%s", data)
	}

	for _, cmd := range sess.commands {
		if strings.HasPrefix(cmd, "AUTH") {
			t.Errorf("AUTH attempted without configured credentials: %q", cmd)
		}
		if cmd == "STARTTLS" {
			t.Errorf("STARTTLS attempted under TLSDisabled config: %q", cmd)
		}
	}
	if len(sess.commands) == 0 || sess.commands[len(sess.commands)-1] != "QUIT" {
		t.Errorf("dialog did not end with QUIT: %v", sess.commands)
	}
}

// Case (b): server without STARTTLS plus the default TLSStartTLS
// config must fail loudly instead of continuing in plaintext.
func TestSMTPSenderRequiresSTARTTLS(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{}) // no STARTTLS advertised
	cfg := srv.config(t)                               // default TLSStartTLS
	sender := NewSMTPSender(cfg)

	err := sender.Send(context.Background(), validTestMessage())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("Send() error = %v, want failure mentioning STARTTLS", err)
	}

	sess := srv.waitSession(t)
	for _, cmd := range sess.commands {
		if strings.HasPrefix(cmd, "MAIL") {
			t.Errorf("dialog proceeded to MAIL FROM despite missing STARTTLS: %q", cmd)
		}
	}
}

// Case (c): credentials configured but plaintext connection: AUTH must
// never be attempted.
func TestSMTPSenderNeverAuthenticatesOverPlaintext(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{acceptAUTH: true})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	cfg.Username = "user@example.test"
	cfg.Password = "secret"
	sender := NewSMTPSender(cfg)

	if err := sender.Send(context.Background(), validTestMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)
	if sess.hadAUTH {
		t.Errorf("AUTH was attempted over plaintext: %q", sess.authArg)
	}
}

// Case (d): an already-cancelled context must prevent any dial.
func TestSMTPSenderCancelledContextDoesNotDial(t *testing.T) {
	dials := withDialRecorder(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sender := NewSMTPSender(SMTPConfig{Host: "smtp.example.test", From: "envelope@example.test"})
	err := sender.Send(ctx, validTestMessage())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if got := dials(); got != 0 {
		t.Errorf("dialed %d times with cancelled context, want 0", got)
	}
}

func TestSMTPSenderHostRequired(t *testing.T) {
	dials := withDialRecorder(t)

	sender := NewSMTPSender(SMTPConfig{})
	err := sender.Send(context.Background(), validTestMessage())
	if err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("Send() error = %v, want host required error", err)
	}
	if got := dials(); got != 0 {
		t.Errorf("dialed %d times without a host, want 0", got)
	}
}

func TestSMTPSenderInvalidMessageDoesNotDial(t *testing.T) {
	dials := withDialRecorder(t)

	bad := validTestMessage()
	bad.To = []string{"not a valid address"}

	srv := startFakeSMTPServer(t, fakeServerOptions{})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	err := NewSMTPSender(cfg).Send(context.Background(), bad)
	if err == nil || !strings.Contains(err.Error(), "invalid message") {
		t.Fatalf("Send() error = %v, want invalid message error", err)
	}
	if got := dials(); got != 0 {
		t.Errorf("dialed %d times for an invalid message, want 0", got)
	}
}

// Case (e): a 5xx server reply mid-dialog must surface as an error.
func TestSMTPSenderServerRejectsMailFrom(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{
		failCommands: map[string]string{"MAIL": "550 5.1.7 sender address rejected"},
	})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	sender := NewSMTPSender(cfg)

	err := sender.Send(context.Background(), validTestMessage())
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("Send() error = %v, want 550 server error surfaced", err)
	}
	sess := srv.waitSession(t)
	if len(sess.rcpts) != 0 {
		t.Errorf("RCPT commands issued after failed MAIL FROM: %v", sess.rcpts)
	}
}

// Case (f): full STARTTLS upgrade with self-signed server certificate
// followed by AUTH PLAIN, both over TLS.
func TestSMTPSenderSTARTTLSAndAuthOverTLS(t *testing.T) {
	cert, pool := selfSignedCert(t)
	srv := startFakeSMTPServer(t, fakeServerOptions{
		advertiseSTARTTLS: true,
		acceptAUTH:        true,
		tlsConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	})
	withTLSClientConfig(t, pool)

	cfg := srv.config(t) // default TLSStartTLS
	cfg.Username = "user@example.test"
	cfg.Password = "secret"
	sender := NewSMTPSender(cfg)

	msg := validTestMessage()
	msg.HTML = "<p>Hello Bob</p>"
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)

	if !sess.startTLS {
		t.Errorf("STARTTLS was not negotiated, commands: %v", sess.commands)
	}
	if !sess.hadAUTH || !strings.HasPrefix(sess.authArg, "PLAIN ") {
		t.Errorf("AUTH PLAIN not negotiated: hadAUTH=%v authArg=%q", sess.hadAUTH, sess.authArg)
	}
	if !strings.Contains(string(sess.data), "multipart/alternative") {
		t.Errorf("multipart message expected in DATA:\n%s", sess.data)
	}
}

// Case (g): implicit TLS from the first byte, no STARTTLS exchange.
func TestSMTPSenderImplicitTLS(t *testing.T) {
	cert, pool := selfSignedCert(t)
	srv := startFakeSMTPServer(t, fakeServerOptions{
		implicitTLS: true,
		tlsConfig:   &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	})
	withTLSClientConfig(t, pool)

	cfg := srv.config(t)
	cfg.TLSMode = TLSImplicit
	sender := NewSMTPSender(cfg)

	msg := validTestMessage()
	msg.To = []string{"bob@example.test", "hidden@example.test"}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)

	if got, want := sess.mailFrom, "alice@example.test"; got != want {
		t.Errorf("MAIL FROM = %q, want %q (message From; config From unset)", got, want)
	}
	if !reflect.DeepEqual(sess.rcpts, msg.To) {
		t.Errorf("RCPT TO list = %v, want %v", sess.rcpts, msg.To)
	}
	for _, cmd := range sess.commands {
		if cmd == "STARTTLS" {
			t.Errorf("unexpected STARTTLS under TLSImplicit: %q", cmd)
		}
	}
	if !sess.quit {
		t.Errorf("dialog did not finish cleanly, commands: %v", sess.commands)
	}
}

// The envelope sender falls back to the message From when the config
// does not set one.
func TestSMTPSenderEnvelopeFromFallsBackToMessage(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	sender := NewSMTPSender(cfg)

	if err := sender.Send(context.Background(), validTestMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)
	if got, want := sess.mailFrom, "alice@example.test"; got != want {
		t.Errorf("MAIL FROM = %q, want %q", got, want)
	}
}

// unstuffData reverses SMTP dot-stuffing on a recorded DATA payload and
// drops the terminating "." line, yielding the message as rendered.
func unstuffData(t *testing.T, data []byte) string {
	t.Helper()
	s, ok := strings.CutSuffix(string(data), ".\r\n")
	if !ok {
		t.Fatalf("DATA payload does not end with the terminating dot line: %q", data)
	}
	lines := strings.Split(s, "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "..") {
			lines[i] = line[1:]
		}
	}
	return strings.Join(lines, "\r\n")
}

// Display names are allowed in every address field, but only bare
// addresses may reach MAIL FROM / RCPT TO.
func TestSMTPSenderEnvelopeStripsDisplayNames(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	sender := NewSMTPSender(cfg)

	msg := Message{
		From:    "Alice Example <alice@example.test>",
		To:      []string{"Bob <bob@example.test>", `"Doe, John" <john@example.test>`},
		Cc:      []string{"Zoë Núñez <zoe@example.test>"},
		Bcc:     []string{"Hidden Person <hidden@example.test>"},
		Subject: "Hello",
		Text:    "hi",
	}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)

	if got, want := sess.mailFrom, "alice@example.test"; got != want {
		t.Errorf("MAIL FROM = %q, want %q", got, want)
	}
	wantRcpts := []string{"bob@example.test", "john@example.test", "zoe@example.test", "hidden@example.test"}
	if !reflect.DeepEqual(sess.rcpts, wantRcpts) {
		t.Errorf("RCPT TO list = %v, want %v", sess.rcpts, wantRcpts)
	}
	data := string(sess.data)
	for _, want := range []string{
		`From: "Alice Example" <alice@example.test>`,
		`To: "Bob" <bob@example.test>, "Doe, John" <john@example.test>`,
		"Cc: =?utf-8?q?Zo=C3=AB_N=C3=BA=C3=B1ez?= <zoe@example.test>",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("DATA payload missing %q\nDATA:\n%s", want, data)
		}
	}
	if strings.Contains(data, "hidden@example.test") || strings.Contains(data, "Hidden Person") {
		t.Errorf("DATA payload leaked a Bcc recipient:\n%s", data)
	}
}

// SMTPConfig.From may carry a display name: the envelope gets the bare
// address and the From header keeps the name.
func TestSMTPSenderConfigFromWithDisplayName(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	cfg.From = "My App <no-reply@example.test>"
	sender := NewSMTPSender(cfg)

	msg := validTestMessage()
	msg.From = ""
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)

	if got, want := sess.mailFrom, "no-reply@example.test"; got != want {
		t.Errorf("MAIL FROM = %q, want %q", got, want)
	}
	if want := `From: "My App" <no-reply@example.test>`; !strings.Contains(string(sess.data), want) {
		t.Errorf("DATA payload missing %q\nDATA:\n%s", want, sess.data)
	}
}

func TestSMTPSenderSenderAddressErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfgFrom string
		msgFrom string
		want    string
	}{
		{"no sender anywhere", "", "", "no sender address"},
		{"whitespace only everywhere", "  ", "  ", "no sender address"},
		{"invalid config from", "not an address", "alice@example.test", "smtp config from"},
		{"invalid config from used as message from", "not an address", "", "invalid message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dials := withDialRecorder(t)
			sender := NewSMTPSender(SMTPConfig{Host: "smtp.example.test", From: tt.cfgFrom, TLSMode: TLSDisabled})

			msg := validTestMessage()
			msg.From = tt.msgFrom
			err := sender.Send(context.Background(), msg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Send() error = %v, want error containing %q", err, tt.want)
			}
			if got := dials(); got != 0 {
				t.Errorf("dialed %d times, want 0", got)
			}
		})
	}
}

// Lines starting with '.' (including a lone '.') must be dot-stuffed on
// the wire so they can never terminate DATA early, and must arrive
// intact after unstuffing and quoted-printable decoding.
func TestSMTPSenderDotLinesAndQuotedPrintable(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	sender := NewSMTPSender(cfg)

	body := "first\n.\n.hidden command\nnaïve résumé token=abc " + strings.Repeat("z", 1200) + "\nlast"
	msg := validTestMessage()
	msg.Text = body
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	sess := srv.waitSession(t)
	if !sess.quit {
		t.Fatalf("dialog did not finish cleanly (DATA terminated early?), commands: %v", sess.commands)
	}
	if !strings.Contains(string(sess.data), "\r\n..\r\n") || !strings.Contains(string(sess.data), "\r\n..hidden command") {
		t.Errorf("dot lines were not stuffed on the wire:\n%s", sess.data)
	}

	wire := []byte(unstuffData(t, sess.data))
	assertWireSafe(t, wire, 76)
	hdr, encoded := splitWire(t, wire)
	if got := hdr.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q, want quoted-printable", got)
	}
	if got, want := decodeQP(t, encoded), crlf(body)+"\r\n"; got != want {
		t.Errorf("decoded body = %q, want %q", got, want)
	}
}
