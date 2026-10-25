package mail

import (
	"errors"
	"fmt"
	"net/textproto"
	"strings"
)

// redactedPlaceholder replaces every email address removed from an error
// message.
const redactedPlaceholder = "[redacted]"

// smtpError builds the error returned when one SMTP dialog stage fails,
// e.g. "mail: smtp: MAIL FROM: 550 5.1.7 sender rejected". Servers often
// quote addresses in their replies ("550 5.1.1 <bob@example.com>: user
// unknown"), so the cause is passed through redactError first.
func smtpError(stage string, err error) error {
	return fmt.Errorf("mail: smtp: %s: %w", stage, redactError(err))
}

// redactError returns err with every email address removed from its
// message. A server reply (*textproto.Error) is replaced by a copy with
// the same Code and a redacted Msg, so callers can still inspect the
// reply code with errors.As. Any other error whose message contains an
// address is wrapped in a redactedError that keeps the original
// reachable through errors.Is and errors.As (for example to detect
// timeouts) while its Error text is redacted. Errors without addresses
// are returned unchanged.
func redactError(err error) error {
	if err == nil {
		return nil
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return &textproto.Error{Code: reply.Code, Msg: redactAddresses(reply.Msg)}
	}
	msg := err.Error()
	if redacted := redactAddresses(msg); redacted != msg {
		return &redactedError{msg: redacted, err: err}
	}
	return err
}

// redactedError is an error whose message had email addresses removed.
type redactedError struct {
	msg string
	err error
}

// Error returns the redacted message.
func (e *redactedError) Error() string { return e.msg }

// Unwrap returns the original error for errors.Is and errors.As.
func (e *redactedError) Unwrap() error { return e.err }

// redactAddresses replaces every email-address-like token in s with
// redactedPlaceholder. A token is a run of characters around an '@'
// bounded by whitespace or by characters that delimit addresses in
// SMTP replies and headers (angle brackets, quotes, parentheses, commas,
// semicolons, colons); it is replaced when it has characters on both
// sides of the '@'. A quoted local part ("john doe"@example.com) is
// treated as part of the token.
func redactAddresses(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	written := 0 // bytes of s already copied to b
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		start := addressStart(s, i, written)
		end := i + 1
		for end < len(s) && !isAddressDelimiter(s[end]) {
			end++
		}
		if start == i || end == i+1 {
			continue // nothing on one side of the '@': not an address
		}
		b.WriteString(s[written:start])
		b.WriteString(redactedPlaceholder)
		written = end
		i = end - 1
	}
	if written == 0 {
		return s
	}
	b.WriteString(s[written:])
	return b.String()
}

// addressStart returns the index where the address whose '@' sits at at
// begins, never looking before floor. A quoted local part directly
// before the '@' is included together with any unquoted text before it.
func addressStart(s string, at, floor int) int {
	start := at
	if start > floor && s[start-1] == '"' {
		if open := strings.LastIndexByte(s[floor:start-1], '"'); open >= 0 {
			start = floor + open
		}
	}
	for start > floor && !isAddressDelimiter(s[start-1]) {
		start--
	}
	return start
}

// isAddressDelimiter reports whether c ends an address token.
func isAddressDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '<', '>', '"', '\'', '(', ')', ',', ';', ':':
		return true
	}
	return false
}
