package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"plain", "Ada", "Ada", nil},
		{"trimmed", "  Ada Lovelace \t\n", "Ada Lovelace", nil},
		{"inner whitespace kept", "Ada   Lovelace", "Ada   Lovelace", nil},
		{"empty", "", "", ErrNameRequired},
		{"spaces only", "   ", "", ErrNameRequired},
		{"mixed whitespace only", " \t\r\n  ", "", ErrNameRequired},
		{"256 ascii", strings.Repeat("a", 256), strings.Repeat("a", 256), nil},
		{"257 ascii", strings.Repeat("a", 257), "", ErrNameTooLong},
		{"256 two-byte runes", strings.Repeat("é", 256), strings.Repeat("é", 256), nil},
		{"257 two-byte runes", strings.Repeat("é", 257), "", ErrNameTooLong},
		{"256 three-byte runes", strings.Repeat("日", 256), strings.Repeat("日", 256), nil},
		{"256 four-byte runes", strings.Repeat("😀", 256), strings.Repeat("😀", 256), nil},
		{"257 four-byte runes", strings.Repeat("😀", 257), "", ErrNameTooLong},
		{"256 after trimming", "   " + strings.Repeat("b", 256) + "   ", strings.Repeat("b", 256), nil},
		{"huge", strings.Repeat("A", 900_000), "", ErrNameTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeName(tt.in)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeImage(t *testing.T) {
	// A valid URL of exactly n bytes.
	sized := func(n int) string {
		prefix := "https://cdn.example.com/"
		return prefix + strings.Repeat("a", n-len(prefix))
	}
	tests := []struct {
		name  string
		in    string
		want  string
		valid bool
	}{
		{"empty", "", "", true},
		{"whitespace only clears", "  \t ", "", true},
		{"https", "https://cdn.example.com/a.png", "https://cdn.example.com/a.png", true},
		{"http", "http://cdn.example.com/a.png", "http://cdn.example.com/a.png", true},
		{"uppercase scheme", "HTTPS://CDN.EXAMPLE.COM/A.PNG", "HTTPS://CDN.EXAMPLE.COM/A.PNG", true},
		{"port query fragment", "https://cdn.example.com:8443/a.png?s=64#x", "https://cdn.example.com:8443/a.png?s=64#x", true},
		{"ip host", "http://127.0.0.1/a.png", "http://127.0.0.1/a.png", true},
		{"trimmed", "  https://cdn.example.com/a.png\n", "https://cdn.example.com/a.png", true},
		{"exactly 2048 bytes", sized(2048), sized(2048), true},
		{"2048 bytes after trimming", " " + sized(2048) + " ", sized(2048), true},
		{"2049 bytes", sized(2049), "", false},
		{"javascript", "javascript:alert(document.domain)", "", false},
		{"javascript with slashes", "JavaScript://cdn.example.com/%0Aalert(1)", "", false},
		{"data", "data:image/png;base64,AAAA", "", false},
		{"vbscript", "vbscript:msgbox(1)", "", false},
		{"ftp", "ftp://cdn.example.com/a.png", "", false},
		{"file", "file:///etc/passwd", "", false},
		{"relative path", "/avatars/a.png", "", false},
		{"bare file", "a.png", "", false},
		{"scheme relative", "//cdn.example.com/a.png", "", false},
		{"no host", "https://", "", false},
		{"empty host with path", "https:///a.png", "", false},
		{"opaque", "http:cdn.example.com/a.png", "", false},
		{"space in host", "https://cdn example.com/a.png", "", false},
		{"control character", "https://cdn.example.com/a\x00.png", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeImage(tt.in)
			if tt.valid {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				if got != tt.want {
					t.Errorf("image = %q, want %q", got, tt.want)
				}
				return
			}
			if !errors.Is(err, ErrInvalidImageURL) {
				t.Fatalf("error = %v, want ErrInvalidImageURL", err)
			}
		})
	}
}

