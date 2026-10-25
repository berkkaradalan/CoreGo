package mail

import (
	"context"
	"errors"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

// secretAddr is the address the no-leak tests plant; no error message
// may ever contain it, nor its local part.
const secretAddr = "secret.person@example.test"

// assertNoAddress fails when msg contains secretAddr, its local part or
// any '@'.
func assertNoAddress(t *testing.T, msg string) {
	t.Helper()
	for _, leak := range []string{secretAddr, "secret.person", "@"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error message %q leaks %q", msg, leak)
		}
	}
}

func TestRedactAddresses(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "no at sign", in: "550 5.1.1 user unknown", want: "550 5.1.1 user unknown"},
		{name: "empty", in: "", want: ""},
		{name: "bare address", in: "bob@example.com unknown", want: "[redacted] unknown"},
		{name: "angle brackets", in: "5.1.1 <bob@example.com>: Recipient address rejected", want: "5.1.1 <[redacted]>: Recipient address rejected"},
		{name: "two addresses", in: "a@x.test, b@y.test", want: "[redacted], [redacted]"},
		{name: "adjacent addresses", in: "<a@x.test><b@y.test>", want: "<[redacted]><[redacted]>"},
		{name: "parentheses", in: "(bob@example.com)", want: "([redacted])"},
		{name: "single quotes", in: "'bob@example.com'", want: "'[redacted]'"},
		{name: "quoted local part", in: `5.1.1 "john doe"@example.com unknown`, want: "5.1.1 [redacted] unknown"},
		{name: "quoted local part in brackets", in: `<"john doe"@example.com>`, want: "<[redacted]>"},
		{name: "ip literal domain", in: "<bob@[192.0.2.1]>", want: "<[redacted]>"},
		{name: "plus tag and dots", in: "to: first.last+tag@sub.example.co.uk;", want: "to: [redacted];"},
		{name: "lone at sign", in: "550 @ nothing", want: "550 @ nothing"},
		{name: "nothing after at", in: "user@ is invalid", want: "user@ is invalid"},
		{name: "nothing before at", in: "@example.com is invalid", want: "@example.com is invalid"},
		{name: "at sign at end", in: "trailing@", want: "trailing@"},
		{name: "multiple at signs in one token", in: "a@b@c.test", want: "[redacted]"},
		{name: "unicode around", in: "ùser@exåmple.test rejected", want: "[redacted] rejected"},
		{name: "tab and newline delimiters", in: "x\tbob@example.com\nnext", want: "x\t[redacted]\nnext"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactAddresses(tt.in); got != tt.want {
				t.Errorf("redactAddresses(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactError(t *testing.T) {
	if redactError(nil) != nil {
		t.Error("redactError(nil) != nil")
	}

	plain := errors.New("connection reset")
	if got := redactError(plain); got != plain {
		t.Errorf("error without address was replaced: %v", got)
	}

	reply := &textproto.Error{Code: 550, Msg: "5.1.1 <" + secretAddr + ">: user unknown"}
	got := redactError(reply)
	assertNoAddress(t, got.Error())
	var tp *textproto.Error
	if !errors.As(got, &tp) || tp.Code != 550 || tp.Msg != "5.1.1 <[redacted]>: user unknown" {
		t.Errorf("redacted reply = %#v, want code 550 and redacted Msg", tp)
	}
	if reply.Msg == tp.Msg {
		t.Error("original reply was mutated or returned as-is")
	}

	other := &wrappedTimeout{msg: "timeout talking to " + secretAddr}
	got = redactError(other)
	assertNoAddress(t, got.Error())
	if got.Error() != "timeout talking to [redacted]" {
		t.Errorf("redacted error = %q", got.Error())
	}
	if !errors.Is(got, os.ErrDeadlineExceeded) {
		t.Error("redacted error lost its cause for errors.Is")
	}
}

// replyText renders a server reply the way net/textproto does (its
// quoting differs between Go versions).
func replyText(code int, msg string) string {
	return (&textproto.Error{Code: code, Msg: msg}).Error()
}

// wrappedTimeout is an error with an address in its message that wraps
// os.ErrDeadlineExceeded.
type wrappedTimeout struct{ msg string }

func (e *wrappedTimeout) Error() string { return e.msg }
func (e *wrappedTimeout) Unwrap() error { return os.ErrDeadlineExceeded }

// TestValidateErrorsNeverContainAddresses plants secretAddr (made
// invalid or injection-bearing) in every address field and checks the
// exact field+index error, which must not contain the address.
func TestValidateErrorsNeverContainAddresses(t *testing.T) {
	invalid := "Secret Person <" + secretAddr // unclosed angle-addr
	injected := secretAddr + "\r\nBcc: other@example.test"
	tests := []struct {
		name string
		mut  func(*Message)
		want string
	}{
		{"invalid from", func(m *Message) { m.From = invalid }, "mail: from: invalid address"},
		{"invalid reply-to", func(m *Message) { m.ReplyTo = invalid }, "mail: reply-to: invalid address"},
		{"invalid to", func(m *Message) { m.To = []string{"ok@example.test", invalid} }, "mail: to[1]: invalid address"},
		{"invalid cc", func(m *Message) { m.Cc = []string{invalid} }, "mail: cc[0]: invalid address"},
		{"invalid bcc", func(m *Message) { m.Bcc = []string{"ok@example.test", "ok2@example.test", invalid} }, "mail: bcc[2]: invalid address"},
		{"two addresses in one entry", func(m *Message) { m.To = []string{secretAddr + ", " + secretAddr} }, "mail: to[0]: invalid address"},
		{"injection in from", func(m *Message) { m.From = injected }, "mail: from: contains CR or LF (possible header injection)"},
		{"injection in reply-to", func(m *Message) { m.ReplyTo = injected }, "mail: reply-to: contains CR or LF (possible header injection)"},
		{"injection in to", func(m *Message) { m.To = []string{"ok@example.test", injected} }, "mail: to[1]: contains CR or LF (possible header injection)"},
		{"injection in cc", func(m *Message) { m.Cc = []string{injected} }, "mail: cc[0]: contains CR or LF (possible header injection)"},
		{"injection in bcc", func(m *Message) { m.Bcc = []string{injected} }, "mail: bcc[0]: contains CR or LF (possible header injection)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{To: []string{"ok@example.test"}, Subject: "Hello", Text: "hi"}
			tt.mut(&msg)
			err := msg.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() = %q, want %q", err, tt.want)
			}
			assertNoAddress(t, err.Error())

			// The same holds for the senders, which validate first.
			for name, s := range map[string]Sender{
				"LogSender":  NewLogSender(nil),
				"SMTPSender": NewSMTPSender(SMTPConfig{Host: "smtp.invalid", From: "app@example.test"}),
			} {
				if err := s.Send(context.Background(), msg); err == nil {
					t.Errorf("%s.Send() = nil, want error", name)
				} else {
					assertNoAddress(t, err.Error())
				}
			}
		})
	}
}

