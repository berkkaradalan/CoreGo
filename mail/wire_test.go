package mail

import (
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
)

// crlf normalizes every line break in s to CRLF, which is how the
// quoted-printable encoder emits hard line breaks.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// decodeQP decodes a quoted-printable body, failing the test on error.
func decodeQP(t *testing.T, encoded string) string {
	t.Helper()
	out, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(encoded)))
	if err != nil {
		t.Fatalf("decode quoted-printable %q: %v", encoded, err)
	}
	return string(out)
}

// assertWireSafe checks that the rendered message is 7-bit clean, that
// no line exceeds the RFC 5322 hard limit of 998 bytes and that no line
// after the top-level header block exceeds maxBodyLine.
func assertWireSafe(t *testing.T, wire []byte, maxBodyLine int) {
	t.Helper()
	for i, b := range wire {
		if b >= 0x80 {
			t.Fatalf("non-ASCII byte 0x%x at offset %d in wire output", b, i)
		}
	}
	inBody := false
	for i, line := range strings.Split(string(wire), "\r\n") {
		limit := 998
		if inBody {
			limit = maxBodyLine
		}
		if line == "" {
			inBody = true
		}
		if len(line) > limit {
			t.Errorf("line %d is %d bytes long (limit %d): %q", i+1, len(line), limit, line)
		}
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("line %d contains a bare CR or LF: %q", i+1, line)
		}
	}
}

// bodyTestCases are bodies that are unsafe to send unencoded.
var bodyTestCases = []struct {
	name string
	body string
}{
	{"long line over 998 bytes", strings.Repeat("x", 2000)},
	{"long line with spaces", strings.Repeat("lorem ipsum ", 200)},
	{"non-ascii", "Naïve café résumé — € ½ ✓ 🎉"},
	{"equals signs", "a=b, 1+1=2, ==, =3D must stay literal"},
	{"verification link", "Verify: https://app.example.com/auth/verify-email?token=abc123&email=ada%40example.com"},
	{"lines starting with dots", ".\n..\n.leading dot\nend"},
	{"trailing whitespace", "trailing space \ntrailing tab\t\nend"},
	{"lf line endings", "line one\nline two\n\nline four"},
	{"crlf line endings", "line one\r\nline two\r\n"},
	{"html", `<p style="color:#333">Hello <a href="https://example.com/?a=1&amp;b=2">link</a></p>`},
}

func TestBuildMessageQuotedPrintableSinglePart(t *testing.T) {
	for _, kind := range []struct {
		name        string
		contentType string
		set         func(*Message, string)
	}{
		{"text", "text/plain; charset=utf-8", func(m *Message, b string) { m.Text = b }},
		{"html", "text/html; charset=utf-8", func(m *Message, b string) { m.HTML = b }},
	} {
		for _, tc := range bodyTestCases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				msg := validTestMessage()
				msg.Text = ""
				kind.set(&msg, tc.body)

				wire, err := buildMessage(msg)
				if err != nil {
					t.Fatalf("buildMessage() error = %v", err)
				}
				assertWireSafe(t, wire, 76)

				hdr, body := splitWire(t, wire)
				if got := hdr.Get("Content-Type"); got != kind.contentType {
					t.Errorf("Content-Type = %q, want %q", got, kind.contentType)
				}
				if got := hdr.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
					t.Errorf("Content-Transfer-Encoding = %q, want quoted-printable", got)
				}
				if !strings.HasSuffix(body, "\r\n") {
					t.Errorf("body does not end with CRLF: %q", body)
				}

				// The renderer terminates the body with CRLF when the
				// content does not already end with a line break.
				want := crlf(tc.body)
				if !strings.HasSuffix(want, "\r\n") {
					want += "\r\n"
				}
				if got := decodeQP(t, body); got != want {
					t.Errorf("decoded body = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestBuildMessageQuotedPrintableMultipart(t *testing.T) {
	for _, tc := range bodyTestCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validTestMessage()
			msg.Text = tc.body
			msg.HTML = "<p>" + tc.body + "</p>"

			wire, err := buildMessage(msg)
			if err != nil {
				t.Fatalf("buildMessage() error = %v", err)
			}
			assertWireSafe(t, wire, 76)

			hdr, body := splitWire(t, wire)
			_, params, err := mime.ParseMediaType(hdr.Get("Content-Type"))
			if err != nil {
				t.Fatalf("parse Content-Type: %v", err)
			}
			if got := hdr.Get("Content-Transfer-Encoding"); got != "" {
				t.Errorf("top-level Content-Transfer-Encoding = %q, want none on a multipart container", got)
			}

			mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
			for _, want := range []struct {
				contentType string
				body        string
			}{
				{"text/plain; charset=utf-8", msg.Text},
				{"text/html; charset=utf-8", msg.HTML},
			} {
				// NextRawPart keeps the Content-Transfer-Encoding
				// header (NextPart would hide it and decode).
				part, err := mr.NextRawPart()
				if err != nil {
					t.Fatalf("NextRawPart(): %v", err)
				}
				if got := part.Header.Get("Content-Type"); got != want.contentType {
					t.Errorf("part Content-Type = %q, want %q", got, want.contentType)
				}
				if got := part.Header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
					t.Errorf("part Content-Transfer-Encoding = %q, want quoted-printable", got)
				}
				raw, err := io.ReadAll(part)
				if err != nil {
					t.Fatalf("read part: %v", err)
				}
				if got := decodeQP(t, string(raw)); got != crlf(want.body) {
					t.Errorf("decoded part = %q, want %q", got, crlf(want.body))
				}
			}
			if _, err := mr.NextRawPart(); err != io.EOF {
				t.Errorf("NextRawPart() after both parts = %v, want io.EOF", err)
			}
		})
	}
}

