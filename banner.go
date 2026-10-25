package corego

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/internal/console"
	"github.com/berkkaradalan/CoreGo/web"
)

// bannerMode selects whether Run prints the startup banner.
type bannerMode int

const (
	// bannerAuto prints the banner when standard error is a terminal.
	bannerAuto bannerMode = iota
	// bannerOn always prints it (COREGO_BANNER=true).
	bannerOn
	// bannerOff never prints it (COREGO_BANNER=false or
	// ServerOptions.DisableBanner).
	bannerOff
)

// envBanner is the environment variable that forces the banner on or off.
const envBanner = "COREGO_BANNER"

// readBannerMode reads COREGO_BANNER: unset or empty selects the
// automatic mode; otherwise it must be a strconv.ParseBool value.
func readBannerMode(e *env.Env) (bannerMode, error) {
	raw := strings.TrimSpace(e.GetOrDefault(envBanner, ""))
	if raw == "" {
		return bannerAuto, nil
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		return bannerAuto, fmt.Errorf("corego: invalid %s %q: valid values are true or false (also 1, t, T, TRUE, True, 0, f, F, FALSE, False)", envBanner, raw)
	}
	if on {
		return bannerOn, nil
	}
	return bannerOff, nil
}

// showBanner reports whether Run prints the banner.
func (c *Core) showBanner() bool {
	switch c.banner {
	case bannerOn:
		return true
	case bannerOff:
		return false
	default:
		return console.IsTerminal(os.Stderr)
	}
}

// authSummary is what the banner shows about the auth service; initAuth
// records it.
type authSummary struct {
	store                string
	prefix               string
	basePath             string // "/" when the routes are mounted at the root
	mail                 string
	requireVerifiedEmail bool
	cookie               bool
}

// Column widths of the banner tables.
const (
	bannerServiceWidth = 12 // "PostgreSQL" + gap
	bannerMethodWidth  = 8  // "OPTIONS" + gap
)

// writeBanner writes the startup summary: the version and address, the
// services in use and every registered route, the application's own
// first and the auth routes after them.
func (c *Core) writeBanner(w io.Writer, p console.Palette, addr string) {
	var b strings.Builder

	b.WriteString("\n  " + p.Bold(p.Cyan("CoreGo")) + " " + p.Dim(Version) + "  " + p.Bold(c.Env.AppURL))
	b.WriteString(p.Dim("  (listening on "+addr+")") + "\n\n")

	b.WriteString("  " + p.Bold("Services") + "\n")
	for _, s := range c.services() {
		mark, name, detail := p.Dim("·"), s.name, p.Dim("not configured")
		if s.active {
			mark, detail = p.Green("✓"), s.detail
		}
		b.WriteString("    " + mark + " " + pad(name, bannerServiceWidth) + detail + "\n")
	}

	appRoutes, authRoutes := c.splitRoutes()
	width := routePathWidth(appRoutes, authRoutes)
	b.WriteString("\n  " + p.Bold("Routes") + "\n")
	if len(appRoutes) == 0 {
		b.WriteString("    " + p.Dim(`none yet; add one with app.Get("/path", handler)`) + "\n")
	}
	writeRoutes(&b, p, appRoutes, width)

	if len(authRoutes) > 0 {
		b.WriteString("\n  " + p.Bold("Auth routes") + p.Dim(fmt.Sprintf(" (%d, under %s)", len(authRoutes), c.authInfo.basePath)) + "\n")
		writeRoutes(&b, p, authRoutes, width)
	}
	b.WriteString("\n")

	_, _ = io.WriteString(w, b.String())
}

// bannerService is one line of the Services table.
type bannerService struct {
	name   string
	active bool
	detail string
}

