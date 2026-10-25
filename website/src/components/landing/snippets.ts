/**
 * The code shown on the landing page. HELLO_WORLD is the hello world of
 * docs/getting-started.md, unchanged, and compiles against the current
 * release; keep the two in sync when an API changes. Its longest line is
 * 75 columns at a tab size of 4, so the block fits its column without
 * scrolling from the md breakpoint up.
 */

/** The hello world of docs/getting-started.md, unchanged. */
export const HELLO_WORLD = `package main

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
	api := app.Group("/api", core.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		user := auth.MustUser(c)
		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
	})
	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}`;