// TestSignUpValidatesProfile: oversized names and non-http(s) images
// are rejected before anything is stored.
func TestSignUpValidatesProfile(t *testing.T) {
	tests := []struct {
		name    string
		in      SignUpInput
		wantErr error
	}{
		{"900KB name and javascript image", SignUpInput{Name: strings.Repeat("A", 900_000), Image: "javascript:alert(document.domain)"}, ErrNameTooLong},
		{"257 character name", SignUpInput{Name: strings.Repeat("é", 257)}, ErrNameTooLong},
		{"javascript image", SignUpInput{Name: "Ada", Image: "javascript:alert(1)"}, ErrInvalidImageURL},
		{"relative image", SignUpInput{Name: "Ada", Image: "/a.png"}, ErrInvalidImageURL},
		{"2049 byte image", SignUpInput{Name: "Ada", Image: "https://cdn.example.com/" + strings.Repeat("a", 2049-24)}, ErrInvalidImageURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newTestAuth(t, Config{})
			tt.in.Email = "profile@example.com"
			tt.in.Password = testPassword
			if _, err := a.SignUp(testContext, tt.in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("SignUp error = %v, want %v", err, tt.wantErr)
			}
			if n, _ := a.CountUsers(testContext); n != 0 {
				t.Errorf("CountUsers = %d, want 0", n)
			}
		})
	}

	t.Run("limits are inclusive and values are trimmed", func(t *testing.T) {
		a, _ := newTestAuth(t, Config{})
		name := strings.Repeat("日", 256)
		image := "https://cdn.example.com/" + strings.Repeat("a", 2048-24)
		out, err := a.SignUp(testContext, SignUpInput{Name: " " + name + " ", Email: "edge@example.com", Password: testPassword, Image: "\t" + image + " "})
		if err != nil {
			t.Fatalf("SignUp: %v", err)
		}
		if out.User.Name != name || out.User.Image != image {
			t.Errorf("stored name/image not trimmed: %q / %q", out.User.Name, out.User.Image)
		}
	})
}

func TestUpdateUserValidatesProfile(t *testing.T) {
	a, _ := newTestAuth(t, Config{})
	out := signUpUser(t, a, "update-profile@example.com")
	str := func(s string) *string { return &s }

	tests := []struct {
		name    string
		in      UpdateUserInput
		wantErr error
	}{
		{"empty name", UpdateUserInput{Name: str("  ")}, ErrNameRequired},
		{"257 character name", UpdateUserInput{Name: str(strings.Repeat("x", 257))}, ErrNameTooLong},
		{"javascript image", UpdateUserInput{Image: str("javascript:alert(1)")}, ErrInvalidImageURL},
		{"valid name with invalid image", UpdateUserInput{Name: str("Fine"), Image: str("data:text/html,x")}, ErrInvalidImageURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := a.UpdateUser(testContext, out.User.ID, tt.in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateUser error = %v, want %v", err, tt.wantErr)
			}
			user, _ := a.GetUser(testContext, out.User.ID)
			if user.Name != out.User.Name || user.Image != "" || user.Version != 1 {
				t.Errorf("user changed by a rejected update: %+v", user)
			}
		})
	}

	t.Run("validation runs before the lookup", func(t *testing.T) {
		if _, err := a.UpdateUser(testContext, "missing", UpdateUserInput{Image: str("ftp://x.example/a")}); !errors.Is(err, ErrInvalidImageURL) {
			t.Fatalf("error = %v, want ErrInvalidImageURL", err)
		}
	})
	t.Run("trimmed values are stored and whitespace clears the image", func(t *testing.T) {
		user, err := a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Name: str(" New "), Image: str(" https://cdn.example.com/n.png ")})
		if err != nil || user.Name != "New" || user.Image != "https://cdn.example.com/n.png" {
			t.Fatalf("UpdateUser = %+v, %v", user, err)
		}
		user, err = a.UpdateUser(testContext, out.User.ID, UpdateUserInput{Image: str("   ")})
		if err != nil || user.Image != "" {
			t.Fatalf("clearing UpdateUser = %+v, %v", user, err)
		}
	})
}