// services lists the parts of the facade and whether they are in use.
func (c *Core) services() []bannerService {
	services := []bannerService{{name: "Auth"}, {name: "MongoDB"}, {name: "PostgreSQL"}, {name: "CORS"}}

	if a := c.authInfo; a != nil {
		store := a.store
		if a.prefix != "" {
			store += " (" + a.prefix + ")"
		}
		mail := a.mail
		if mail == mailKindNone {
			mail = "off"
		}
		parts := []string{"routes under " + a.basePath, "store " + store, "email " + mail}
		if a.requireVerifiedEmail {
			parts = append(parts, "verified email required")
		}
		if a.cookie {
			parts = append(parts, "cookie sessions")
		}
		services[0] = bannerService{name: "Auth", active: true, detail: strings.Join(parts, " · ")}
	}
	if c.Mongo != nil {
		services[1] = bannerService{name: "MongoDB", active: true, detail: databaseTarget(c.Mongo.Name(), urlHosts(c.mongoURL))}
	}
	if c.Postgres != nil {
		cc := c.Postgres.Pool().Config().ConnConfig
		services[2] = bannerService{name: "PostgreSQL", active: true, detail: databaseTarget(cc.Database, cc.Host+":"+strconv.Itoa(int(cc.Port)))}
	}
	if c.cors != nil {
		services[3] = bannerService{name: "CORS", active: true, detail: strings.Join(c.cors.AllowedOrigins, ", ")}
	}
	return services
}

// databaseTarget renders "<database> on <host>", leaving out what is
// unknown.
func databaseTarget(database, host string) string {
	switch {
	case database != "" && host != "":
		return database + " on " + host
	case host != "":
		return host
	default:
		return database
	}
}

// urlHosts returns the host list of a connection URL without the scheme,
// the credentials, the path and the query, so it is safe to print:
// "mongodb://user:secret@a:27017,b:27017/app?x=1" gives "a:27017,b:27017".
func urlHosts(raw string) string {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	return rest
}

// bannerRoute is one line of a route table.
type bannerRoute struct {
	method          string
	path            string
	requiresSession bool
}

// splitRoutes returns the application's routes, sorted by path and then
// method, and the auth routes in their registration order (sign-up
// first), marked when they need a session token.
func (c *Core) splitRoutes() (app, authRoutes []bannerRoute) {
	if c.server == nil {
		return nil, nil
	}
	isAuth := map[web.Route]bool{}
	if c.authHandlers != nil {
		for _, r := range c.authHandlers.Routes() {
			isAuth[web.Route{Method: r.Method, Path: r.Path}] = true
			authRoutes = append(authRoutes, bannerRoute{method: r.Method, path: r.Path, requiresSession: r.RequiresSession})
		}
	}
	registered := c.server.Routes()
	sortRoutes(registered)
	for _, r := range registered {
		if !isAuth[r] {
			app = append(app, bannerRoute{method: r.Method, path: r.Path})
		}
	}
	return app, authRoutes
}

// methodOrder ranks methods in the order people read a resource.
var methodOrder = map[string]int{"GET": 0, "HEAD": 1, "POST": 2, "PUT": 3, "PATCH": 4, "DELETE": 5, "OPTIONS": 6}

// sortRoutes orders routes by path, then by methodOrder.
func sortRoutes(routes []web.Route) {
	rank := func(m string) int {
		if r, ok := methodOrder[m]; ok {
			return r
		}
		return len(methodOrder)
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return rank(routes[i].Method) < rank(routes[j].Method)
	})
}

// routePathWidth is the column width that aligns the notes after the
// longest path of both tables.
func routePathWidth(tables ...[]bannerRoute) int {
	width := 0
	for _, routes := range tables {
		for _, r := range routes {
			width = max(width, len(r.path))
		}
	}
	return width + 2
}

// writeRoutes writes one "METHOD  /path" line per route, with a note on
// the routes that need a session token.
func writeRoutes(b *strings.Builder, p console.Palette, routes []bannerRoute, pathWidth int) {
	for _, r := range routes {
		// Color the bare method, then pad: the palette matches on the name.
		line := "    " + p.Method(r.method) + strings.Repeat(" ", max(bannerMethodWidth-len(r.method), 1))
		if r.requiresSession {
			line += pad(r.path, pathWidth) + p.Dim("session required")
		} else {
			line += r.path
		}
		b.WriteString(line + "\n")
	}
}

// pad right-pads s with spaces to width; longer strings get one space.
func pad(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-len(s))
}