func TestBuildMessageEscapesEqualsSign(t *testing.T) {
	msg := validTestMessage()
	msg.Text = "token=abc"
	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	_, body := splitWire(t, wire)
	if body != "token=3Dabc\r\n" {
		t.Errorf("body = %q, want %q", body, "token=3Dabc\r\n")
	}
}

func TestFormatAddress(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		want     string // exact expected rendering; empty = check round trip only
		wantName string
		wantAddr string
		wantErr  string
	}{
		{name: "bare address", in: "ada@example.test", want: "ada@example.test", wantAddr: "ada@example.test"},
		{name: "angle brackets without name", in: "<ada@example.test>", want: "ada@example.test", wantAddr: "ada@example.test"},
		{name: "surrounding whitespace", in: "  ada@example.test ", want: "ada@example.test", wantAddr: "ada@example.test"},
		{name: "ascii name", in: "Ada Lovelace <ada@example.test>", want: `"Ada Lovelace" <ada@example.test>`, wantName: "Ada Lovelace", wantAddr: "ada@example.test"},
		{name: "name with comma", in: `"Doe, John" <john@example.test>`, want: `"Doe, John" <john@example.test>`, wantName: "Doe, John", wantAddr: "john@example.test"},
		{name: "name with quotes", in: `"Ann \"The Boss\" Lee" <ann@example.test>`, want: `"Ann \"The Boss\" Lee" <ann@example.test>`, wantName: `Ann "The Boss" Lee`, wantAddr: "ann@example.test"},
		{name: "non-ascii name", in: "José Núñez <jose@example.test>", wantName: "José Núñez", wantAddr: "jose@example.test"},
		{name: "non-ascii name with comma", in: `"Núñez, José" <jose@example.test>`, wantName: "Núñez, José", wantAddr: "jose@example.test"},
		{name: "non-ascii name with quotes", in: `"Zoë \"Z\" Núñez" <zoe@example.test>`, wantName: `Zoë "Z" Núñez`, wantAddr: "zoe@example.test"},
		{name: "emoji name", in: "Party 🎉 <party@example.test>", wantName: "Party 🎉", wantAddr: "party@example.test"},
		{name: "encoded name in input", in: "=?utf-8?q?Jos=C3=A9?= <jose@example.test>", wantName: "José", wantAddr: "jose@example.test"},
		{name: "empty", in: "", wantErr: "an address is required"},
		{name: "whitespace only", in: "   ", wantErr: "an address is required"},
		{name: "invalid", in: "not an address", wantErr: "mail: to: invalid address"},
		{name: "address list is not one address", in: "a@example.test, b@example.test", wantErr: "mail: to: invalid address"},
		{name: "crlf injection", in: "ada@example.test\r\nBcc: evil@example.test", wantErr: "header injection"},
		{name: "lf injection in name", in: "Ada\nBcc: evil@example.test <ada@example.test>", wantErr: "header injection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatAddress("to", tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("formatAddress(%q) = %q, %v; want error containing %q", tt.in, got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("formatAddress(%q) error = %v", tt.in, err)
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("formatAddress(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for i := 0; i < len(got); i++ {
				if got[i] >= 0x80 {
					t.Fatalf("formatAddress(%q) = %q contains non-ASCII bytes", tt.in, got)
				}
			}
			parsed, err := mail.ParseAddress(got)
			if err != nil {
				t.Fatalf("rendered address %q does not parse: %v", got, err)
			}
			if parsed.Name != tt.wantName || parsed.Address != tt.wantAddr {
				t.Errorf("round trip of %q = (%q, %q), want (%q, %q)", got, parsed.Name, parsed.Address, tt.wantName, tt.wantAddr)
			}
		})
	}
}

