// Command quickstart is the smallest complete CoreGo application:
// sign-up, sign-in and sessions under /auth, plus a few routes of its own
// behind core.RequireAuth.
//
// It needs no infrastructure. Without a database the auth service uses
// the in-memory store (data is lost on restart); set
// MONGODB_CONNECTION_URL or POSTGRES_CONNECTION_URL to persist it, and
// COREGO_MAIL=log to read the verification and reset links in the log.
//
// Run it from the repository root and follow README.md:
//
//	go run ./examples/quickstart
package main

import (
	"log"
	"net/http"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}
	app := core.Server()

	// A public route.
	app.Get("/health", func(c *web.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Everything in this group requires a valid session.
	api := app.Group("/api", core.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		user := auth.MustUser(c)
		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
	})
	api.Get("/sessions", func(c *web.Context) error {
		// web.Context is a context.Context: pass c straight to services.
		sessions, err := core.Auth.ListSessions(c, auth.MustUser(c).ID)
		if err != nil {
			return err // unknown errors are logged and answered with 500
		}
		return c.JSON(http.StatusOK, map[string]int{"active_sessions": len(sessions)})
	})
	api.Post("/notes", func(c *web.Context) error {
		var req struct {
			Text string `json:"text"`
		}
		if err := c.BindJSON(&req); err != nil {
			return err // malformed, oversized or non-JSON bodies become 400, 413 or 415
		}
		if req.Text == "" {
			return web.BadRequest("text is required")
		}
		return c.JSON(http.StatusCreated, map[string]string{
			"author": auth.MustUser(c).Name,
			"text":   req.Text,
		})
	})

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}
