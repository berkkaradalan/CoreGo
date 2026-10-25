package mail

import (
	"bufio"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"
	"testing"
	"time"
)

// validTestMessage returns the smallest message that passes Validate.
// Shared by the other test files in this package.
func validTestMessage() Message {
	return Message{
		From:    "alice@example.test",
		To:      []string{"bob@example.test"},
		Subject: "Hello",
		Text:    "Hello Bob",
	}
}

// withFrozenClock replaces the nowFunc seam with a fixed clock for the
// duration of the test.
func withFrozenClock(t *testing.T, ts time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return ts }
	t.Cleanup(func() { nowFunc = orig })
}

// withFixedHostname replaces the hostnameFunc seam with a fixed host
// for the duration of the test.
func withFixedHostname(t *testing.T, host string) {
	t.Helper()
	orig := hostnameFunc
	hostnameFunc = func() string { return host }
	t.Cleanup(func() { hostnameFunc = orig })
}

// splitWire splits rendered wire bytes into parsed headers and the
// body that follows the blank separator line.
func splitWire(t *testing.T, wire []byte) (textproto.MIMEHeader, string) {
	t.Helper()
	s := string(wire)
	idx := strings.Index(s, "\r\n\r\n")
	if idx < 0 {
		t.Fatalf("no header/body separator in wire output: %q", s)
	}
	hdr, err := textproto.NewReader(bufio.NewReader(strings.NewReader(s[:idx+4]))).ReadMIMEHeader()
	if err != nil {
		t.Fatalf("parse headers: %v", err)
	}
	return hdr, s[idx+4:]
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr string // empty means the message must be valid
	}{
		{
			name: "valid minimal",
			msg:  validTestMessage(),
		},
		{
			name: "valid full",
			msg: Message{
				From:    "Alice <alice@example.test>",
				To:      []string{"bob@example.test", "Carol <carol@example.test>"},
				Cc:      []string{"dave@example.test"},
				Bcc:     []string{"hidden@example.test"},
				ReplyTo: "support@example.test",
				Subject: "Quarterly report",
				Text:    "plain body",
				HTML:    "<p>html body</p>",
			},
		},
		{
			name: "valid html only",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Subject: "Hello",
				HTML:    "<p>Hello Bob</p>",
			},
		},
		{
			// Empty From is valid: senders supply their own default
			// (for example SMTPSender fills it from SMTPConfig.From).
			name: "empty from is allowed",
			msg:  Message{To: []string{"bob@example.test"}, Subject: "Hello", Text: "hi"},
		},
		{
			name: "whitespace from is treated as empty and allowed",
			msg:  Message{From: "   ", To: []string{"bob@example.test"}, Subject: "Hello", Text: "hi"},
		},
		{
			name:    "unparseable from",
			msg:     Message{From: "not an address", To: []string{"bob@example.test"}, Subject: "Hello", Text: "hi"},
			wantErr: "mail: from: invalid address",
		},
		{
			name:    "no recipients",
			msg:     Message{From: "alice@example.test", Subject: "Hello", Text: "hi"},
			wantErr: "at least one recipient",
		},
		{
			name: "unparseable to entry",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test", "not valid @@example.test"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "mail: to[1]: invalid address",
		},
		{
			name: "unparseable cc entry",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Cc:      []string{"bad cc address"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "mail: cc[0]: invalid address",
		},
		{
			name: "unparseable bcc entry",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Bcc:     []string{"bad bcc address"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "mail: bcc[0]: invalid address",
		},
		{
			name: "valid reply-to with display name",
			msg: Message{
				To:      []string{"bob@example.test"},
				ReplyTo: "Support Team <support@example.test>",
				Subject: "Hello",
				Text:    "hi",
			},
		},
		{
			name: "valid non-ascii display names",
			msg: Message{
				From:    "José Núñez <jose@example.test>",
				To:      []string{"Zoë <zoe@example.test>", `"Doe, John" <john@example.test>`},
				Subject: "Hello",
				Text:    "hi",
			},
		},
		{
			name: "unparseable reply-to",
			msg: Message{
				To:      []string{"bob@example.test"},
				ReplyTo: "not an address",
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "mail: reply-to: invalid address",
		},
		{
			name:    "missing subject",
			msg:     Message{From: "alice@example.test", To: []string{"bob@example.test"}, Text: "hi"},
			wantErr: "subject",
		},
		{
			name: "whitespace subject",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Subject: "   ",
				Text:    "hi",
			},
			wantErr: "subject",
		},
		{
			name:    "empty body",
			msg:     Message{From: "alice@example.test", To: []string{"bob@example.test"}, Subject: "Hello"},
			wantErr: "text or html",
		},
		{
			name: "crlf injection in subject",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Subject: "hi\r\nBcc: attacker@example.test",
				Text:    "hi",
			},
			wantErr: "injection",
		},
		{
			name: "bare lf injection in subject",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Subject: "hi\nBcc: attacker@example.test",
				Text:    "hi",
			},
			wantErr: "injection",
		},
		{
			name: "crlf injection in from",
			msg: Message{
				From:    "alice@example.test\r\nBcc: attacker@example.test",
				To:      []string{"bob@example.test"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "injection",
		},
		{
			name: "crlf injection in to entry",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test\r\nBcc: attacker@example.test"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "injection",
		},
		{
			name: "crlf injection in cc entry",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				Cc:      []string{"dave@example.test\nBcc: attacker@example.test"},
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "injection",
		},
		{
			name: "crlf injection in reply-to",
			msg: Message{
				From:    "alice@example.test",
				To:      []string{"bob@example.test"},
				ReplyTo: "support@example.test\r\nBcc: attacker@example.test",
				Subject: "Hello",
				Text:    "hi",
			},
			wantErr: "injection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("Validate() error = nil, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildMessageHeaders(t *testing.T) {
	withFrozenClock(t, time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC))
	withFixedHostname(t, "example.test")

	msg := validTestMessage()
	msg.To = []string{"bob@example.test", "carol@example.test"}
	msg.Cc = []string{"dave@example.test"}
	msg.ReplyTo = "support@example.test"

	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	hdr, body := splitWire(t, wire)

	if got, want := hdr.Get("From"), "alice@example.test"; got != want {
		t.Errorf("From = %q, want %q", got, want)
	}
	if got, want := hdr.Get("To"), "bob@example.test, carol@example.test"; got != want {
		t.Errorf("To = %q, want %q", got, want)
	}
	if got, want := hdr.Get("Cc"), "dave@example.test"; got != want {
		t.Errorf("Cc = %q, want %q", got, want)
	}
	if got, want := hdr.Get("Reply-To"), "support@example.test"; got != want {
		t.Errorf("Reply-To = %q, want %q", got, want)
	}
	if got, want := hdr.Get("Subject"), "Hello"; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	if got, want := hdr.Get("Date"), "Fri, 02 Jan 2026 03:04:05 +0000"; got != want {
		t.Errorf("Date = %q, want %q", got, want)
	}
	if got, want := hdr.Get("MIME-Version"), "1.0"; got != want {
		t.Errorf("MIME-Version = %q, want %q", got, want)
	}
	if got, want := hdr.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := body, "Hello Bob\r\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got := hdr.Get("Message-ID"); !regexp.MustCompile(`^<[0-9a-f]{32}@example\.test>$`).MatchString(got) {
		t.Errorf("Message-ID = %q, want <32 hex chars@example.test>", got)
	}
}

func TestBuildMessageMultipartAlternative(t *testing.T) {
	msg := validTestMessage()
	msg.HTML = "<p>Hello Bob</p>"

	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	hdr, body := splitWire(t, wire)

	mediaType, params, err := mime.ParseMediaType(hdr.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type %q: %v", hdr.Get("Content-Type"), err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type media type = %q, want multipart/alternative", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("multipart boundary parameter missing")
	}
	if !strings.HasPrefix(body, "--"+boundary+"\r\n") {
		t.Errorf("body does not start with opening boundary:\n%s", body)
	}

	mr := multipart.NewReader(strings.NewReader(body), boundary)

	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("first NextPart(): %v", err)
	}
	if got := part.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("part 1 Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	gotText, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read part 1: %v", err)
	}
	if string(gotText) != msg.Text {
		t.Errorf("part 1 body = %q, want %q", gotText, msg.Text)
	}

	part, err = mr.NextPart()
	if err != nil {
		t.Fatalf("second NextPart(): %v", err)
	}
	if got := part.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("part 2 Content-Type = %q, want text/html; charset=utf-8", got)
	}
	gotHTML, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read part 2: %v", err)
	}
	if string(gotHTML) != msg.HTML {
		t.Errorf("part 2 body = %q, want %q", gotHTML, msg.HTML)
	}

	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("NextPart() after both parts = %v, want io.EOF", err)
	}
}

