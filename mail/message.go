package mail

import (
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
)

// Validate reports whether the message satisfies the minimum rules for
// sending:
//
//   - From is optional: when set it must be a parseable address
//     (net/mail). An empty From is valid because senders supply their
//     own default, for example SMTPSender uses SMTPConfig.From.
//   - To is required (non-empty) and every entry must be parseable
//   - Cc and Bcc entries, when present, must be parseable
//   - ReplyTo is optional; when set it must be a parseable address
//   - addresses may carry a display name ("Ada Lovelace
//     <ada@example.com>"); non-ASCII names are allowed and are
//     RFC 2047 encoded when the message is rendered
//   - Subject must be non-empty (not only whitespace)
//   - at least one of Text or HTML must be non-empty
//   - no header-bound field (From, To, Cc, Bcc, Reply-To, Subject)
//     may contain CR or LF characters, which would allow header
//     injection into the generated wire format
//
// Errors are plain lowercase errors of the form
// "mail: <field>: <problem>" describing the first violation found.
// Address list entries are identified by field and zero-based index
// ("mail: to[1]: invalid address"). Error messages never contain the
// offending value, so they are safe to log without leaking email
// addresses. There are no sentinel values; callers should log the
// message as-is.
func (m Message) Validate() error {
	// Header-injection guard runs first so smuggling attempts are
	// never masked by other validation errors. Bcc never reaches a
	// header, but it is checked for symmetry with the other address
	// lists.
	if err := checkHeaderField("from", m.From); err != nil {
		return err
	}
	if err := checkHeaderField("reply-to", m.ReplyTo); err != nil {
		return err
	}
	if err := checkHeaderField("subject", m.Subject); err != nil {
		return err
	}
	for _, list := range m.addressLists() {
		for i, addr := range list.addrs {
			if err := checkHeaderField(indexedField(list.field, i), addr); err != nil {
				return err
			}
		}
	}

	if from := strings.TrimSpace(m.From); from != "" {
		if _, err := mail.ParseAddress(from); err != nil {
			return invalidAddress("from")
		}
	}
	if replyTo := strings.TrimSpace(m.ReplyTo); replyTo != "" {
		if _, err := mail.ParseAddress(replyTo); err != nil {
			return invalidAddress("reply-to")
		}
	}
	if len(m.To) == 0 {
		return errors.New("mail: to: at least one recipient is required")
	}
	for _, list := range m.addressLists() {
		for i, addr := range list.addrs {
			if _, err := mail.ParseAddress(addr); err != nil {
				return invalidAddress(indexedField(list.field, i))
			}
		}
	}
	if strings.TrimSpace(m.Subject) == "" {
		return errors.New("mail: subject: is required")
	}
	if m.Text == "" && m.HTML == "" {
		return errors.New("mail: body: at least one of text or html is required")
	}
	return nil
}

// addressList is one named recipient list of a Message.
type addressList struct {
	field string
	addrs []string
}

// addressLists returns the recipient lists in envelope order: To, Cc,
// then Bcc.
func (m Message) addressLists() []addressList {
	return []addressList{{"to", m.To}, {"cc", m.Cc}, {"bcc", m.Bcc}}
}

// indexedField names entry i of an address list field, e.g. "to[1]".
func indexedField(field string, i int) string {
	return field + "[" + strconv.Itoa(i) + "]"
}

// invalidAddress reports an unparseable address in field. The value and
// the parser's diagnosis are deliberately left out: the parser quotes
// parts of its input, and error messages must never carry addresses.
func invalidAddress(field string) error {
	return fmt.Errorf("mail: %s: invalid address", field)
}

// checkHeaderField rejects values containing CR or LF characters. Such
// values would let a caller splice additional headers into the wire
// format ("header injection"), for example a Subject of
// "hi\r\nBcc: attacker@example.com". The value itself is never part of
// the error message.
func checkHeaderField(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("mail: %s: contains CR or LF (possible header injection)", field)
	}
	return nil
}
