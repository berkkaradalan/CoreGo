package authhttp_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/web"
)

// newExampleService returns an in-memory auth service for the examples.
func newExampleService() *auth.Auth {
	service, err := auth.New(auth.Config{Logger: slog.New(slog.DiscardHandler)}, auth.NewMemoryStore())
	if err != nil {
		log.Fatal(err)
	}
	return service
}

// Mount the auth routes on a server, then sign up over HTTP.
func ExampleHandlers_Mount() {
	server := web.New()
	authhttp.New(newExampleService(), authhttp.Config{}).Mount(server)

	body := `{"name":"Ada","email":"ada@example.com","password":"analytical-engine"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/signup", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)

	fmt.Println(w.Code)
	fmt.Println(len(server.Routes()), "routes")
	// Output:
	// 201
	// 17 routes
}

// Protect application routes with RequireAuth and read the signed-in user
// with auth.MustUser.
func ExampleHandlers_RequireAuth() {
	service := newExampleService()
	h := authhttp.New(service, authhttp.Config{})
	server := web.New()
	h.Mount(server)

	api := server.Group("/api", h.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		user := auth.MustUser(c)
		return c.String(http.StatusOK, "hello "+user.Name)
	})

	out, err := service.SignUp(context.Background(), auth.SignUpInput{
		Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, token := range []string{out.SessionToken, ""} {
		r := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		fmt.Println(w.Code, strings.TrimSpace(w.Body.String()))
	}
	// Output:
	// 200 hello Ada
	// 401 {"error":"Missing session token."}
}

// Protect a plain net/http handler with Middleware.
func ExampleHandlers_Middleware() {
	h := authhttp.New(newExampleService(), authhttp.Config{})

	mux := http.NewServeMux()
	mux.Handle("/dashboard", h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "welcome %s", auth.MustUser(r.Context()).Name)
	})))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	fmt.Println(w.Code, w.Body.String())
	// Output:
	// 401 {"error":"Missing session token."}
}

// Serve signed-in and anonymous visitors from one route with
// OptionalAuth and auth.UserFromContext.
func ExampleHandlers_OptionalAuth() {
	service := newExampleService()
	h := authhttp.New(service, authhttp.Config{})
	server := web.New()

	server.Get("/greeting", func(c *web.Context) error {
		if user, ok := auth.UserFromContext(c); ok {
			return c.String(http.StatusOK, "hello "+user.Name)
		}
		return c.String(http.StatusOK, "hello guest")
	}, h.OptionalAuth())

	out, err := service.SignUp(context.Background(), auth.SignUpInput{
		Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, token := range []string{out.SessionToken, "", "revoked-or-unknown"} {
		r := httptest.NewRequest(http.MethodGet, "/greeting", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		fmt.Println(w.Code, w.Body.String())
	}
	// Output:
	// 200 hello Ada
	// 200 hello guest
	// 200 hello guest
}

// Let a frontend on another origin use the session cookie for
// state-changing requests.
func ExampleConfig_trustedOrigins() {
	cfg := authhttp.Config{
		Cookie: authhttp.CookieConfig{
			Enabled:  true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		},
		TrustedOrigins: []string{"https://app.example.com"},
	}
	fmt.Println(cfg.Validate())

	cfg.TrustedOrigins = []string{"*"}
	fmt.Println(cfg.Validate())
	// Output:
	// <nil>
	// authhttp: invalid TrustedOrigins entry "*": wildcards are not allowed; list each origin
}
