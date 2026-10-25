package env

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
)

// readDotenv reads and parses a .env file. When required is false a
// missing file yields an empty map and no error.
func readDotenv(path string, required bool) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("env: read %s: %w", path, err)
	}
	vars, err := parseDotenv(content)
	if err != nil {
		return nil, fmt.Errorf("env: parse %s: line %d: %s", path, failingLine(content), describeParseError(err))
	}
	return vars, nil
}

// parseDotenv parses .env content with godotenv (which never touches
// the process environment) and additionally rejects variable names
// that are empty or contain whitespace, which godotenv accepts silently
// (for example a line reading "=value" or "MY VAR=value").
func parseDotenv(content []byte) (map[string]string, error) {
	vars, err := godotenv.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	for key := range vars {
		if key == "" || strings.IndexFunc(key, unicode.IsSpace) >= 0 {
			return nil, errInvalidName
		}
	}
	return vars, nil
}

// errInvalidName reports an empty variable name or one containing
// whitespace.
var errInvalidName = errors.New("invalid variable name")

// failingLine returns the 1-based line on which parsing of content
// fails. It is the line after the longest prefix of whole lines that
// still parses. Searching for the longest valid prefix (instead of the
// shortest invalid one) keeps a valid multi-line quoted value from
// being blamed for an error further down the file. .env files are
// small, so the quadratic cost is irrelevant; it is only paid on the
// error path.
func failingLine(content []byte) int {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	lastValid := 0
	for n := 1; n <= len(lines); n++ {
		if _, err := parseDotenv([]byte(strings.Join(lines[:n], "\n"))); err == nil {
			lastValid = n
		}
	}
	if lastValid >= len(lines) {
		// Defensive: every prefix parsed, so the whole file did too.
		return len(lines)
	}
	return lastValid + 1
}

// describeParseError maps a parser error to a short description that
// never contains file content. godotenv embeds the remainder of the
// file in its messages, which could leak secrets into logs.
func describeParseError(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, errInvalidName), strings.HasPrefix(msg, "unexpected character"):
		return "invalid variable name (allowed: letters, digits, '_' and '.', followed by '=')"
	case strings.HasPrefix(msg, "unterminated quoted value"):
		return "unterminated quoted value"
	default:
		return "syntax error"
	}
}
