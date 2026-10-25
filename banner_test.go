package corego

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/berkkaradalan/CoreGo/auth/authhttp"
	"github.com/berkkaradalan/CoreGo/internal/console"
	"github.com/berkkaradalan/CoreGo/web"
)

// renderBanner builds a Core from cfg, lets register add routes and
// returns the plain-text banner.
func renderBanner(t *testing.T, cfg *Config, register func(app *web.Server)) string {
	t.Helper()
	logger, _ := newTestLogger()
	cfg.Logger = logger
	core := newCore(t, cfg)
	app := core.Server()
	if register != nil {
		register(app)
	}
	var buf bytes.Buffer
	core.writeBanner(&buf, console.NewPalette(false), ":8080")
	return buf.String()
}

func noop(c *web.Context) error { return nil }

func TestBannerWithAuthAndRoutes(t *testing.T) {
	isolateEnv(t)
	t.Setenv("APP_URL", "http://localhost:8080")
	out := renderBanner(t, &Config{Auth: &AuthOptions{}}, func(app *web.Server) {
		api := app.Group("/api")
		api.Delete("/items/:id", noop)
		api.Post("/items", noop)
		api.Get("/items/:id", noop)
		api.Get("/items", noop)
	})

	for _, want := range []string{
		"CoreGo " + Version + "  http://localhost:8080  (listening on :8080)",
		"✓ Auth        routes under /auth · store memory · email off",
		"· MongoDB     not configured",
		"· PostgreSQL  not configured",
		"· CORS        not configured",
		"Auth routes (17, under /auth)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner lacks %q:\n%s", want, out)
		}
	}

	// Application routes: sorted by path, then GET before POST before DELETE.
	assertOrder(t, out,
		"GET     /api/items\n",
		"POST    /api/items\n",
		"GET     /api/items/:id\n",
		"DELETE  /api/items/:id\n",
		"Auth routes",
	)
	// Auth routes keep their registration order and mark the routes that
	// need a session.
	assertOrder(t, out, "POST    /auth/signup", "POST    /auth/signin", "POST    /auth/signout")
	if !lineHas(out, "/auth/signout", "session required") || !lineHas(out, "/auth/me", "session required") {
		t.Errorf("protected auth routes are not marked:\n%s", out)
	}
	if lineHas(out, "/auth/signup", "session required") {
		t.Errorf("sign-up must not be marked as protected:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain banner contains escape codes:\n%q", out)
	}
}

