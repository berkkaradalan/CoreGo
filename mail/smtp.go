package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// TLSMode selects how an SMTPSender secures its connection.
type TLSMode int

const (
	// TLSStartTLS connects over plaintext and upgrades the session
	// with the STARTTLS extension. It is the zero value and therefore
	// the default. If the server does not advertise STARTTLS, sending
	// fails with a clear error rather than continuing in plaintext.
	TLSStartTLS TLSMode = iota
	// TLSImplicit establishes TLS before the SMTP dialog starts
	// (formerly "SMTPS", typically served on port 465).
	TLSImplicit
	// TLSDisabled speaks plaintext SMTP for the whole dialog. The
	// message body travels unencrypted, so this mode is only intended
	// for local development servers. Credentials are never
	// transmitted in this mode (see SMTPConfig.Username).
	TLSDisabled
)

// String returns a human-readable name for the mode.
func (m TLSMode) String() string {
	switch m {
	case TLSStartTLS:
		return "starttls"
	case TLSImplicit:
		return "implicit"
	case TLSDisabled:
		return "disabled"
	default:
		return fmt.Sprintf("TLSMode(%d)", int(m))
	}
}

// SMTPConfig configures an SMTPSender.
type SMTPConfig struct {
	// Host is the SMTP server hostname or IP address. Required; Send
	// fails without it.
	Host string
	// Port is the SMTP server port. Zero defaults to 587.
	Port int
	// Username and Password enable SMTP authentication (AUTH PLAIN).
	// When Username is empty, no authentication is attempted.
	// Credentials are only ever sent over a TLS session: under
	// TLSDisabled authentication is silently skipped so credentials
	// can never leak onto a plaintext channel.
	Username string
	Password string
	// From is the envelope sender (MAIL FROM) and the default value
	// for Message.From when a message does not set its own From. It
	// may carry a display name ("My App <no-reply@example.com>"); the
	// envelope uses the bare address and the From header keeps the
	// name.
	From string
	// TLSMode selects STARTTLS (default), implicit TLS or plaintext.
	TLSMode TLSMode
	// Timeout bounds both the connection dial and every individual
	// SMTP command exchange. Zero defaults to 10 seconds.
	Timeout time.Duration
}

// SMTPSender delivers messages over SMTP using net/smtp. It is safe
// for concurrent use: each Send runs an independent dialog on its own
// connection.
type SMTPSender struct {
	cfg SMTPConfig
}

var _ Sender = (*SMTPSender)(nil)

// dialSMTPConn is the seam over which SMTPSender obtains its TCP
// connection, with signature dialSMTPConn(ctx, timeout, network,
// addr). It is an unexported package-level variable so tests can
// observe or replace connection establishment (for example, asserting
// that an already-cancelled context prevents any dial at all). The
// production implementation dials via net.Dialer.DialContext with a
// timeout derived from the configured Timeout.
var dialSMTPConn = defaultDialSMTPConn

// defaultDialSMTPConn dials addr honoring ctx and a connect timeout of
// timeout (when positive).
func defaultDialSMTPConn(ctx context.Context, timeout time.Duration, network, addr string) (net.Conn, error) {
	var d net.Dialer
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return d.DialContext(ctx, network, addr)
}

// tlsConfigFor is the seam producing the client TLS configuration used
// for both implicit TLS and STARTTLS upgrades. Swappable so tests can
// trust a local TLS server with a self-signed certificate.
var tlsConfigFor = defaultTLSConfigFor

// defaultTLSConfigFor verifies the server certificate against public
// roots using the configured Host as SNI/verification name.
func defaultTLSConfigFor(host string) *tls.Config {
	return &tls.Config{ServerName: host}
}

// smtpLocalName is the client name announced in the EHLO command.
const smtpLocalName = "localhost"

// NewSMTPSender returns an SMTPSender with defaults applied: Port 587,
// Timeout 10s and TLSStartTLS when unset.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	if cfg.Port <= 0 {
		cfg.Port = 587
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPSender{cfg: cfg}
}

