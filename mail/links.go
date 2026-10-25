package mail

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// messageLinks returns the http and https URLs a developer may want to
// open from a logged message: every URL in the Text body, or, when the
// Text body contains none (or is empty), every http(s) href attribute of
// the HTML body. Duplicates are dropped and the order of first
// appearance is kept.
func messageLinks(msg Message) []string {
	if links := textLinks(msg.Text); len(links) > 0 {
		return links
	}
	return htmlHrefLinks(msg.HTML)
}

// textLinks returns the http(s) URLs found in plain text, in order and
// without duplicates. A URL starts at "http://" or "https://" (any letter
// case) that does not continue a word, runs until whitespace, a control
// character or one of the characters that cannot appear unescaped in a
// URL and typically delimit one in prose (<, >, ", ', `), and loses
// trailing sentence punctuation and unbalanced closing brackets.
func textLinks(text string) []string {
	var links []string
	seen := make(map[string]bool)
	for i := 0; i < len(text); {
		n := schemeLen(text[i:])
		if n == 0 || (i > 0 && continuesWord(text[i-1])) {
			i++
			continue
		}
		end := i + n
		for end < len(text) {
			r, size := utf8.DecodeRuneInString(text[end:])
			if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("<>\"'`", r) {
				break
			}
			end += size
		}
		link := trimLinkSuffix(text[i:end])
		if isHTTPURL(link) && !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
		i = end
	}
	return links
}

// htmlHrefLinks returns the http(s) values of the href attributes in an
// HTML document, HTML-unescaped ("&amp;" becomes "&"), in order and
// without duplicates. Quoted and unquoted attribute values are
// understood; other schemes (mailto:, javascript:, relative paths) are
// skipped.
func htmlHrefLinks(doc string) []string {
	var links []string
	seen := make(map[string]bool)
	for i := 0; i+4 <= len(doc); i++ {
		if !strings.EqualFold(doc[i:i+4], "href") || i == 0 || !isAttrSeparator(doc[i-1]) {
			continue
		}
		j := skipHTMLSpace(doc, i+4)
		if j >= len(doc) || doc[j] != '=' {
			continue
		}
		j = skipHTMLSpace(doc, j+1)
		if j >= len(doc) {
			break
		}
		var value string
		if quote := doc[j]; quote == '"' || quote == '\'' {
			end := strings.IndexByte(doc[j+1:], quote)
			if end < 0 {
				break
			}
			value = doc[j+1 : j+1+end]
			i = j + 1 + end
		} else {
			end := j
			for end < len(doc) && !isHTMLSpace(doc[end]) && doc[end] != '>' {
				end++
			}
			value = doc[j:end]
			i = end - 1
		}
		link := strings.TrimSpace(html.UnescapeString(value))
		if schemeLen(link) > 0 && isHTTPURL(link) && !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}
	return links
}

// schemeLen returns the length of the "http://" or "https://" prefix of
// s, matched case-insensitively, or 0 when s starts with neither.
func schemeLen(s string) int {
	for _, prefix := range []string{"https://", "http://"} {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			return len(prefix)
		}
	}
	return 0
}

// continuesWord reports whether b, the byte before a scheme, makes the
// scheme part of a longer token ("xhttp://", "git+https://").
func continuesWord(b byte) bool {
	return b == '+' || b == '-' || b == '.' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// trimLinkSuffix removes characters that end a sentence rather than the
// URL: trailing punctuation, and closing brackets without a matching
// opening bracket inside the URL ("(see https://x.test/a)").
func trimLinkSuffix(link string) string {
	for link != "" {
		last := link[len(link)-1]
		switch {
		case strings.IndexByte(".,;:!?*", last) >= 0:
			link = link[:len(link)-1]
		case last == ')' && strings.Count(link, "(") < strings.Count(link, ")"),
			last == ']' && strings.Count(link, "[") < strings.Count(link, "]"),
			last == '}' && strings.Count(link, "{") < strings.Count(link, "}"):
			link = link[:len(link)-1]
		default:
			return link
		}
	}
	return link
}

// isHTTPURL reports whether link parses as an http or https URL with a
// host.
func isHTTPURL(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Hostname() != ""
}

// isAttrSeparator reports whether b may precede an attribute name, so
// that "data-href" or "xhref" are not mistaken for href.
func isAttrSeparator(b byte) bool {
	return isHTMLSpace(b) || b == '/' || b == '"' || b == '\''
}

// isHTMLSpace reports whether b is HTML whitespace.
func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

// skipHTMLSpace returns the index of the first non-whitespace byte of s
// at or after i.
func skipHTMLSpace(s string, i int) int {
	for i < len(s) && isHTMLSpace(s[i]) {
		i++
	}
	return i
}
