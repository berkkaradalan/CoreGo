package auth

import "context"

// sessionContextKey is the unexported key under which the
// authenticated session and user are stored in a context. Being an
// unexported type, it cannot collide with keys of other packages.
type sessionContextKey struct{}

// sessionContextValue is what ContextWithSession stores.
type sessionContextValue struct {
	session *Session
	user    *User
}

// ContextWithSession returns a copy of ctx that carries the
// authenticated session and user. The HTTP layer calls it after
// resolving a request's token; application code reads the values with
// UserFromContext, SessionFromContext or MustUser. These helpers are
// framework agnostic: any context.Context works, including
// *web.Context, which implements context.Context.
//
// A nil ctx is treated as context.Background(). Nil session or user
// values are stored as given and reported as absent by the readers.
func ContextWithSession(ctx context.Context, session *Session, user *User) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionContextValue{session: session, user: user})
}

// UserFromContext returns the authenticated user stored by
// ContextWithSession and whether one is present. It returns (nil,
// false) for a nil ctx, a context without a session and a stored nil
// user.
func UserFromContext(ctx context.Context) (*User, bool) {
	value, ok := sessionFromContextValue(ctx)
	if !ok || value.user == nil {
		return nil, false
	}
	return value.user, true
}

// SessionFromContext returns the authenticated session stored by
// ContextWithSession and whether one is present. It returns (nil,
// false) for a nil ctx, a context without a session and a stored nil
// session.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	value, ok := sessionFromContextValue(ctx)
	if !ok || value.session == nil {
		return nil, false
	}
	return value.session, true
}

// MustUser returns the authenticated user stored in ctx. Use it in
// handlers behind the RequireAuth middleware, where a user is always
// present:
//
//	api.Get("/me", func(c *web.Context) error {
//		user := auth.MustUser(c)
//		return c.JSON(http.StatusOK, map[string]string{"id": user.ID, "name": user.Name})
//	})
//
// It panics when no user is present, because that is a programming
// error (a route that is not protected) rather than a runtime
// condition; the web framework's recovery turns the panic into a 500.
// Use UserFromContext on routes where authentication is optional (the
// HTTP layer's OptionalAuth middleware).
func MustUser(ctx context.Context) *User {
	user, ok := UserFromContext(ctx)
	if !ok {
		panic("auth: no authenticated user in context; protect the route with RequireAuth")
	}
	return user
}

// sessionFromContextValue extracts the stored value; a nil ctx holds
// nothing.
func sessionFromContextValue(ctx context.Context) (sessionContextValue, bool) {
	if ctx == nil {
		return sessionContextValue{}, false
	}
	value, ok := ctx.Value(sessionContextKey{}).(sessionContextValue)
	return value, ok
}
