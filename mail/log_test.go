package mail

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// memRecord is one structured log record captured by memHandler.
type memRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

// memHandler is a minimal in-memory slog.Handler used to assert the
// records LogSender emits. It is safe for concurrent use.
type memHandler struct {
	mu      sync.Mutex
	records []memRecord
}

func (h *memHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *memHandler) Handle(_ context.Context, r slog.Record) error {
	rec := memRecord{
		level:   r.Level,
		message: r.Message,
		attrs:   make(map[string]any, r.NumAttrs()),
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *memHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *memHandler) WithName(string) slog.Handler       { return h }
func (h *memHandler) WithGroup(string) slog.Handler      { return h }

func (h *memHandler) all() []memRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]memRecord, len(h.records))
	copy(out, h.records)
	return out
}

func TestLogSenderLogsStructuredRecords(t *testing.T) {
	h := &memHandler{}
	sender := NewLogSender(slog.New(h))

	msg := Message{
		From:    "alice@example.test",
		To:      []string{"bob@example.test", "carol@example.test"},
		Subject: "Hello",
		Text:    "body text",
		HTML:    "<p>body html</p>",
	}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	recs := h.all()
	if len(recs) != 2 {
		t.Fatalf("captured %d records, want 2 (Info + Debug)", len(recs))
	}
	info, debug := recs[0], recs[1]

	if info.level != slog.LevelInfo {
		t.Errorf("first record level = %v, want Info", info.level)
	}
	if info.message != "mail: outgoing message" {
		t.Errorf("Info message = %q, want %q", info.message, "mail: outgoing message")
	}
	if got := info.attrs["from"]; got != msg.From {
		t.Errorf("Info from = %v, want %q", got, msg.From)
	}
	if got := info.attrs["to"]; got != "bob@example.test, carol@example.test" {
		t.Errorf("Info to = %v, want comma-joined recipient list", got)
	}
	if got := info.attrs["subject"]; got != msg.Subject {
		t.Errorf("Info subject = %v, want %q", got, msg.Subject)
	}
	if _, ok := info.attrs["links"]; ok {
		t.Errorf("Info links = %v, want the attribute omitted for a message without links", info.attrs["links"])
	}
	if len(info.attrs) != 3 {
		t.Errorf("Info attrs = %v, want exactly from, to and subject", info.attrs)
	}

	if debug.level != slog.LevelDebug {
		t.Errorf("second record level = %v, want Debug", debug.level)
	}
	if debug.message != "mail: message body" {
		t.Errorf("Debug message = %q, want %q", debug.message, "mail: message body")
	}
	if got := debug.attrs["text"]; got != msg.Text {
		t.Errorf("Debug text = %v, want %q", got, msg.Text)
	}
	if got := debug.attrs["html"]; got != msg.HTML {
		t.Errorf("Debug html = %v, want %q", got, msg.HTML)
	}
	if got := debug.attrs["from"]; got != msg.From {
		t.Errorf("Debug from = %v, want %q", got, msg.From)
	}
	if got := debug.attrs["subject"]; got != msg.Subject {
		t.Errorf("Debug subject = %v, want %q", got, msg.Subject)
	}
}

func TestLogSenderRejectsInvalidMessage(t *testing.T) {
	h := &memHandler{}
	sender := NewLogSender(slog.New(h))

	bad := validTestMessage()
	bad.Subject = ""
	if err := sender.Send(context.Background(), bad); err == nil {
		t.Fatal("Send() error = nil, want validation error")
	}
	if got := len(h.all()); got != 0 {
		t.Errorf("captured %d records after invalid message, want 0", got)
	}
}

