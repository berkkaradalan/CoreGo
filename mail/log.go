package mail

import (
	"context"
	"log/slog"
	"strings"
)

// LogSender is a Sender that logs messages instead of delivering them.
// It is meant for local development and for test suites where a real
// SMTP server is not desirable.
//
// Every message produces one compact Info record, "mail: outgoing
// message", with the attributes "from" (omitted when the message has no
// From), "to" (comma-joined), "subject" and "links": the http and https
// URLs found in the Text body (or, when the Text body has none, the
// href targets of the HTML body), space-separated and deduplicated, and
// omitted when there are none. That is how developers click the
// verification or password reset link of a locally sent email without
// enabling Debug logging. The full Text and HTML bodies are logged in a
// second record at Debug level.
//
// DEVELOPMENT ONLY. Those links and bodies contain live single-use
// tokens (email verification, password reset), and the links are logged
// at Info level, which production logs usually keep. Never use LogSender
// as the mailer of a production deployment: anyone with access to the
// logs could take over accounts. Use SMTPSender (or your own Sender)
// there.
//
// LogSender is safe for concurrent use: slog.Logger is itself
// goroutine-safe and LogSender holds no mutable state.
type LogSender struct {
	logger *slog.Logger
}

var _ Sender = (*LogSender)(nil)

// NewLogSender returns a LogSender that logs to logger. A nil logger
// selects slog.Default(). See LogSender: it logs live tokens and is for
// development only.
func NewLogSender(logger *slog.Logger) *LogSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogSender{logger: logger}
}

// Send implements Sender. It validates the message, logs the Info
// summary described on LogSender (from, to, subject, links) and the
// full bodies at Debug level (keys "from", "to", "subject", "text" and
// "html"; "from" again only when set). No delivery is attempted; Send
// returns nil unless validation fails.
func (l *LogSender) Send(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	to := strings.Join(msg.To, ", ")

	summary := make([]slog.Attr, 0, 4)
	if msg.From != "" {
		summary = append(summary, slog.String("from", msg.From))
	}
	summary = append(summary,
		slog.String("to", to),
		slog.String("subject", msg.Subject),
	)
	if links := messageLinks(msg); len(links) > 0 {
		summary = append(summary, slog.String("links", strings.Join(links, " ")))
	}
	l.logger.LogAttrs(ctx, slog.LevelInfo, "mail: outgoing message", summary...)

	if !l.logger.Enabled(ctx, slog.LevelDebug) {
		return nil
	}
	body := make([]slog.Attr, 0, 5)
	if msg.From != "" {
		body = append(body, slog.String("from", msg.From))
	}
	body = append(body,
		slog.String("to", to),
		slog.String("subject", msg.Subject),
		slog.String("text", msg.Text),
		slog.String("html", msg.HTML),
	)
	l.logger.LogAttrs(ctx, slog.LevelDebug, "mail: message body", body...)
	return nil
}
