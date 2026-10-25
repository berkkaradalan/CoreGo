package pgstore

import (
	"errors"
	"strings"
	"testing"
)

func TestResolvePrefix(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		want    string
		wantErr bool
	}{
		{name: "empty selects the default", prefix: "", want: "corego_"},
		{name: "letters and underscore", prefix: "myapp_", want: "myapp_"},
		{name: "no trailing underscore", prefix: "auth", want: "auth"},
		{name: "digits", prefix: "app2_", want: "app2_"},
		{name: "leading digit", prefix: "2fa_", want: "2fa_"},
		{name: "only underscores", prefix: "__", want: "__"},
		{name: "single letter", prefix: "a", want: "a"},
		{name: "maximum length", prefix: strings.Repeat("a", MaxTablePrefixLength), want: strings.Repeat("a", MaxTablePrefixLength)},
		{name: "one byte too long", prefix: strings.Repeat("a", MaxTablePrefixLength+1), wantErr: true},
		{name: "uppercase letter", prefix: "MyApp_", wantErr: true},
		{name: "hyphen", prefix: "my-app_", wantErr: true},
		{name: "space", prefix: "my app_", wantErr: true},
		{name: "dot (schema qualification)", prefix: "public.", wantErr: true},
		{name: "double quote", prefix: `a"b`, wantErr: true},
		{name: "single quote", prefix: "a'b", wantErr: true},
		{name: "semicolon", prefix: "a;b", wantErr: true},
		{name: "sql comment", prefix: "a--", wantErr: true},
		{name: "placeholder", prefix: "{p}", wantErr: true},
		{name: "non-ascii letter", prefix: "café_", wantErr: true},
		{name: "nul byte", prefix: "a\x00", wantErr: true},
		{name: "newline", prefix: "a\n", wantErr: true},
		{name: "whitespace only", prefix: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePrefix(Options{TablePrefix: tt.prefix})
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidTablePrefix) {
					t.Fatalf("resolvePrefix(%q) error = %v, want ErrInvalidTablePrefix", tt.prefix, err)
				}
				if got != "" {
					t.Fatalf("resolvePrefix(%q) = %q alongside an error", tt.prefix, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePrefix(%q) error = %v", tt.prefix, err)
			}
			if got != tt.want {
				t.Fatalf("resolvePrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}
