// Package console renders human-friendly terminal output: ANSI colors
// that switch themselves off when they would not help, and a compact,
// readable slog handler for development logs.
package console

import (
	"os"
	"strconv"
	"strings"
)

// ColorEnabled reports whether output written to f should use ANSI
// colors. It follows the common conventions: NO_COLOR (any non-empty
// value) turns colors off, FORCE_COLOR (any non-empty value) turns them
// on, TERM=dumb turns them off, and otherwise colors are used only when
// f is a terminal. getenv is os.Getenv in production; tests inject it.
func ColorEnabled(f *os.File, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	if getenv("FORCE_COLOR") != "" {
		return true
	}
	if strings.EqualFold(getenv("TERM"), "dumb") {
		return false
	}
	return IsTerminal(f)
}

// IsTerminal reports whether f is a character device, which is how a
// terminal shows up on Unix and in modern Windows consoles.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ANSI escape sequences used by Palette.
const (
	reset   = "\x1b[0m"
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	red     = "\x1b[31m"
	green   = "\x1b[32m"
	yellow  = "\x1b[33m"
	blue    = "\x1b[34m"
	magenta = "\x1b[35m"
	cyan    = "\x1b[36m"
)

// Palette wraps text in ANSI colors when enabled and returns it
// unchanged otherwise, so callers never branch on color support.
type Palette struct {
	enabled bool
}

// NewPalette returns a palette that colors text only when enabled.
func NewPalette(enabled bool) Palette {
	return Palette{enabled: enabled}
}

// Enabled reports whether the palette emits colors.
func (p Palette) Enabled() bool { return p.enabled }

func (p Palette) wrap(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return code + s + reset
}

// Bold renders s in bold.
func (p Palette) Bold(s string) string { return p.wrap(bold, s) }

// Dim renders s faint, for secondary information.
func (p Palette) Dim(s string) string { return p.wrap(dim, s) }

// Red renders s in red.
func (p Palette) Red(s string) string { return p.wrap(red, s) }

// Green renders s in green.
func (p Palette) Green(s string) string { return p.wrap(green, s) }

// Yellow renders s in yellow.
func (p Palette) Yellow(s string) string { return p.wrap(yellow, s) }

// Blue renders s in blue.
func (p Palette) Blue(s string) string { return p.wrap(blue, s) }

// Magenta renders s in magenta.
func (p Palette) Magenta(s string) string { return p.wrap(magenta, s) }

// Cyan renders s in cyan.
func (p Palette) Cyan(s string) string { return p.wrap(cyan, s) }

// Method renders an HTTP method in the color conventionally used for it:
// reads green, creates cyan, updates yellow, deletes red, others magenta.
func (p Palette) Method(method string) string {
	switch method {
	case "GET", "HEAD":
		return p.Green(method)
	case "POST":
		return p.Cyan(method)
	case "PUT", "PATCH":
		return p.Yellow(method)
	case "DELETE":
		return p.Red(method)
	default:
		return p.Magenta(method)
	}
}

// Status renders an HTTP status code by class: 2xx green, 3xx cyan,
// 4xx yellow, 5xx red.
func (p Palette) Status(code int64) string {
	s := strconv.FormatInt(code, 10)
	switch {
	case code >= 500:
		return p.Red(s)
	case code >= 400:
		return p.Yellow(s)
	case code >= 300:
		return p.Cyan(s)
	case code >= 200:
		return p.Green(s)
	default:
		return s
	}
}
