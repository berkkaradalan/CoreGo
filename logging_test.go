package corego

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/internal/console"
)

// loadEnv loads the environment of the current (isolated) test.
func loadEnv(t *testing.T) *env.Env {
	t.Helper()
	e, err := env.Load()
	if err != nil {
		t.Fatalf("env.Load: %v", err)
	}
	return e
}

func TestResolveLoggerConfiguredWins(t *testing.T) {
	isolateEnv(t)
	t.Setenv("FORCE_COLOR", "1")
	configured := slog.New(slog.NewTextHandler(io.Discard, nil))
	if got := resolveLogger(configured, loadEnv(t)); got != configured {
		t.Fatal("Config.Logger must be used as given")
	}
}

func TestResolveLoggerConsoleWhenColorsAreOn(t *testing.T) {
	isolateEnv(t)
	t.Setenv("FORCE_COLOR", "1")
	got := resolveLogger(nil, loadEnv(t))
	if _, ok := got.Handler().(*console.Handler); !ok {
		t.Fatalf("handler = %T, want *console.Handler", got.Handler())
	}
}

func TestResolveLoggerDefaultWithoutColors(t *testing.T) {
	isolateEnv(t)
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")
	if got := resolveLogger(nil, loadEnv(t)); got != slog.Default() {
		t.Fatal("NO_COLOR must keep slog.Default()")
	}

	// Without FORCE_COLOR and with standard error not a terminal (as
	// under go test), the default logger is kept as well.
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	if !console.IsTerminal(os.Stderr) {
		if got := resolveLogger(nil, loadEnv(t)); got != slog.Default() {
			t.Fatal("non-terminal output must keep slog.Default()")
		}
	}
}

func TestResolveLoggerRespectsCustomDefault(t *testing.T) {
	isolateEnv(t)
	t.Setenv("FORCE_COLOR", "1")
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })

	if !usesBuiltinDefaultHandler() {
		t.Fatal("tests must start with slog's built-in default handler")
	}
	custom := slog.New(slog.NewJSONHandler(io.Discard, nil))
	slog.SetDefault(custom)
	if usesBuiltinDefaultHandler() {
		t.Fatal("a replaced default handler must be detected")
	}
	if got := resolveLogger(nil, loadEnv(t)); got != custom {
		t.Fatal("an application-installed default logger must be respected")
	}
}

func TestEnvLookup(t *testing.T) {
	isolateEnv(t)
	t.Setenv("NO_COLOR", "1")
	if got := envLookup(loadEnv(t))("NO_COLOR"); got != "1" {
		t.Fatalf("envLookup = %q, want 1", got)
	}
	if got := envLookup(nil)("NO_COLOR"); got != "1" {
		t.Fatalf("envLookup(nil) = %q, want the process value", got)
	}
}

func TestNewUsesResolvedLogger(t *testing.T) {
	isolateEnv(t)
	logger, buf := newTestLogger()
	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	if core.logger() != logger {
		t.Fatal("Core must log through Config.Logger")
	}
	if buf.String() == "" {
		t.Fatal("startup logs must go to Config.Logger")
	}
}
