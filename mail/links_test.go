package mail

import (
	"reflect"
	"testing"
)

func TestTextLinks(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: nil},
		{name: "no links", text: "Hello, world.", want: nil},
		{name: "single link", text: "https://example.test/verify?token=abc", want: []string{"https://example.test/verify?token=abc"}},
		{name: "http link", text: "go http://localhost:8080/x now", want: []string{"http://localhost:8080/x"}},
		{name: "upper-case scheme", text: "HTTPS://Example.test/A", want: []string{"HTTPS://Example.test/A"}},
		{name: "trailing period", text: "Visit https://example.test/a.", want: []string{"https://example.test/a"}},
		{name: "trailing punctuation run", text: "Really? https://example.test/a?!", want: []string{"https://example.test/a"}},
		{name: "comma separated", text: "https://a.test,https://b.test", want: []string{"https://a.test,https://b.test"}},
		{name: "comma then space", text: "https://a.test/1, https://b.test/2", want: []string{"https://a.test/1", "https://b.test/2"}},
		{name: "in parentheses", text: "(see https://example.test/a)", want: []string{"https://example.test/a"}},
		{name: "balanced parentheses kept", text: "https://en.wiki.test/Go_(language)", want: []string{"https://en.wiki.test/Go_(language)"}},
		{name: "in square brackets", text: "[https://example.test/a]", want: []string{"https://example.test/a"}},
		{name: "in angle brackets", text: "<https://example.test/a>", want: []string{"https://example.test/a"}},
		{name: "in double quotes", text: `"https://example.test/a"`, want: []string{"https://example.test/a"}},
		{name: "in single quotes", text: `'https://example.test/a'`, want: []string{"https://example.test/a"}},
		{name: "newline terminated", text: "https://example.test/a\nnext line", want: []string{"https://example.test/a"}},
		{name: "tab terminated", text: "https://example.test/a\tx", want: []string{"https://example.test/a"}},
		{name: "fragment and query", text: "https://example.test/a?b=1&c=2#frag", want: []string{"https://example.test/a?b=1&c=2#frag"}},
		{name: "duplicates removed", text: "https://a.test https://a.test https://b.test", want: []string{"https://a.test", "https://b.test"}},
		{name: "scheme without host", text: "https:// and http://", want: nil},
		{name: "scheme only path", text: "https:///path", want: nil},
		{name: "other schemes ignored", text: "ftp://x.test mailto:a@b.test javascript:alert(1)", want: nil},
		{name: "scheme inside a word", text: "xhttps://evil.test git+https://repo.test", want: nil},
		{name: "unicode around", text: "Link → https://example.test/é ← 😀", want: []string{"https://example.test/é"}},
		{name: "non-breaking space terminates", text: "https://example.test/a\u00a0b", want: []string{"https://example.test/a"}},
		{name: "ends with scheme", text: "text https://", want: nil},
		{name: "unparsable host", text: "http://[::1/x and https://ok.test", want: []string{"https://ok.test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textLinks(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("textLinks(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestHTMLHrefLinks(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{name: "empty", doc: "", want: nil},
		{name: "double quoted", doc: `<a href="https://a.test/x">x</a>`, want: []string{"https://a.test/x"}},
		{name: "single quoted", doc: `<a href='https://a.test/x'>x</a>`, want: []string{"https://a.test/x"}},
		{name: "unquoted", doc: `<a href=https://a.test/x>x</a>`, want: []string{"https://a.test/x"}},
		{name: "unquoted then attribute", doc: `<a href=https://a.test/x class=b>x</a>`, want: []string{"https://a.test/x"}},
		{name: "spaces around equals", doc: `<a href = "https://a.test/x">x</a>`, want: []string{"https://a.test/x"}},
		{name: "upper-case attribute", doc: `<A HREF="https://a.test/x">x</A>`, want: []string{"https://a.test/x"}},
		{name: "entities unescaped", doc: `<a href="https://a.test/v?e=a%40b&amp;t=1">x</a>`, want: []string{"https://a.test/v?e=a%40b&t=1"}},
		{name: "whitespace inside value trimmed", doc: `<a href=" https://a.test/x ">x</a>`, want: []string{"https://a.test/x"}},
		{name: "mailto skipped", doc: `<a href="mailto:a@b.test">x</a>`, want: nil},
		{name: "javascript skipped", doc: `<a href="javascript:alert(1)">x</a>`, want: nil},
		{name: "relative skipped", doc: `<a href="/reset">x</a>`, want: nil},
		{name: "data-href ignored", doc: `<a data-href="https://a.test/x">x</a>`, want: nil},
		{name: "href text outside attribute ignored", doc: `<p>href is a word, href=nothing</p>`, want: nil},
		{name: "duplicates removed", doc: `<a href="https://a.test/x">1</a><a href="https://a.test/x">2</a><a href="https://b.test">3</a>`, want: []string{"https://a.test/x", "https://b.test"}},
		{name: "unterminated quote", doc: `<a href="https://a.test/x`, want: nil},
		{name: "href at end", doc: `<a href=`, want: nil},
		{name: "href without equals", doc: `<a href>x</a>`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := htmlHrefLinks(tt.doc); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("htmlHrefLinks(%q) = %q, want %q", tt.doc, got, tt.want)
			}
		})
	}
}

func TestMessageLinks(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want []string
	}{
		{name: "text wins", msg: Message{Text: "https://t.test", HTML: `<a href="https://h.test">x</a>`}, want: []string{"https://t.test"}},
		{name: "html fallback without text", msg: Message{HTML: `<a href="https://h.test">x</a>`}, want: []string{"https://h.test"}},
		{name: "html fallback when text has no links", msg: Message{Text: "none", HTML: `<a href="https://h.test">x</a>`}, want: []string{"https://h.test"}},
		{name: "nothing", msg: Message{Text: "none", HTML: "<p>none</p>"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageLinks(tt.msg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("messageLinks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrimLinkSuffix(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://a.test/x", "https://a.test/x"},
		{"https://a.test/x.", "https://a.test/x"},
		{"https://a.test/x).", "https://a.test/x"},
		{"https://a.test/(x)", "https://a.test/(x)"},
		{"https://a.test/[x]]", "https://a.test/[x]"},
		{"https://a.test/{x}}", "https://a.test/{x}"},
		{"https://a.test/x*", "https://a.test/x"},
		{".,;:!?", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := trimLinkSuffix(tt.in); got != tt.want {
			t.Errorf("trimLinkSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
