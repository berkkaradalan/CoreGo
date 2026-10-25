package mongostore

import (
	"strings"
	"testing"
)

func TestValidatePrefix(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{name: "empty", prefix: "", wantErr: false},
		{name: "default", prefix: DefaultCollectionPrefix, wantErr: false},
		{name: "letters only", prefix: "auth", wantErr: false},
		{name: "letters digits underscore", prefix: "tenant_42_", wantErr: false},
		{name: "underscore only", prefix: "_", wantErr: false},
		{name: "digits only", prefix: "2024", wantErr: false},
		{name: "maximum length", prefix: strings.Repeat("a", maxCollectionPrefixLength), wantErr: false},
		{name: "too long", prefix: strings.Repeat("a", maxCollectionPrefixLength+1), wantErr: true},
		{name: "uppercase letter", prefix: "Corego_", wantErr: true},
		{name: "dot", prefix: "corego.", wantErr: true},
		{name: "dollar sign", prefix: "corego$", wantErr: true},
		{name: "hyphen", prefix: "core-go_", wantErr: true},
		{name: "space", prefix: "core go", wantErr: true},
		{name: "slash", prefix: "core/go", wantErr: true},
		{name: "nul byte", prefix: "core\x00go", wantErr: true},
		{name: "non ASCII letter", prefix: "cørego_", wantErr: true},
		{name: "system namespace attempt", prefix: "system.", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePrefix(tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePrefix(%q) error = %v, wantErr %v", tt.prefix, err, tt.wantErr)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "mongostore: ") {
				t.Fatalf("error %q does not carry the package prefix", err)
			}
		})
	}
}

func TestResolvePrefix(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty selects the default", in: "", want: "corego_"},
		{name: "custom prefix is kept", in: "billing_", want: "billing_"},
		{name: "prefix without trailing underscore is kept verbatim", in: "app", want: "app"},
		{name: "invalid prefix is rejected", in: "App_", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePrefix(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolvePrefix(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("resolvePrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewCollectionNames(t *testing.T) {
	got := newCollectionNames("corego_")
	want := collectionNames{
		users:              "corego_users",
		accounts:           "corego_accounts",
		sessions:           "corego_sessions",
		verificationTokens: "corego_verification_tokens",
	}
	if got != want {
		t.Fatalf("newCollectionNames(corego_) = %+v, want %+v", got, want)
	}

	got = newCollectionNames("t1_")
	if got.users != "t1_users" || got.verificationTokens != "t1_verification_tokens" {
		t.Fatalf("newCollectionNames(t1_) = %+v", got)
	}
}

func TestNewStoreErrors(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		wantMessage string
	}{
		{name: "nil database with default prefix", opts: Options{}, wantMessage: "got nil"},
		{name: "nil database with valid custom prefix", opts: Options{CollectionPrefix: "app_"}, wantMessage: "got nil"},
		{name: "invalid prefix is reported first", opts: Options{CollectionPrefix: "Bad-Prefix"}, wantMessage: "invalid collection prefix"},
		{name: "too long prefix", opts: Options{CollectionPrefix: strings.Repeat("x", 65)}, wantMessage: "at most 64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewStore(nil, tt.opts)
			if err == nil {
				t.Fatal("NewStore returned no error")
			}
			if store != nil {
				t.Fatalf("NewStore returned a store together with an error: %+v", store)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("NewStore error = %q, want it to mention %q", err, tt.wantMessage)
			}
		})
	}
}