func TestFormatAddressReencodesSmuggledLineBreaks(t *testing.T) {
	// net/mail decodes RFC 2047 words, so a display name can carry CR/LF
	// after decoding even though the raw input has none. The rendered
	// header must re-encode it instead of emitting raw line breaks.
	for _, in := range []string{
		"=?utf-8?q?Evil=0D=0AX-Injected=3A_yes?= <ada@example.test>",
		"=?utf-8?b?RXZpbA0KWC1JbmplY3RlZDogeWVz?= <ada@example.test>",
	} {
		got, err := formatAddress("to", in)
		if err != nil {
			t.Fatalf("formatAddress(%q) error = %v", in, err)
		}
		if strings.ContainsAny(got, "\r\n") || strings.Contains(got, "X-Injected") {
			t.Fatalf("formatAddress(%q) = %q exposes the smuggled header", in, got)
		}
		parsed, err := mail.ParseAddress(got)
		if err != nil {
			t.Fatalf("rendered address %q does not parse: %v", got, err)
		}
		if parsed.Name != "Evil\r\nX-Injected: yes" || parsed.Address != "ada@example.test" {
			t.Errorf("round trip = (%q, %q), want the original name and address", parsed.Name, parsed.Address)
		}

		msg := validTestMessage()
		msg.To = []string{in}
		wire, err := buildMessage(msg)
		if err != nil {
			t.Fatalf("buildMessage() error = %v", err)
		}
		hdr, _ := splitWire(t, wire)
		if v := hdr.Get("X-Injected"); v != "" {
			t.Errorf("smuggled header reached the wire: X-Injected: %q", v)
		}
	}
}

