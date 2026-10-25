package mail

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"strings"
	"time"
)

// nowFunc is the clock used when stamping the Date header of outgoing
// messages. It is an unexported package-level variable (a test seam)
// so tests can freeze the clock and assert byte-exact Date output.
var nowFunc = time.Now

// hostnameFunc resolves the host part used when generating Message-ID
// headers. It is an unexported seam for deterministic tests; the
// default resolves the machine hostname and falls back to
// "localhost" when it cannot be determined.
var hostnameFunc = defaultHostname

// defaultHostname returns the local hostname, or "localhost" when the
// hostname cannot be determined.
func defaultHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}

// randomHex returns n cryptographically random bytes as lowercase hex.
// It is used for multipart boundaries and Message-ID tokens; the
// randomness makes boundary collisions with body content and
// Message-ID collisions vanishingly unlikely.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mail: read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// foldLineLength is the line length, excluding CRLF, that header
// folding aims for (the RFC 5322 section 2.1.1 recommendation).
const foldLineLength = 78

// buildMessage renders msg as RFC 5322 wire bytes ready for the DATA
// phase of an SMTP dialog. It assumes msg.Validate has succeeded and
// repeats the header-injection guard defensively for direct callers.
//
// Generated headers, in order: From, To (comma-joined), Cc and
// Reply-To (only when set), Subject, Date, Message-ID, MIME-Version
// and Content-Type (plus Content-Transfer-Encoding for single-part
// messages).
//
// Address headers are re-rendered from their parsed form with
// net/mail.Address semantics: a bare address stays bare, a display
// name is quoted when it contains special characters and RFC 2047
// encoded when it contains non-ASCII characters. The Subject is
// Q-encoded per RFC 2047 when it contains non-ASCII or otherwise
// unencodable characters. Long address and Subject headers are folded
// at whitespace so lines stay near 78 characters.
//
// Bcc recipients deliberately appear nowhere in the output. Blind
// carbon copy semantics are provided purely through the SMTP envelope
// (RCPT TO): every recipient receives byte-identical headers with no
// trace of the Bcc list, while Bcc recipients still get delivered.
//
// When both Text and HTML are set the body is a multipart/alternative
// container with a cryptographically random boundary containing a
// text/plain and a text/html part (both charset=utf-8). When only one
// body is set a single part with the matching Content-Type is emitted.
// Every body is quoted-printable encoded (Content-Transfer-Encoding:
// quoted-printable), so the output is 7-bit clean with lines of at most
// 76 characters regardless of line length or non-ASCII content; line
// breaks in the body are emitted as CRLF.
func buildMessage(msg Message) ([]byte, error) {
	// Defensive: even if a caller skipped Validate, never emit
	// CR/LF-bearing header values.
	if err := checkHeaderField("subject", msg.Subject); err != nil {
		return nil, err
	}
	from, err := formatAddress("from", msg.From)
	if err != nil {
		return nil, err
	}
	to, err := formatAddressList("to", msg.To)
	if err != nil {
		return nil, err
	}
	cc, err := formatAddressList("cc", msg.Cc)
	if err != nil {
		return nil, err
	}
	var replyTo string
	if msg.ReplyTo != "" {
		if replyTo, err = formatAddress("reply-to", msg.ReplyTo); err != nil {
			return nil, err
		}
	}

	var b strings.Builder
	writeFoldedHeader(&b, "From", from)
	writeFoldedHeader(&b, "To", to)
	if cc != "" {
		writeFoldedHeader(&b, "Cc", cc)
	}
	if replyTo != "" {
		writeFoldedHeader(&b, "Reply-To", replyTo)
	}
	writeFoldedHeader(&b, "Subject", mime.QEncoding.Encode("UTF-8", msg.Subject))
	writeHeader(&b, "Date", nowFunc().Format(time.RFC1123Z))
	id, err := newMessageID()
	if err != nil {
		return nil, err
	}
	writeHeader(&b, "Message-ID", id)
	writeHeader(&b, "MIME-Version", "1.0")

	switch {
	case msg.Text != "" && msg.HTML != "":
		boundary, err := randomHex(16)
		if err != nil {
			return nil, err
		}
		text, err := encodeQuotedPrintable(msg.Text)
		if err != nil {
			return nil, err
		}
		html, err := encodeQuotedPrintable(msg.HTML)
		if err != nil {
			return nil, err
		}
		writeHeader(&b, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", boundary))
		b.WriteString("\r\n")
		writePart(&b, boundary, "text/plain; charset=utf-8", text)
		writePart(&b, boundary, "text/html; charset=utf-8", html)
		b.WriteString("--" + boundary + "--\r\n")
	case msg.Text != "":
		if err := writeSinglePart(&b, "text/plain; charset=utf-8", msg.Text); err != nil {
			return nil, err
		}
	case msg.HTML != "":
		if err := writeSinglePart(&b, "text/html; charset=utf-8", msg.HTML); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("mail: body: at least one of text or html is required")
	}
	return []byte(b.String()), nil
}

// formatAddress renders one address for a header. The input is parsed
// with net/mail and re-rendered: a bare address stays bare
// ("ada@example.com"), a display name is emitted as
// net/mail.Address.String does (quoted, or RFC 2047 encoded when it
// contains non-ASCII characters). The CR/LF guard runs on both the
// input and the output: net/mail decodes encoded words in the input,
// so the decoded name is re-encoded on output and verified once more.
func formatAddress(field, raw string) (string, error) {
	if err := checkHeaderField(field, raw); err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("mail: %s: an address is required", field)
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", invalidAddress(field)
	}
	out := addr.String()
	if addr.Name == "" {
		out = strings.TrimSuffix(strings.TrimPrefix(out, "<"), ">")
	}
	if err := checkHeaderField(field, out); err != nil {
		return "", err
	}
	return out, nil
}

