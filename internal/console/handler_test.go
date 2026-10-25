package console

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// at is the fixed time of the test records.
var at = time.Date(2026, 10, 25, 17, 37, 11, 0, time.Local)

// render handles one record and returns the written line.
func render(t *testing.T, h slog.Handler, level slog.Level, msg string, attrs ...slog.Attr) string {
	t.Helper()
	var buf bytes.Buffer
	if ch, ok := h.(*Handler); ok {
		ch.w = &buf
	}
	r := slog.NewRecord(at, level, msg, 0)
	r.AddAttrs(attrs...)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return buf.String()
}

func TestHandlerPlainLine(t *testing.T) {
	h := NewHandler(nil, HandlerOptions{})
	got := render(t, h, slog.LevelInfo, "corego: auth ready",
		slog.String("store", "mongo"), slog.Bool("cookie", false), slog.Int("status", 401))
	want := "17:37:11 INF corego: auth ready store=mongo cookie=false status=401\n"
	if got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestHandlerLevels(t *testing.T) {
	h := NewHandler(nil, HandlerOptions{Level: slog.LevelDebug})
	tests := map[slog.Level]string{
		slog.LevelDebug:     "DBG",
		slog.LevelInfo:      "INF",
		slog.LevelWarn:      "WRN",
		slog.LevelError:     "ERR",
		slog.LevelError + 4: "ERR",
	}
	for level, tag := range tests {
		if got := render(t, h, level, "m"); !strings.HasPrefix(got, "17:37:11 "+tag+" m") {
			t.Errorf("level %v rendered %q, want tag %s", level, got, tag)
		}
	}
}

func TestHandlerEnabled(t *testing.T) {
	ctx := context.Background()
	def := NewHandler(nil, HandlerOptions{})
	if def.Enabled(ctx, slog.LevelDebug) || !def.Enabled(ctx, slog.LevelInfo) {
		t.Error("the default level must be Info")
	}
	warn := NewHandler(nil, HandlerOptions{Level: slog.LevelWarn})
	if warn.Enabled(ctx, slog.LevelInfo) || !warn.Enabled(ctx, slog.LevelError) {
		t.Error("a Warn handler must drop Info and keep Error")
	}
}

func TestHandlerValues(t *testing.T) {
	h := NewHandler(nil, HandlerOptions{})
	tests := []struct {
		name string
		attr slog.Attr
		want string
	}{
		{"simple", slog.String("k", "value"), " k=value"},
		{"space is quoted", slog.String("k", "two words"), ` k="two words"`},
		{"empty is quoted", slog.String("k", ""), ` k=""`},
		{"equals is quoted", slog.String("k", "a=b"), ` k="a=b"`},
		{"quote is escaped", slog.String("k", `say "hi"`), ` k="say \"hi\""`},
		{"newline is escaped", slog.String("k", "a\nb"), ` k="a\nb"`},
		{"duration", slog.Duration("took", 1500*time.Millisecond), " took=1.5s"},
		{"time", slog.Time("at", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), " at=2026-01-02T03:04:05Z"},
		{"error", slog.Any("error", errors.New("boom now")), ` error="boom now"`},
		{"uint status", slog.Uint64("status", 204), " status=204"},
		{"string status is plain", slog.String("status", "ok"), " status=ok"},
		{"group", slog.Group("req", slog.String("id", "7"), slog.Int("n", 1)), " req.id=7 req.n=1"},
		{"empty attr is dropped", slog.Attr{}, ""},
		{"inline group", slog.Group("", slog.String("a", "1")), " a=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, h, slog.LevelInfo, "m", tt.attr)
			want := "17:37:11 INF m" + tt.want + "\n"
			if got != want {
				t.Fatalf("line = %q, want %q", got, want)
			}
		})
	}
}

func TestHandlerColors(t *testing.T) {
	h := NewHandler(nil, HandlerOptions{Color: true})
	got := render(t, h, slog.LevelWarn, "careful",
		slog.Int("status", 503), slog.String("error", "down"), slog.String("path", "/x"))
	for _, want := range []string{
		dim + "17:37:11" + reset,
		yellow + "WRN" + reset,
		dim + "status=" + reset + red + "503" + reset,
		dim + "error=" + reset + red + "down" + reset,
		dim + "path=" + reset + "/x",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q lacks %q", got, want)
		}
	}
}

func TestHandlerWithAttrsAndGroups(t *testing.T) {
	base := NewHandler(nil, HandlerOptions{})
	h := base.WithAttrs([]slog.Attr{slog.String("app", "demo")}).
		WithGroup("req").
		WithAttrs([]slog.Attr{slog.String("id", "9")})
	got := render(t, h, slog.LevelInfo, "m", slog.String("path", "/x"))
	want := "17:37:11 INF m app=demo req.id=9 req.path=/x\n"
	if got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}

	// The parent handler is not affected by its children.
	if got := render(t, base, slog.LevelInfo, "m"); got != "17:37:11 INF m\n" {
		t.Fatalf("parent line = %q", got)
	}
	// Empty additions return the handler itself.
	if base.WithAttrs(nil) != slog.Handler(base) || base.WithGroup("") != slog.Handler(base) {
		t.Fatal("empty WithAttrs/WithGroup must return the receiver")
	}
}

func TestHandlerZeroTimeIsOmitted(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(&buf, HandlerOptions{})
	if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "INF m\n" {
		t.Fatalf("line = %q", buf.String())
	}
}

func TestHandlerConcurrentWritesStayWhole(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, HandlerOptions{}))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				logger.Info("line", "k", "v")
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 1000 {
		t.Fatalf("got %d lines, want 1000", len(lines))
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, " INF line k=v") {
			t.Fatalf("interleaved line %q", l)
		}
	}
}
