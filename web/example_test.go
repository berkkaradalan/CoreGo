package web_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/berkkaradalan/CoreGo/web"
)

type tenantKey struct{}

func Example() {
	s := web.New()
	s.Use(web.RequestLogger(nil))

	api := s.Group("/api")
	api.Get("/users/:id", func(c *web.Context) error {
		if c.Param("id") != "42" {
			return web.NotFound("user not found")
		}
		return c.JSON(http.StatusOK, map[string]string{"id": "42", "name": "Ada"})
	})

	// In a real program: if err := s.Run(":8080"); err != nil { log.Fatal(err) }
	for _, path := range []string{"/api/users/42", "/api/users/7"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(rec.Code, rec.Body.String())
	}
	// Output:
	// 200 {"id":"42","name":"Ada"}
	// 404 {"error":"user not found"}
}

func ExampleMiddlewareFunc() {
	tenant := func(next web.HandlerFunc) web.HandlerFunc {
		return func(c *web.Context) error {
			name := c.GetHeader("X-Tenant")
			if name == "" {
				return web.BadRequest("missing X-Tenant header")
			}
			ctx := context.WithValue(c.Request().Context(), tenantKey{}, name)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}

	s := web.New()
	s.Get("/whoami", func(c *web.Context) error {
		// c is a context.Context: values attached by middleware are visible.
		return c.String(http.StatusOK, c.Value(tenantKey{}).(string))
	}, tenant)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("X-Tenant", "acme")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 200 acme
}

func ExampleCORS() {
	s := web.New()
	s.Use(web.CORS(web.CORSConfig{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
	}))
	s.Post("/api/items", func(c *web.Context) error { return c.NoContent() })

	// A browser preflight is answered by the middleware.
	req := httptest.NewRequest(http.MethodOptions, "/api/items", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	// Output: 204 https://app.example.com
}

func ExampleRateLimit() {
	s := web.New()
	s.Post("/login", func(c *web.Context) error { return c.NoContent() },
		web.RateLimit(web.RateLimitConfig{Limit: 2, Window: time.Minute}))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/login", nil))
		fmt.Println(rec.Code, rec.Header().Get("RateLimit-Remaining"))
	}
	// Output:
	// 204 1
	// 204 0
	// 429 0
}

func ExampleWrapHandler() {
	s := web.New()
	s.Get("/health", web.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})))

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	fmt.Println(rec.Body.String())
	// Output: ok
}

func ExampleServer_Run() {
	s := web.New()
	s.TrustProxyHeaders = true // behind a reverse proxy that sets X-Forwarded-For
	s.WriteTimeout = time.Minute
	s.Get("/", func(c *web.Context) error { return c.String(http.StatusOK, "hello") })

	// Serves until SIGINT/SIGTERM, then shuts down gracefully. (This
	// example has no Output comment, so it is compiled but not run.)
	// A clean shutdown returns nil.
	if err := s.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
