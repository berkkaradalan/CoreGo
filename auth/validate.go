package auth

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// Profile field limits enforced by SignUp and UpdateUser.
const (
	// maxNameLength is the maximum display name length in characters
	// (Unicode code points), counted after trimming.
	maxNameLength = 256

	// maxImageURLLength is the maximum image URL length in bytes,
	// counted after trimming.
	maxImageURLLength = 2048
)

// normalizeName trims surrounding whitespace from a display name and
// validates the result: an empty name yields ErrNameRequired and a
// name longer than maxNameLength characters yields ErrNameTooLong.
func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return "", ErrNameTooLong
	}
	return name, nil
}

// normalizeImage trims surrounding whitespace from an image URL and
// validates the result. The empty string is valid (no image).
// Anything else must be at most maxImageURLLength bytes and parse as
// an absolute URL with the http or https scheme (case-insensitive) and
// a non-empty host; everything else, including javascript: and data:
// URLs and relative references, yields ErrInvalidImageURL.
//
// Rejecting other schemes matters because applications commonly render
// the value as an <img src> or a link: a javascript: URL there is a
// cross-site scripting vector, and an unbounded value lets a single
// user bloat every response that carries the profile.
func normalizeImage(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", nil
	}
	if len(image) > maxImageURLLength {
		return "", ErrInvalidImageURL
	}
	parsed, err := url.Parse(image)
	if err != nil || parsed.Host == "" {
		return "", ErrInvalidImageURL
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return image, nil
	default:
		return "", ErrInvalidImageURL
	}
}