// TestWireAndEnvelopeErrorsNeverContainAddresses covers the helpers
// used after validation (direct buildMessage callers, envelope
// construction).
func TestWireAndEnvelopeErrorsNeverContainAddresses(t *testing.T) {
	invalid := "<" + secretAddr
	msg := validTestMessage()
	msg.Cc = []string{"ok@example.test", invalid}
	if _, err := buildMessage(msg); err == nil || err.Error() != "mail: cc[1]: invalid address" {
		t.Errorf("buildMessage() error = %v, want mail: cc[1]: invalid address", err)
	} else {
		assertNoAddress(t, err.Error())
	}

	s := NewSMTPSender(SMTPConfig{Host: "smtp.invalid"})
	msg = validTestMessage()
	msg.Bcc = []string{invalid}
	if _, _, err := s.envelope(msg); err == nil || err.Error() != "mail: bcc[0]: invalid address" {
		t.Errorf("envelope() error = %v, want mail: bcc[0]: invalid address", err)
	} else {
		assertNoAddress(t, err.Error())
	}

	s = NewSMTPSender(SMTPConfig{Host: "smtp.invalid", From: invalid})
	if _, _, err := s.envelope(validTestMessage()); err == nil || err.Error() != "mail: smtp config from: invalid address" {
		t.Errorf("envelope() error = %v, want mail: smtp config from: invalid address", err)
	} else {
		assertNoAddress(t, err.Error())
	}
}

