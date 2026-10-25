package auth

import (
	"context"
	"testing"
)

func TestContextWithSessionRoundTrip(t *testing.T) {
	session := &Session{ID: "session-1", UserID: "user-1"}
	user := &User{ID: "user-1", Email: "ctx@example.com"}
	ctx := ContextWithSession(testContext, session, user)

	gotUser, ok := UserFromContext(ctx)
	if !ok || gotUser != user {
		t.Errorf("UserFromContext = (%v, %v), want the stored user", gotUser, ok)
	}
	gotSession, ok := SessionFromContext(ctx)
	if !ok || gotSession != session {
		t.Errorf("SessionFromContext = (%v, %v), want the stored session", gotSession, ok)
	}
	if MustUser(ctx) != user {
		t.Error("MustUser returned a different user")
	}
	// The parent is not modified.
	if _, ok := UserFromContext(testContext); ok {
		t.Error("the parent context gained a user")
	}
}

// otherKey is a foreign context key type; values stored under it must
// never be mistaken for the authenticated user.
type otherKey string

func TestContextHelpersWithoutSession(t *testing.T) {
	var nilCtx context.Context
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"nil context", nilCtx},
		{"unrelated value of the same type", context.WithValue(context.Background(), otherKey("user"), &User{ID: "x"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if user, ok := UserFromContext(tt.ctx); ok || user != nil {
				t.Errorf("UserFromContext = (%v, %v), want (nil, false)", user, ok)
			}
			if session, ok := SessionFromContext(tt.ctx); ok || session != nil {
				t.Errorf("SessionFromContext = (%v, %v), want (nil, false)", session, ok)
			}
		})
	}
}

func TestContextWithSessionNilValues(t *testing.T) {
	session := &Session{ID: "session-only"}
	ctx := ContextWithSession(testContext, session, nil)
	if user, ok := UserFromContext(ctx); ok || user != nil {
		t.Errorf("UserFromContext with a nil user = (%v, %v), want (nil, false)", user, ok)
	}
	if got, ok := SessionFromContext(ctx); !ok || got != session {
		t.Errorf("SessionFromContext = (%v, %v), want the session", got, ok)
	}

	user := &User{ID: "user-only"}
	ctx = ContextWithSession(testContext, nil, user)
	if session, ok := SessionFromContext(ctx); ok || session != nil {
		t.Errorf("SessionFromContext with a nil session = (%v, %v), want (nil, false)", session, ok)
	}
	if got, ok := UserFromContext(ctx); !ok || got != user {
		t.Errorf("UserFromContext = (%v, %v), want the user", got, ok)
	}
}

func TestContextWithSessionNilParent(t *testing.T) {
	var nilCtx context.Context
	user := &User{ID: "user-1"}
	ctx := ContextWithSession(nilCtx, nil, user)
	if ctx == nil {
		t.Fatal("ContextWithSession(nil, ...) returned nil")
	}
	if got, ok := UserFromContext(ctx); !ok || got != user {
		t.Errorf("UserFromContext = (%v, %v), want the user", got, ok)
	}
}

func TestContextWithSessionInnermostWins(t *testing.T) {
	outer := ContextWithSession(testContext, &Session{ID: "outer"}, &User{ID: "outer"})
	inner := ContextWithSession(outer, &Session{ID: "inner"}, &User{ID: "inner"})
	if MustUser(inner).ID != "inner" {
		t.Error("inner context does not shadow the outer session")
	}
	if MustUser(outer).ID != "outer" {
		t.Error("outer context was modified")
	}
	// A cancelable child keeps the values.
	child, cancel := context.WithCancel(inner)
	defer cancel()
	if MustUser(child).ID != "inner" {
		t.Error("child context lost the user")
	}
}

func TestMustUserPanicsWithoutUser(t *testing.T) {
	const want = "auth: no authenticated user in context; protect the route with RequireAuth"
	var nilCtx context.Context
	for name, ctx := range map[string]context.Context{
		"background": context.Background(),
		"nil":        nilCtx,
		"nil user":   ContextWithSession(context.Background(), &Session{ID: "s"}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got := recover()
				if got != want {
					t.Errorf("panic value = %v, want %q", got, want)
				}
			}()
			MustUser(ctx)
			t.Error("MustUser did not panic")
		})
	}
}
