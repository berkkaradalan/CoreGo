package console

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Handler is a slog.Handler for people reading a terminal. Each record
// is one line:
//
//	17:37:11 INF corego: auth ready store=mongo base_path=/auth
//
// The level is colored, attribute keys are dimmed, an attribute named
// "status" holding an HTTP status code is colored by class and one named
// "error" is red. Values containing spaces or quotes are quoted. Without
// colors the output is the same text, so it stays readable in files.
type Handler struct {
	opts    HandlerOptions
	palette Palette
	attrs   []slog.Attr // from WithAttrs, already prefixed with groups
	group   string      // dotted group prefix from WithGroup

	mu *sync.Mutex
	w  io.Writer
}

// HandlerOptions configures a Handler.
type HandlerOptions struct {
	// Level is the minimum level logged; nil means slog.LevelInfo.
	Level slog.Leveler
	// Color enables ANSI colors.
	Color bool
}

// NewHandler returns a Handler that writes to w.
func NewHandler(w io.Writer, opts HandlerOptions) *Handler {
	return &Handler{opts: opts, palette: NewPalette(opts.Color), mu: &sync.Mutex{}, w: w}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	min := slog.LevelInfo
	if h.opts.Level != nil {
		min = h.opts.Level.Level()
	}
	return level >= min
}

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	if !r.Time.IsZero() {
		b.WriteString(h.palette.Dim(r.Time.Format(time.TimeOnly)))
		b.WriteByte(' ')
	}
	b.WriteString(h.level(r.Level))
	b.WriteByte(' ')
	b.WriteString(r.Message)

	for _, a := range h.attrs {
		h.writeAttr(&b, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		h.writeAttr(&b, h.group, a)
		return true
	})
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		if h.group != "" {
			a.Key = h.group + a.Key
		}
		clone.attrs = append(clone.attrs, a)
	}
	return &clone
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.group = h.group + name + "."
	return &clone
}

// level renders the three-letter level tag.
func (h *Handler) level(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return h.palette.Bold(h.palette.Red("ERR"))
	case l >= slog.LevelWarn:
		return h.palette.Yellow("WRN")
	case l >= slog.LevelInfo:
		return h.palette.Cyan("INF")
	default:
		return h.palette.Dim("DBG")
	}
}

// writeAttr appends " key=value", flattening groups into dotted keys.
func (h *Handler) writeAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		group := prefix
		if a.Key != "" {
			group += a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			h.writeAttr(b, group, ga)
		}
		return
	}
	key := prefix + a.Key
	b.WriteByte(' ')
	b.WriteString(h.palette.Dim(key + "="))
	b.WriteString(h.value(a.Key, a.Value))
}

// value renders an attribute value, coloring the well-known ones.
func (h *Handler) value(key string, v slog.Value) string {
	switch {
	case key == "status" && v.Kind() == slog.KindInt64:
		return h.palette.Status(v.Int64())
	case key == "status" && v.Kind() == slog.KindUint64 && v.Uint64() < 1000:
		return h.palette.Status(int64(v.Uint64()))
	case key == "error":
		return h.palette.Red(quote(v.String()))
	case v.Kind() == slog.KindDuration:
		return v.Duration().String()
	case v.Kind() == slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return quote(fmt.Sprint(v.Any()))
	}
}

// quote returns s as is when it is a simple token, and Go-quoted when it
// is empty or contains spaces, quotes, '=' or control characters.
func quote(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r <= ' ' || r == '"' || r == '=' || r == 0x7f {
			return strconv.Quote(s)
		}
	}
	return s
}