// Regression for the delivery-log leak: a server rejecting a recipient
// usually quotes it in its reply ("550 5.1.1 <addr>: ..."), and the
// error used to name the recipient too. The error must identify the
// recipient by field and index only.
func TestSMTPSenderRecipientRejectionDoesNotLeakAddress(t *testing.T) {
	tests := []struct {
		name      string
		rcptIndex int // index across To, Cc, Bcc in envelope order
		wantField string
	}{
		{name: "first to", rcptIndex: 0, wantField: "to[0]"},
		{name: "second to", rcptIndex: 1, wantField: "to[1]"},
		{name: "cc", rcptIndex: 2, wantField: "cc[0]"},
		{name: "bcc", rcptIndex: 3, wantField: "bcc[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startFakeSMTPServer(t, fakeServerOptions{
				rcptReplies: map[int]string{tt.rcptIndex: "550 5.1.1 <" + secretAddr + ">: Recipient address rejected"},
			})
			cfg := srv.config(t)
			cfg.TLSMode = TLSDisabled
			cfg.From = "app@example.test"
			msg := Message{
				To:      []string{"Secret Person <" + secretAddr + ">", "Other <" + secretAddr + ">"},
				Cc:      []string{secretAddr},
				Bcc:     []string{secretAddr},
				Subject: "Hello",
				Text:    "hi",
			}
			err := NewSMTPSender(cfg).Send(context.Background(), msg)
			if err == nil {
				t.Fatal("Send() = nil, want RCPT error")
			}
			want := "mail: smtp: RCPT TO for " + tt.wantField + ": " + replyText(550, "5.1.1 <[redacted]>: Recipient address rejected")
			if err.Error() != want {
				t.Errorf("Send() error = %q, want %q", err, want)
			}
			assertNoAddress(t, err.Error())
			var reply *textproto.Error
			if !errors.As(err, &reply) || reply.Code != 550 {
				t.Errorf("errors.As(*textproto.Error) = %v (code %v), want code 550", reply != nil, reply)
			}
			sess := srv.waitSession(t)
			if len(sess.rcpts) != tt.rcptIndex+1 {
				t.Errorf("RCPT commands = %d, want %d (stop at the first rejection)", len(sess.rcpts), tt.rcptIndex+1)
			}
		})
	}
}

func TestSMTPSenderMailFromRejectionDoesNotLeakAddress(t *testing.T) {
	srv := startFakeSMTPServer(t, fakeServerOptions{
		failCommands: map[string]string{"MAIL": "553 5.7.1 <" + secretAddr + ">: Sender address rejected"},
	})
	cfg := srv.config(t)
	cfg.TLSMode = TLSDisabled
	cfg.From = "Secret Person <" + secretAddr + ">"
	msg := validTestMessage()
	msg.From = ""
	err := NewSMTPSender(cfg).Send(context.Background(), msg)
	want := "mail: smtp: MAIL FROM: " + replyText(553, "5.7.1 <[redacted]>: Sender address rejected")
	if err == nil || err.Error() != want {
		t.Fatalf("Send() error = %v, want %q", err, want)
	}
	assertNoAddress(t, err.Error())
}

// Replies of every other stage are redacted as well (an AUTH failure
// may echo a username that is an email address).
func TestSMTPSenderOtherStageRepliesAreRedacted(t *testing.T) {
	tests := []struct {
		verb, stage string
	}{
		{verb: "EHLO", stage: "EHLO"},
		{verb: "DATA", stage: "DATA"},
		{verb: "QUIT", stage: "QUIT"},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			fail := map[string]string{tt.verb: "554 5.0.0 rejected for " + secretAddr}
			if tt.verb == "EHLO" {
				// net/smtp falls back to HELO when EHLO fails.
				fail["HELO"] = fail["EHLO"]
			}
			srv := startFakeSMTPServer(t, fakeServerOptions{failCommands: fail})
			cfg := srv.config(t)
			cfg.TLSMode = TLSDisabled
			err := NewSMTPSender(cfg).Send(context.Background(), validTestMessage())
			want := "mail: smtp: " + tt.stage + ": " + replyText(554, "5.0.0 rejected for [redacted]")
			if err == nil || err.Error() != want {
				t.Fatalf("Send() error = %v, want %q", err, want)
			}
		})
	}
}