func TestLogSenderNilLoggerUsesDefault(t *testing.T) {
	prev := slog.Default()
	h := &memHandler{}
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(prev)

	sender := NewLogSender(nil)
	if err := sender.Send(context.Background(), validTestMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := len(h.all()); got != 2 {
		t.Errorf("captured %d records via slog.Default(), want 2", got)
	}
}

func TestLogSenderConcurrentSend(t *testing.T) {
	h := &memHandler{}
	sender := NewLogSender(slog.New(h))

	const goroutines = 8
	const perGoroutine = 25
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				msg := validTestMessage()
				msg.Subject = fmt.Sprintf("message %d-%d", n, j)
				if err := sender.Send(context.Background(), msg); err != nil {
					t.Errorf("Send() error = %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	if got, want := len(h.all()), goroutines*perGoroutine*2; got != want {
		t.Errorf("captured %d records, want %d", got, want)
	}
}

func TestLogSenderInfoSummary(t *testing.T) {
	const verifyLink = "http://localhost:8080/auth/verify-email?email=ada%40example.test&token=abc123"
	tests := []struct {
		name      string
		msg       Message
		wantFrom  string // "" = the attribute must be absent
		wantLinks string // "" = the attribute must be absent
	}{
		{
			name:      "verification email with text and html",
			msg:       Message{From: "app@example.test", To: []string{"ada@example.test"}, Subject: "Verify", Text: "Open " + verifyLink + " to verify.", HTML: `<a href="` + strings.ReplaceAll(verifyLink, "&", "&amp;") + `">Verify</a>`},
			wantFrom:  "app@example.test",
			wantLinks: verifyLink,
		},
		{
			name:      "empty from is omitted",
			msg:       Message{To: []string{"ada@example.test"}, Subject: "Verify", Text: verifyLink},
			wantLinks: verifyLink,
		},
		{
			name:      "html only falls back to hrefs",
			msg:       Message{To: []string{"ada@example.test"}, Subject: "Reset", HTML: `<p><a href="https://app.test/reset?t=1&amp;e=a">Reset</a> <a href="mailto:help@example.test">help</a></p>`},
			wantLinks: "https://app.test/reset?t=1&e=a",
		},
		{
			name:      "text without links falls back to hrefs",
			msg:       Message{To: []string{"ada@example.test"}, Subject: "Reset", Text: "Use the button in the HTML version.", HTML: `<a href='https://app.test/r'>Reset</a>`},
			wantLinks: "https://app.test/r",
		},
		{
			name:      "text links win over html links",
			msg:       Message{To: []string{"ada@example.test"}, Subject: "Hi", Text: "See https://a.test/x", HTML: `<a href="https://b.test/y">y</a>`},
			wantLinks: "https://a.test/x",
		},
		{
			name:      "several links deduplicated in order",
			msg:       Message{To: []string{"ada@example.test"}, Subject: "Hi", Text: "https://a.test/1 and http://b.test/2, again https://a.test/1."},
			wantLinks: "https://a.test/1 http://b.test/2",
		},
		{
			name: "no links anywhere",
			msg:  Message{To: []string{"ada@example.test"}, Subject: "Hi", Text: "plain", HTML: "<p>plain</p>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &memHandler{}
			if err := NewLogSender(slog.New(h)).Send(context.Background(), tt.msg); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			info := h.all()[0]
			if info.level != slog.LevelInfo {
				t.Fatalf("first record level = %v, want Info", info.level)
			}
			checkAttr := func(key, want string) {
				t.Helper()
				got, ok := info.attrs[key]
				if want == "" {
					if ok {
						t.Errorf("Info %s = %v, want the attribute omitted", key, got)
					}
					return
				}
				if got != want {
					t.Errorf("Info %s = %v, want %q", key, got, want)
				}
			}
			checkAttr("from", tt.wantFrom)
			checkAttr("links", tt.wantLinks)
			checkAttr("to", strings.Join(tt.msg.To, ", "))
			checkAttr("subject", tt.msg.Subject)
			for key := range info.attrs {
				if key == "text" || key == "html" {
					t.Errorf("Info record must not carry the body attribute %q", key)
				}
			}
		})
	}
}

// TestLogSenderTextOutput checks the rendered line developers actually
// see: one compact Info line with a clickable link and no from="" noise,
// and no body unless Debug is enabled.
func TestLogSenderTextOutput(t *testing.T) {
	msg := Message{
		To:      []string{"ada@example.test"},
		Subject: "Verify your email",
		Text:    "Open https://app.test/verify?token=secret-token to verify.",
		HTML:    "<p>body</p>",
	}
	tests := []struct {
		name      string
		level     slog.Level
		wantLines int
	}{
		{name: "info level", level: slog.LevelInfo, wantLines: 1},
		{name: "debug level", level: slog.LevelDebug, wantLines: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: tt.level}))
			if err := NewLogSender(logger).Send(context.Background(), msg); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			out := buf.String()
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) != tt.wantLines {
				t.Fatalf("logged %d lines, want %d:\n%s", len(lines), tt.wantLines, out)
			}
			if strings.Contains(out, "from=") {
				t.Errorf("output prints an empty from attribute:\n%s", out)
			}
			if !strings.Contains(lines[0], "https://app.test/verify?token=secret-token") {
				t.Errorf("Info line lacks the link:\n%s", lines[0])
			}
			if strings.Contains(lines[0], "<p>body</p>") || strings.Contains(lines[0], "text=") {
				t.Errorf("Info line must not contain the bodies:\n%s", lines[0])
			}
			if tt.wantLines == 2 && !strings.Contains(lines[1], "<p>body</p>") {
				t.Errorf("Debug line lacks the HTML body:\n%s", lines[1])
			}
		})
	}
}