// formatAddressList renders every address with formatAddress and joins
// them with ", ". An empty list renders as "". Errors name the entry by
// field and index ("to[1]").
func formatAddressList(field string, list []string) (string, error) {
	out := make([]string, 0, len(list))
	for i, raw := range list {
		formatted, err := formatAddress(indexedField(field, i), raw)
		if err != nil {
			return "", err
		}
		out = append(out, formatted)
	}
	return strings.Join(out, ", "), nil
}

// envelopeAddress returns the bare address (no display name) used in
// the SMTP envelope commands MAIL FROM and RCPT TO. Like every error of
// this package, a parse error names the field but not the value.
func envelopeAddress(field, raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", invalidAddress(field)
	}
	return addr.Address, nil
}

// encodeQuotedPrintable encodes a body with quoted-printable. Line
// breaks (LF or CRLF) become CRLF, lines are soft-wrapped at 76
// characters, and '=', non-ASCII bytes and trailing whitespace are
// escaped.
func encodeQuotedPrintable(body string) (string, error) {
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	if _, err := io.WriteString(w, body); err != nil {
		return "", fmt.Errorf("mail: encode body: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("mail: encode body: %w", err)
	}
	return b.String(), nil
}

// newMessageID generates an RFC 5322 Message-ID of the form
// "<random-token@hostname>".
func newMessageID() (string, error) {
	token, err := randomHex(16)
	if err != nil {
		return "", err
	}
	return "<" + token + "@" + hostnameFunc() + ">", nil
}

// writeHeader appends a single "Key: Value\r\n" header line.
func writeHeader(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

// writeFoldedHeader appends a header line, folding it (inserting CRLF
// before a space) whenever the line would exceed foldLineLength.
// Folding only happens at single spaces outside quoted strings, never
// produces whitespace-only lines, and is lossless: unfolding (removing
// the CRLFs, RFC 5322 section 2.2.3) restores the original value. A
// single word longer than the limit is emitted unbroken.
func writeFoldedHeader(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(":")
	lineLen := len(key) + 1
	for i, word := range splitFoldable(value) {
		if i > 0 && word != "" && lineLen+1+len(word) > foldLineLength {
			b.WriteString("\r\n")
			lineLen = 0
		}
		b.WriteString(" ")
		b.WriteString(word)
		lineLen += 1 + len(word)
	}
	b.WriteString("\r\n")
}

// splitFoldable splits a header value at every space that lies outside
// a quoted string. Joining the result with single spaces restores the
// input exactly.
func splitFoldable(value string) []string {
	var (
		words   []string
		start   int
		quoted  bool
		escaped bool
	)
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ' ' && !quoted:
			words = append(words, value[start:i])
			start = i + 1
		}
	}
	return append(words, value[start:])
}

// writePart appends one quoted-printable multipart body part delimited
// by boundary. The CRLF after the encoded body belongs to the next
// delimiter, so the decoded part is exactly the encoded body.
func writePart(b *strings.Builder, boundary, contentType, encodedBody string) {
	b.WriteString("--" + boundary + "\r\n")
	writeHeader(b, "Content-Type", contentType)
	writeHeader(b, "Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	b.WriteString(encodedBody)
	b.WriteString("\r\n")
}

// writeSinglePart appends the Content-Type and Content-Transfer-Encoding
// headers of a single-part message followed by its quoted-printable
// body, terminated by CRLF.
func writeSinglePart(b *strings.Builder, contentType, body string) error {
	encoded, err := encodeQuotedPrintable(body)
	if err != nil {
		return err
	}
	writeHeader(b, "Content-Type", contentType)
	writeHeader(b, "Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	b.WriteString(encoded)
	if !strings.HasSuffix(encoded, "\r\n") {
		b.WriteString("\r\n")
	}
	return nil
}