func TestBuildMessageSinglePartHTML(t *testing.T) {
	msg := Message{
		From:    "alice@example.test",
		To:      []string{"bob@example.test"},
		Subject: "Hello",
		HTML:    "<p>Hello Bob</p>",
	}
	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	hdr, body := splitWire(t, wire)
	if got, want := hdr.Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := body, "<p>Hello Bob</p>\r\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestBuildMessageBccOmittedFromWire(t *testing.T) {
	msg := validTestMessage()
	msg.Bcc = []string{"hidden@example.test", "secret@example.test"}

	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	hdr, _ := splitWire(t, wire)

	if got := hdr.Get("Bcc"); got != "" {
		t.Errorf("Bcc header leaked into wire: %q", got)
	}
	for _, bcc := range msg.Bcc {
		if strings.Contains(string(wire), bcc) {
			t.Errorf("bcc address %q leaked into wire:\n%s", bcc, wire)
		}
	}
}

func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	injected := validTestMessage()
	injected.Subject = "hi\r\nBcc: attacker@example.test"
	if _, err := buildMessage(injected); err == nil || !strings.Contains(err.Error(), "injection") {
		t.Errorf("buildMessage() error = %v, want header injection error", err)
	}
}

func TestBuildMessageRequiresBody(t *testing.T) {
	empty := validTestMessage()
	empty.Text = ""
	if _, err := buildMessage(empty); err == nil || !strings.Contains(err.Error(), "text or html") {
		t.Errorf("buildMessage() error = %v, want body required error", err)
	}
}

func TestBuildMessageEncodesNonASCIISubject(t *testing.T) {
	msg := validTestMessage()
	msg.Subject = "Naïve résumé"
	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	hdr, _ := splitWire(t, wire)
	got := hdr.Get("Subject")
	if !strings.HasPrefix(got, "=?UTF-8?q?") || !strings.HasSuffix(got, "?=") {
		t.Errorf("Subject = %q, want RFC 2047 Q-encoded value", got)
	}
}
