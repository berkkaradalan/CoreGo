// Package mail sends email through pluggable backends.
//
// The package defines a small Sender interface together with production
// and development implementations: SMTPSender delivers over SMTP,
// LogSender logs instead of delivering and CaptureSender records
// messages in memory for tests.
package mail

import "context"

// Message represents an outgoing email message.
type Message struct {
	From    string
	To      []string
	Cc      []string
	Bcc     []string
	ReplyTo string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers email messages. Implementations must be safe for
// concurrent use.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SenderFunc adapts a plain function to the Sender interface.
type SenderFunc func(ctx context.Context, msg Message) error

// Send implements Sender.
func (f SenderFunc) Send(ctx context.Context, msg Message) error {
	return f(ctx, msg)
}
