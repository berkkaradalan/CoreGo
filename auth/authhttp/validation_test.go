package authhttp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestSignUpProfileValidation(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		image   string
		message string
	}{
		{name: "huge name with a javascript image", user: strings.Repeat("A", 900_000), image: "javascript:alert(document.domain)", message: msgNameTooLong},
		{name: "name of 257 characters", user: strings.Repeat("é", 257), message: msgNameTooLong},
		{name: "javascript image", user: "Ada", image: "javascript:alert(document.domain)", message: msgInvalidImageURL},
		{name: "data image", user: "Ada", image: "data:image/png;base64,AAAA", message: msgInvalidImageURL},
		{name: "image without a host", user: "Ada", image: "https://", message: msgInvalidImageURL},
		{name: "image over 2048 bytes", user: "Ada", image: "https://img.example/" + strings.Repeat("a", 2048), message: msgInvalidImageURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, appOptions{})
			rec := app.do(t, req{method: http.MethodPost, path: "/signup", body: map[string]any{
				"name": tt.user, "email": "ada@example.com", "password": testPassword, "image": tt.image,
			}})
			expectError(t, rec, http.StatusBadRequest, tt.message)
			if _, err := app.store.GetUserByEmail(t.Context(), "ada@example.com"); err == nil {
				t.Fatal("the rejected sign-up created a user")
			}
		})
	}

	app := newTestApp(t, appOptions{})
	out := app.signUp(t, strings.Repeat("é", 256), "ada@example.com", testPassword)
	if len([]rune(out.User.Name)) != 256 {
		t.Fatalf("a 256 character name must be accepted, got %d", len([]rune(out.User.Name)))
	}
}

// conflictStore fails every user update with auth.ErrConflict, as if
// concurrent writers always won the race.
type conflictStore struct {
	*auth.MemoryStore
}

func (s *conflictStore) UpdateUser(context.Context, *auth.User) error {
	return auth.ErrConflict
}

func TestConcurrentModificationIs409(t *testing.T) {
	app := newTestApp(t, appOptions{store: &conflictStore{MemoryStore: auth.NewMemoryStore()}})
	token := app.signUp(t, "Ada", "ada@example.com", testPassword).Token
	rec := app.do(t, req{method: http.MethodPatch, path: "/me", token: token, body: map[string]any{"name": "Grace"}})
	expectError(t, rec, http.StatusConflict, "The account was modified concurrently. Please retry.")
}
