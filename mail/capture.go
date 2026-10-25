package mail

import (
	"context"
	"sync"
)

// CaptureSender is a Sender that records every message it receives in
// memory. It is intended for tests and development previews where the
// exact messages handed to a Sender need to be inspected.
//
// All methods are safe for concurrent use.
type CaptureSender struct {
	mu       sync.Mutex
	messages []Message
}

var _ Sender = (*CaptureSender)(nil)

// NewCaptureSender returns an empty CaptureSender.
func NewCaptureSender() *CaptureSender {
	return &CaptureSender{}
}

// Send implements Sender. It stores a deep copy of msg (the To, Cc and
// Bcc slices are copied; the remaining fields are immutable strings)
// and always returns nil. Messages are not validated: a capture sink
// should record exactly what the application produced, including
// malformed input. The context is accepted to satisfy Sender but is
// deliberately not consulted, so capturing keeps working even when the
// surrounding request context is already done.
func (c *CaptureSender) Send(_ context.Context, msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, cloneMessage(msg))
	return nil
}

// Messages returns a deep copy of all captured messages in the order
// they were sent. Mutating the returned value never affects the
// sender's internal state.
func (c *CaptureSender) Messages() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Message, len(c.messages))
	for i, m := range c.messages {
		out[i] = cloneMessage(m)
	}
	return out
}

// Last returns a copy of the most recently captured message and
// whether any message has been captured at all.
func (c *CaptureSender) Last() (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		return Message{}, false
	}
	return cloneMessage(c.messages[len(c.messages)-1]), true
}

// Reset discards all captured messages.
func (c *CaptureSender) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = nil
}

// cloneMessage returns a deep copy of m: the header string fields are
// immutable, but the recipient slices must be copied so that later
// mutation of the source (or of the clone) cannot bleed through.
func cloneMessage(m Message) Message {
	return Message{
		From:    m.From,
		To:      append([]string(nil), m.To...),
		Cc:      append([]string(nil), m.Cc...),
		Bcc:     append([]string(nil), m.Bcc...),
		ReplyTo: m.ReplyTo,
		Subject: m.Subject,
		Text:    m.Text,
		HTML:    m.HTML,
	}
}