func TestBannerWithoutAuthOrRoutes(t *testing.T) {
	isolateEnv(t)
	out := renderBanner(t, &Config{}, nil)
	for _, want := range []string{
		"· Auth        not configured",
		`none yet; add one with app.Get("/path", handler)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Auth routes") {
		t.Errorf("no auth section expected without auth:\n%s", out)
	}
}

func TestBannerShowsAuthOptionsAndCORS(t *testing.T) {
	isolateEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,https://app.example.com")
	t.Setenv("COREGO_MAIL", "log")
	t.Setenv("COREGO_REQUIRE_VERIFIED_EMAIL", "true")
	t.Setenv("COREGO_AUTH_COOKIE", "true")
	out := renderBanner(t, &Config{Auth: &AuthOptions{HTTP: authhttp.Config{BasePath: "/accounts"}}}, nil)
	for _, want := range []string{
		"routes under /accounts · store memory · email log · verified email required · cookie sessions",
		"✓ CORS        http://localhost:5173, https://app.example.com",
		"Auth routes (17, under /accounts)",
		"POST    /accounts/signup",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner lacks %q:\n%s", want, out)
		}
	}
}

func TestBannerAuthAtRoot(t *testing.T) {
	isolateEnv(t)
	out := renderBanner(t, &Config{Auth: &AuthOptions{HTTP: authhttp.Config{BasePath: "/"}}}, func(app *web.Server) {
		app.Get("/hello", noop)
	})
	assertOrder(t, out, "routes under /", "GET     /hello", "Auth routes (17, under /)", "POST    /signup")
}

func TestBannerColors(t *testing.T) {
	isolateEnv(t)
	logger, _ := newTestLogger()
	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	core.Server().Get("/x", noop)
	var buf bytes.Buffer
	core.writeBanner(&buf, console.NewPalette(true), ":8080")
	out := buf.String()
	for _, want := range []string{"\x1b[32mGET\x1b[0m", "\x1b[36mPOST\x1b[0m", "\x1b[31mDELETE\x1b[0m", "\x1b[32m✓\x1b[0m"} {
		if !strings.Contains(out, want) {
			t.Errorf("colored banner lacks %q", want)
		}
	}
}

func TestBannerMode(t *testing.T) {
	tests := []struct {
		value   string
		want    bannerMode
		wantErr bool
	}{
		{value: "", want: bannerAuto},
		{value: "  ", want: bannerAuto},
		{value: "true", want: bannerOn},
		{value: "1", want: bannerOn},
		{value: "false", want: bannerOff},
		{value: "0", want: bannerOff},
		{value: "yes", wantErr: true},
		{value: "off", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("COREGO_BANNER", tt.value)
			core, err := New(nil)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "COREGO_BANNER") {
					core.Close()
					t.Fatalf("New error = %v, want one naming COREGO_BANNER", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer core.Close()
			if core.banner != tt.want {
				t.Fatalf("banner mode = %v, want %v", core.banner, tt.want)
			}
		})
	}
}

func TestShowBanner(t *testing.T) {
	isolateEnv(t)
	t.Setenv("COREGO_BANNER", "true")
	if !newCore(t, nil).showBanner() {
		t.Error("COREGO_BANNER=true must show the banner")
	}
	t.Setenv("COREGO_BANNER", "false")
	if newCore(t, nil).showBanner() {
		t.Error("COREGO_BANNER=false must hide the banner")
	}
	t.Setenv("COREGO_BANNER", "true")
	if newCore(t, &Config{Server: ServerOptions{DisableBanner: true}}).showBanner() {
		t.Error("ServerOptions.DisableBanner must win over COREGO_BANNER=true")
	}
}

func TestURLHosts(t *testing.T) {
	tests := map[string]string{
		"mongodb://localhost:27017":                              "localhost:27017",
		"mongodb://localhost:27017/myapp":                        "localhost:27017",
		"mongodb://user:s3cret@db.internal:27017/app?ssl=true":   "db.internal:27017",
		"mongodb://u:p@a:27017,b:27017,c:27017/app?replicaSet=r": "a:27017,b:27017,c:27017",
		"mongodb+srv://u:p@cluster0.abc.mongodb.net/app":         "cluster0.abc.mongodb.net",
		"mongodb://us%40er:p%3Aw@host":                           "host",
		"postgres://user:p@ss@word@localhost:5432/db":            "localhost:5432",
		"localhost:27017":                                        "localhost:27017",
		"":                                                       "",
	}
	for raw, want := range tests {
		if got := urlHosts(raw); got != want {
			t.Errorf("urlHosts(%q) = %q, want %q", raw, got, want)
		}
		if strings.Contains(urlHosts(raw), "s3cret") {
			t.Errorf("urlHosts(%q) leaks the password", raw)
		}
	}
}

func TestDatabaseTarget(t *testing.T) {
	tests := []struct{ db, host, want string }{
		{"myapp", "localhost:27017", "myapp on localhost:27017"},
		{"", "localhost:27017", "localhost:27017"},
		{"myapp", "", "myapp"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := databaseTarget(tt.db, tt.host); got != tt.want {
			t.Errorf("databaseTarget(%q, %q) = %q, want %q", tt.db, tt.host, got, tt.want)
		}
	}
}

func TestSortRoutes(t *testing.T) {
	routes := []web.Route{
		{Method: "PROPFIND", Path: "/a"},
		{Method: http.MethodDelete, Path: "/a"},
		{Method: http.MethodGet, Path: "/b"},
		{Method: http.MethodPost, Path: "/a"},
		{Method: http.MethodGet, Path: "/a"},
	}
	sortRoutes(routes)
	got := ""
	for _, r := range routes {
		got += r.Method + " " + r.Path + ";"
	}
	if want := "GET /a;POST /a;DELETE /a;PROPFIND /a;GET /b;"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

// assertOrder fails unless every part occurs in s, in the given order.
func assertOrder(t *testing.T, s string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			t.Fatalf("%q not found after offset %d in:\n%s", p, at, s)
		}
		at += i + len(p)
	}
}

// lineHas reports whether some route line for exactly path (the second
// column) also contains want.
func lineHas(s, path, want string) bool {
	found := false
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			found = true
			if strings.Contains(line, want) {
				return true
			}
		}
	}
	if !found {
		panic("lineHas: no route line for " + path)
	}
	return false
}