func TestBuildMessageAddressHeaders(t *testing.T) {
	msg := Message{
		From:    "José Núñez <jose@example.test>",
		To:      []string{"Zoë Núñez <zoe@example.test>", `"Doe, John" <john@example.test>`, "plain@example.test"},
		Cc:      []string{"Åsa <asa@example.test>"},
		ReplyTo: "Support Ø <support@example.test>",
		Subject: "Hello",
		Text:    "hi",
	}
	wire, err := buildMessage(msg)
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	assertWireSafe(t, wire, 76)
	hdr, _ := splitWire(t, wire)

	checkList := func(header string, want []mail.Address) {
		t.Helper()
		got, err := mail.ParseAddressList(hdr.Get(header))
		if err != nil {
			t.Fatalf("parse %s header %q: %v", header, hdr.Get(header), err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s has %d addresses, want %d: %q", header, len(got), len(want), hdr.Get(header))
		}
		for i := range want {
			if got[i].Name != want[i].Name || got[i].Address != want[i].Address {
				t.Errorf("%s[%d] = (%q, %q), want (%q, %q)", header, i, got[i].Name, got[i].Address, want[i].Name, want[i].Address)
			}
		}
	}
	checkList("From", []mail.Address{{Name: "José Núñez", Address: "jose@example.test"}})
	checkList("To", []mail.Address{
		{Name: "Zoë Núñez", Address: "zoe@example.test"},
		{Name: "Doe, John", Address: "john@example.test"},
		{Address: "plain@example.test"},
	})
	checkList("Cc", []mail.Address{{Name: "Åsa", Address: "asa@example.test"}})
	checkList("Reply-To", []mail.Address{{Name: "Support Ø", Address: "support@example.test"}})
}

func TestBuildMessageRejectsInjectionInEveryAddressHeader(t *testing.T) {
	const evil = "x@example.test\r\nBcc: attacker@example.test"
	tests := []struct {
		name string
		mut  func(*Message)
	}{
		{"from", func(m *Message) { m.From = evil }},
		{"to", func(m *Message) { m.To = append(m.To, evil) }},
		{"cc", func(m *Message) { m.Cc = []string{evil} }},
		{"reply-to", func(m *Message) { m.ReplyTo = evil }},
		{"display name", func(m *Message) { m.To = []string{"Eve\r\nBcc: attacker@example.test <eve@example.test>"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validTestMessage()
			tt.mut(&msg)
			if wire, err := buildMessage(msg); err == nil || !strings.Contains(err.Error(), "injection") {
				t.Errorf("buildMessage() = %q, %v; want header injection error", wire, err)
			}
		})
	}
}

func TestBuildMessageRejectsInvalidAddresses(t *testing.T) {
	tests := []struct {
		name, want string
		mut        func(*Message)
	}{
		{"empty from", "from: an address is required", func(m *Message) { m.From = "" }},
		{"invalid to", "mail: to[1]: invalid address", func(m *Message) { m.To = append(m.To, "nope") }},
		{"invalid cc", "mail: cc[0]: invalid address", func(m *Message) { m.Cc = []string{"nope"} }},
		{"invalid reply-to", "mail: reply-to: invalid address", func(m *Message) { m.ReplyTo = "nope" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validTestMessage()
			tt.mut(&msg)
			if _, err := buildMessage(msg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("buildMessage() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestBuildMessageFoldsLongHeaders(t *testing.T) {
	subject := strings.Repeat("Ùnïcødé subject words ", 20)
	subject = strings.TrimSpace(subject)
	asciiSubject := strings.TrimSpace(strings.Repeat("plain ascii subject ", 15))
	var to []string
	for i := 0; i < 20; i++ {
		to = append(to, "Recipient Ø <r"+strings.Repeat("x", i)+"@example.test>")
	}

	for _, tc := range []struct {
		name    string
		subject string
	}{
		{"non-ascii subject", subject},
		{"ascii subject", asciiSubject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := validTestMessage()
			msg.Subject = tc.subject
			msg.To = to

			wire, err := buildMessage(msg)
			if err != nil {
				t.Fatalf("buildMessage() error = %v", err)
			}
			assertWireSafe(t, wire, 76)
			continuations := 0
			for _, line := range strings.Split(string(wire), "\r\n") {
				if line != "" && strings.TrimSpace(line) == "" {
					t.Errorf("whitespace-only line in output: %q", line)
				}
				if strings.HasPrefix(line, " ") {
					continuations++
					if len(line) > foldLineLength {
						t.Errorf("folded line is %d bytes long (limit %d): %q", len(line), foldLineLength, line)
					}
				}
			}
			if continuations < 2 {
				t.Errorf("expected Subject and To to be folded, got %d continuation lines:\n%s", continuations, wire)
			}

			hdr, _ := splitWire(t, wire)
			decoded, err := new(mime.WordDecoder).DecodeHeader(hdr.Get("Subject"))
			if err != nil {
				t.Fatalf("decode Subject: %v", err)
			}
			if decoded != tc.subject {
				t.Errorf("Subject round trip = %q, want %q", decoded, tc.subject)
			}
			list, err := mail.ParseAddressList(hdr.Get("To"))
			if err != nil {
				t.Fatalf("parse folded To: %v", err)
			}
			if len(list) != len(to) {
				t.Fatalf("To has %d addresses, want %d", len(list), len(to))
			}
			for i, addr := range list {
				if addr.Name != "Recipient Ø" {
					t.Errorf("To[%d].Name = %q, want %q", i, addr.Name, "Recipient Ø")
				}
			}
		})
	}
}

func TestWriteFoldedHeader(t *testing.T) {
	long := strings.Repeat("y", 120)
	tests := []struct {
		name, key, value, want string
	}{
		{"short value is not folded", "Subject", "hello world", "Subject: hello world\r\n"},
		{"empty value", "Subject", "", "Subject: \r\n"},
		{"single long word is not broken", "Subject", long, "Subject: " + long + "\r\n"},
		{
			"folds before the word that overflows",
			"Subject", strings.Repeat("a", 60) + " " + strings.Repeat("b", 20),
			"Subject: " + strings.Repeat("a", 60) + "\r\n " + strings.Repeat("b", 20) + "\r\n",
		},
		{
			"never folds inside a quoted string",
			"To", `"` + strings.Repeat("n ", 40) + `" <a@example.test>`,
			"To: " + `"` + strings.Repeat("n ", 40) + `"` + "\r\n <a@example.test>\r\n",
		},
		{
			"never creates whitespace-only lines",
			"Subject", strings.Repeat("c", 70) + "    " + strings.Repeat("d", 20),
			"Subject: " + strings.Repeat("c", 70) + "   \r\n " + strings.Repeat("d", 20) + "\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeFoldedHeader(&b, tt.key, tt.value)
			got := b.String()
			if got != tt.want {
				t.Errorf("writeFoldedHeader() =\n%q\nwant\n%q", got, tt.want)
			}
			// Unfolding (removing CRLF) must restore the original line.
			if unfolded := strings.ReplaceAll(got, "\r\n ", " "); unfolded != tt.key+": "+tt.value+"\r\n" {
				t.Errorf("unfolded = %q, want %q", unfolded, tt.key+": "+tt.value+"\r\n")
			}
		})
	}
}

func TestSplitFoldable(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{"a b", []string{"a", "b"}},
		{"a  b", []string{"a", "", "b"}},
		{`"x y" <a@b>`, []string{`"x y"`, "<a@b>"}},
		{`"x \" y" z`, []string{`"x \" y"`, "z"}},
		{`"unterminated x y`, []string{`"unterminated x y`}},
	}
	for _, tt := range tests {
		got := splitFoldable(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
			t.Errorf("splitFoldable(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if strings.Join(got, " ") != tt.in {
			t.Errorf("splitFoldable(%q) does not join back to the input: %q", tt.in, got)
		}
	}
}
