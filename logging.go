package corego

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/internal/console"
)

// resolveLogger returns the logger the facade uses: the configured one,
// otherwise the colored console logger when standard error is a
// terminal (see console.ColorEnabled) and nobody replaced slog's default
// handler, otherwise slog.Default().
func resolveLogger(configured *slog.Logger, e *env.Env) *slog.Logger {
	if configured != nil {
		return configured
	}
	if !usesBuiltinDefaultHandler() {
		// The application installed its own default handler (JSON, a
		// log shipper, ...); respect it.
		return slog.Default()
	}
	if !console.ColorEnabled(os.Stderr, envLookup(e)) {
		return slog.Default()
	}
	return slog.New(console.NewHandler(os.Stderr, console.HandlerOptions{Color: true}))
}

// usesBuiltinDefaultHandler reports whether slog.Default() still uses
// the handler slog starts with, i.e. the application has not called
// slog.SetDefault. slog does not export that handler type, so its name
// is compared.
func usesBuiltinDefaultHandler() bool {
	return fmt.Sprintf("%T", slog.Default().Handler()) == "*slog.defaultHandler"
}

// envLookup returns a getenv function over the loaded environment, so
// NO_COLOR and friends may also come from .env.
func envLookup(e *env.Env) func(string) string {
	if e == nil {
		return os.Getenv
	}
	return func(key string) string {
		v, _ := e.Get(key)
		return v
	}
}