// Send implements Sender. It validates the message (filling
// Message.From from the config when empty), renders the wire bytes,
// then runs one SMTP dialog:
//
//	dial -> [implicit TLS handshake] -> EHLO -> [STARTTLS] -> [AUTH]
//	-> MAIL FROM -> RCPT TO (To + Cc + Bcc) -> DATA -> QUIT
//
// A message without any sender address (neither Message.From nor
// SMTPConfig.From) is rejected before dialing. The envelope sender is
// cfg.From when set, otherwise the message From. The envelope recipient
// list always includes To, Cc and Bcc; Bcc travels in the envelope only
// and never in the message headers (see buildMessage). Display names
// never reach the envelope: "Ada <ada@example.com>" becomes
// "ada@example.com" in MAIL FROM and RCPT TO. ctx is checked before
// dialing and between every command, and the configured Timeout is
// applied as the connect timeout and as a deadline before each command.
//
// Errors never contain email addresses, so they are safe to log: a
// rejected recipient is named by field and index ("mail: smtp: RCPT TO
// for cc[0]: 550 ..."), and addresses quoted in server replies are
// replaced with "[redacted]". A server reply stays available as a
// *net/textproto.Error (with its Code) through errors.As.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mail: smtp: cancelled before dial: %w", err)
	}
	if s.cfg.Host == "" {
		return errors.New("mail: smtp: host is required")
	}

	// An empty or whitespace-only From falls back to the configured
	// sender address; validation then checks whichever value remains.
	if strings.TrimSpace(msg.From) == "" {
		msg.From = s.cfg.From
	}
	if strings.TrimSpace(msg.From) == "" {
		return errors.New("mail: smtp: no sender address: set Message.From or SMTPConfig.From")
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("mail: smtp: invalid message: %w", err)
	}
	wire, err := buildMessage(msg)
	if err != nil {
		return err
	}
	envelopeFrom, recipients, err := s.envelope(msg)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	conn, err := dialSMTPConn(ctx, s.cfg.Timeout, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: smtp: dial %s: %w", addr, err)
	}
	defer conn.Close()

	if s.cfg.TLSMode == TLSImplicit {
		if err := s.step(ctx, conn); err != nil {
			return err
		}
		tlsConn := tls.Client(conn, tlsConfigFor(s.cfg.Host))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return smtpError("tls handshake with "+addr, err)
		}
		conn = tlsConn
	}

	if err := s.step(ctx, conn); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return smtpError("greeting from "+addr, err)
	}

	if err := s.step(ctx, conn); err != nil {
		return err
	}
	if err := client.Hello(smtpLocalName); err != nil {
		return smtpError("EHLO", err)
	}

	usingTLS := s.cfg.TLSMode == TLSImplicit
	if s.cfg.TLSMode == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("mail: smtp: server %s does not advertise STARTTLS; refusing to send over plaintext", addr)
		}
		if err := s.step(ctx, conn); err != nil {
			return err
		}
		if err := client.StartTLS(tlsConfigFor(s.cfg.Host)); err != nil {
			return smtpError("STARTTLS upgrade", err)
		}
		usingTLS = true
	}

	// Never send credentials over a plaintext channel: authentication
	// runs only when the session is TLS (implicit or upgraded). Under
	// TLSDisabled it is skipped entirely, even if configured.
	if s.cfg.Username != "" && usingTLS {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := s.step(ctx, conn); err != nil {
			return err
		}
		if err := client.Auth(auth); err != nil {
			return smtpError("AUTH PLAIN", err)
		}
	}

	if err := s.step(ctx, conn); err != nil {
		return err
	}
	if err := client.Mail(envelopeFrom); err != nil {
		return smtpError("MAIL FROM", err)
	}
	for _, rcpt := range recipients {
		if err := s.step(ctx, conn); err != nil {
			return err
		}
		if err := client.Rcpt(rcpt.addr); err != nil {
			return smtpError("RCPT TO for "+rcpt.field, err)
		}
	}

	if err := s.step(ctx, conn); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return smtpError("DATA", err)
	}
	if _, err := w.Write(wire); err != nil {
		w.Close()
		return smtpError("write message", err)
	}
	if err := w.Close(); err != nil {
		return smtpError("finish DATA", err)
	}

	if err := s.step(ctx, conn); err != nil {
		return err
	}
	if err := client.Quit(); err != nil {
		return smtpError("QUIT", err)
	}
	return nil
}

// step enforces the context and per-command timeout before each SMTP
// exchange: it fails fast when ctx is already done and otherwise
// pushes the connection deadline out by the configured Timeout.
func (s *SMTPSender) step(ctx context.Context, conn net.Conn) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mail: smtp: context done: %w", err)
	}
	if s.cfg.Timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(s.cfg.Timeout)); err != nil {
			return fmt.Errorf("mail: smtp: set deadline: %w", err)
		}
	}
	return nil
}

// envelopeRecipient is one RCPT TO address together with the message
// field it came from ("to[0]", "bcc[2]"), used in error messages in
// place of the address.
type envelopeRecipient struct {
	field string
	addr  string
}

// envelope returns the bare addresses (display names stripped) for the
// SMTP envelope: the sender for MAIL FROM, which is cfg.From when set
// and the message From otherwise, and every recipient for RCPT TO: To
// and Cc for regular delivery, plus Bcc which is delivered through the
// envelope only (never through headers).
func (s *SMTPSender) envelope(msg Message) (string, []envelopeRecipient, error) {
	sender := s.cfg.From
	field := "smtp config from"
	if strings.TrimSpace(sender) == "" {
		sender, field = msg.From, "from"
	}
	from, err := envelopeAddress(field, sender)
	if err != nil {
		return "", nil, err
	}

	recipients := make([]envelopeRecipient, 0, len(msg.To)+len(msg.Cc)+len(msg.Bcc))
	for _, list := range msg.addressLists() {
		for i, raw := range list.addrs {
			field := indexedField(list.field, i)
			addr, err := envelopeAddress(field, raw)
			if err != nil {
				return "", nil, err
			}
			recipients = append(recipients, envelopeRecipient{field: field, addr: addr})
		}
	}
	return from, recipients, nil
}
