package env

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// knownKeys lists every variable with a typed Env field (plus the
// legacy URL fallback). Tests unset them so the developer's shell never
// influences the results.
var knownKeys = []string{
	"APP_URL", "URL", "PORT",
	"MONGODB_CONNECTION_URL", "MONGODB_DATABASE", "POSTGRES_CONNECTION_URL",
}

// unsetEnv removes keys from the process environment for the duration
// of the test. t.Setenv registers the restore of the original value;
// the following Unsetenv then makes the variable genuinely unset, which
// differs from set-but-empty.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// inTempDir switches the working directory to a fresh temporary
// directory for the duration of the test, optionally containing a .env
// file with the given content (no file when content is nil).
func inTempDir(t *testing.T, content *string) string {
	t.Helper()
	dir := t.TempDir()
	if content != nil {
		writeFile(t, filepath.Join(dir, DefaultFile), *content)
	}
	t.Chdir(dir)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func ptr(s string) *string { return &s }

func mustLoad(t *testing.T) *Env {
	t.Helper()
	e, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return e
}

func TestLoadDefaults(t *testing.T) {
	unsetEnv(t, knownKeys...)
	inTempDir(t, nil)

	e := mustLoad(t)

	want := Env{AppURL: "http://localhost:8080", Port: "8080"}
	got := Env{
		AppURL: e.AppURL, Port: e.Port, MongoURL: e.MongoURL,
		MongoDatabase: e.MongoDatabase, PostgresURL: e.PostgresURL,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed fields = %+v, want %+v", got, want)
	}
}

func TestLoadTypedFields(t *testing.T) {
	tests := []struct {
		name    string
		process map[string]string // set in the process environment
		dotenv  *string           // .env content; nil = no file
		want    Env
	}{
		{
			name: "everything from the process environment",
			process: map[string]string{
				"APP_URL":                 "https://api.example.com",
				"PORT":                    "9000",
				"MONGODB_CONNECTION_URL":  "mongodb://mongo:27017",
				"MONGODB_DATABASE":        "app",
				"POSTGRES_CONNECTION_URL": "postgres://pg:5432/app",
			},
			want: Env{
				AppURL: "https://api.example.com", Port: "9000",
				MongoURL: "mongodb://mongo:27017", MongoDatabase: "app",
				PostgresURL: "postgres://pg:5432/app",
			},
		},
		{
			name: "everything from .env",
			dotenv: ptr("APP_URL=https://api.example.com\nPORT=9000\n" +
				"MONGODB_CONNECTION_URL=mongodb://mongo:27017\nMONGODB_DATABASE=app\n" +
				"POSTGRES_CONNECTION_URL=postgres://pg:5432/app\n"),
			want: Env{
				AppURL: "https://api.example.com", Port: "9000",
				MongoURL: "mongodb://mongo:27017", MongoDatabase: "app",
				PostgresURL: "postgres://pg:5432/app",
			},
		},
		{
			name:    "default app url follows a custom port",
			process: map[string]string{"PORT": "3000"},
			want:    Env{AppURL: "http://localhost:3000", Port: "3000"},
		},
		{
			name:    "legacy URL is used when APP_URL is unset",
			process: map[string]string{"URL": "https://legacy.example.com"},
			want:    Env{AppURL: "https://legacy.example.com", Port: "8080"},
		},
		{
			name:    "APP_URL wins over legacy URL",
			process: map[string]string{"APP_URL": "https://new.example.com", "URL": "https://legacy.example.com"},
			want:    Env{AppURL: "https://new.example.com", Port: "8080"},
		},
		{
			name:    "empty APP_URL falls back to legacy URL",
			process: map[string]string{"APP_URL": "", "URL": "https://legacy.example.com"},
			want:    Env{AppURL: "https://legacy.example.com", Port: "8080"},
		},
		{
			name:    "trailing slashes are removed from the app url",
			process: map[string]string{"APP_URL": "https://example.com/base//"},
			want:    Env{AppURL: "https://example.com/base", Port: "8080"},
		},
		{
			name:    "app url made only of slashes falls back to the default",
			process: map[string]string{"APP_URL": "/"},
			want:    Env{AppURL: "http://localhost:8080", Port: "8080"},
		},
		{
			name:    "surrounding whitespace is trimmed",
			process: map[string]string{"PORT": " 7000 ", "APP_URL": "  https://example.com/ ", "MONGODB_DATABASE": " app "},
			want:    Env{AppURL: "https://example.com", Port: "7000", MongoDatabase: "app"},
		},
		{
			name:    "whitespace-only port uses the default",
			process: map[string]string{"PORT": "   "},
			want:    Env{AppURL: "http://localhost:8080", Port: "8080"},
		},
		{
			name:    "empty connection urls mean not configured",
			process: map[string]string{"MONGODB_CONNECTION_URL": "", "POSTGRES_CONNECTION_URL": ""},
			want:    Env{AppURL: "http://localhost:8080", Port: "8080"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, knownKeys...)
			for k, v := range tt.process {
				t.Setenv(k, v)
			}
			inTempDir(t, tt.dotenv)

			e := mustLoad(t)
			got := Env{
				AppURL: e.AppURL, Port: e.Port, MongoURL: e.MongoURL,
				MongoDatabase: e.MongoDatabase, PostgresURL: e.PostgresURL,
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("typed fields = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadPrecedence(t *testing.T) {
	unsetEnv(t, knownKeys...)
	unsetEnv(t, "COREGO_TEST_FILE_ONLY")
	t.Setenv("PORT", "7000")
	t.Setenv("COREGO_TEST_EMPTY", "")
	inTempDir(t, ptr("PORT=4000\nCOREGO_TEST_EMPTY=from-file\nCOREGO_TEST_FILE_ONLY=file\n"))

	e := mustLoad(t)

	if e.Port != "7000" {
		t.Errorf("Port = %q, want %q (process environment must win)", e.Port, "7000")
	}
	if v, ok := e.Get("COREGO_TEST_EMPTY"); !ok || v != "" {
		t.Errorf(`Get(COREGO_TEST_EMPTY) = %q, %v; want "", true (empty-but-set process variable must win)`, v, ok)
	}
	if v, ok := e.Get("COREGO_TEST_FILE_ONLY"); !ok || v != "file" {
		t.Errorf("Get(COREGO_TEST_FILE_ONLY) = %q, %v; want %q, true (.env fills gaps)", v, ok, "file")
	}
}

func TestLoadEmptyProcessPortBlocksDotenv(t *testing.T) {
	// An empty-but-set PORT wins over .env, so the typed field falls
	// back to its default rather than to the .env value.
	unsetEnv(t, knownKeys...)
	t.Setenv("PORT", "")
	inTempDir(t, ptr("PORT=4000\n"))

	if got := mustLoad(t).Port; got != DefaultPort {
		t.Errorf("Port = %q, want %q", got, DefaultPort)
	}
}

func TestLoadDoesNotMutateProcessEnvironment(t *testing.T) {
	unsetEnv(t, "COREGO_TEST_FILE_ONLY")
	inTempDir(t, ptr("COREGO_TEST_FILE_ONLY=file\n"))

	e := mustLoad(t)

	if _, ok := e.Get("COREGO_TEST_FILE_ONLY"); !ok {
		t.Fatal("Get(COREGO_TEST_FILE_ONLY) reported unset, want set from .env")
	}
	if v, ok := os.LookupEnv("COREGO_TEST_FILE_ONLY"); ok {
		t.Errorf("process environment was mutated: COREGO_TEST_FILE_ONLY=%q", v)
	}
}

func TestLoadMissingFileIsSilent(t *testing.T) {
	inTempDir(t, nil)

	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(&buf)
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	if _, err := Load(); err != nil {
		t.Fatalf("Load() without .env error = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Load() without .env logged %q, want no output", buf.String())
	}
}

func TestLoadMalformedFile(t *testing.T) {
	const secret = "s3cr3t-value"
	tests := []struct {
		name     string
		content  string
		wantLine string
		wantDesc string
	}{
		{
			name:     "invalid character in name",
			content:  "PORT=1\nBAD-NAME=" + secret + "\n",
			wantLine: "line 2:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "bare word on a line before another variable",
			content:  "PORT=1\nOOPS\nTOKEN=" + secret + "\n",
			wantLine: "line 2:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "bare word on the last line",
			content:  "TOKEN=" + secret + "\nOOPS",
			wantLine: "line 2:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "missing name",
			content:  "PORT=1\n=" + secret + "\n",
			wantLine: "line 2:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "whitespace inside name",
			content:  "MY VAR=" + secret + "\n",
			wantLine: "line 1:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "unterminated quote",
			content:  "PORT=1\n# comment\nTOKEN=\"" + secret + "\nOTHER=2\n",
			wantLine: "line 3:",
			wantDesc: "unterminated quoted value",
		},
		{
			name:     "error after a valid multi-line quoted value",
			content:  "KEY=\"line one\nline two\"\nPORT=1\nBAD-NAME=" + secret + "\n",
			wantLine: "line 4:",
			wantDesc: "invalid variable name",
		},
		{
			name:     "windows line endings",
			content:  "PORT=1\r\nBAD-NAME=" + secret + "\r\n",
			wantLine: "line 2:",
			wantDesc: "invalid variable name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inTempDir(t, ptr(tt.content))

			e, err := Load()
			if err == nil {
				t.Fatalf("Load() = %+v, nil; want parse error", e)
			}
			if e != nil {
				t.Errorf("Load() returned a non-nil Env alongside the error")
			}
			msg := err.Error()
			for _, want := range []string{"env: parse .env:", tt.wantLine, tt.wantDesc} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			if strings.Contains(msg, secret) {
				t.Errorf("error %q leaks file content", msg)
			}
		})
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	// A .env that exists but is not a readable file (here: a directory)
	// is an error, not a silently skipped "missing" file.
	dir := inTempDir(t, nil)
	if err := os.Mkdir(filepath.Join(dir, DefaultFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "env: read .env") {
		t.Errorf("Load() error = %v, want read error", err)
	}
}

func TestLoadFile(t *testing.T) {
	unsetEnv(t, knownKeys...)
	unsetEnv(t, "COREGO_TEST_CUSTOM")
	dir := inTempDir(t, ptr("PORT=1111\n"))
	path := filepath.Join(dir, "config", "app.env")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, path, "PORT=2222\nCOREGO_TEST_CUSTOM=custom\n")

	e, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if e.Port != "2222" {
		t.Errorf("Port = %q, want %q (LoadFile must read the given file, not ./.env)", e.Port, "2222")
	}
	if got := e.GetOrDefault("COREGO_TEST_CUSTOM", ""); got != "custom" {
		t.Errorf("COREGO_TEST_CUSTOM = %q, want %q", got, "custom")
	}

	t.Setenv("PORT", "3333")
	if e, err := LoadFile(path); err != nil || e.Port != "3333" {
		t.Errorf("LoadFile() Port = %v, %v; want 3333 (process environment must win)", e, err)
	}
}

func TestLoadFileErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadFile(filepath.Join(dir, "missing.env"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("LoadFile(missing) error = %v, want fs.ErrNotExist", err)
	}

	bad := filepath.Join(dir, "bad.env")
	writeFile(t, bad, "OK=1\nNOT VALID\n")
	if _, err := LoadFile(bad); err == nil || !strings.Contains(err.Error(), "line 2:") {
		t.Errorf("LoadFile(malformed) error = %v, want parse error on line 2", err)
	}
}

func TestDotenvSyntax(t *testing.T) {
	keys := []string{"PLAIN", "QUOTED", "SINGLE", "EXPORTED", "COMMENTED", "EXPANDED", "EMPTY", "MULTI", "NO_PROCESS_EXPANSION"}
	unsetEnv(t, keys...)
	// Expansion only sees variables defined earlier in the same file,
	// never the process environment.
	t.Setenv("COREGO_TEST_PROCESS_ONLY", "process")
	inTempDir(t, ptr(strings.Join([]string{
		"# full line comment",
		"PLAIN=value",
		`QUOTED="hello world"`,
		`SINGLE='a $literal'`,
		"export EXPORTED=yes",
		"COMMENTED=kept # trailing comment",
		"EXPANDED=${PLAIN}-suffix",
		"EMPTY=",
		`MULTI="first\nsecond"`,
		"NO_PROCESS_EXPANSION=${COREGO_TEST_PROCESS_ONLY}",
		"",
	}, "\n")))

	e := mustLoad(t)
	want := map[string]string{
		"PLAIN":     "value",
		"QUOTED":    "hello world",
		"SINGLE":    "a $literal",
		"EXPORTED":  "yes",
		"COMMENTED": "kept",
		"EXPANDED":  "value-suffix",
		"EMPTY":     "",
		"MULTI":     "first\nsecond",

		"NO_PROCESS_EXPANSION": "",
	}
	for _, key := range keys {
		got, ok := e.Get(key)
		if !ok || got != want[key] {
			t.Errorf("Get(%s) = %q, %v; want %q, true", key, got, ok, want[key])
		}
	}
}

func TestGetAndGetOrDefault(t *testing.T) {
	t.Setenv("COREGO_TEST_PRESENT", "value")
	t.Setenv("COREGO_TEST_PADDED", "  padded  ")
	t.Setenv("COREGO_TEST_EMPTY", "")
	t.Setenv("COREGO_TEST_BLANK", "   ")
	unsetEnv(t, "COREGO_TEST_MISSING")
	inTempDir(t, nil)
	e := mustLoad(t)

	getTests := []struct {
		key    string
		want   string
		wantOK bool
	}{
		{"COREGO_TEST_PRESENT", "value", true},
		{"COREGO_TEST_PADDED", "  padded  ", true}, // Get returns the raw value
		{"COREGO_TEST_EMPTY", "", true},
		{"COREGO_TEST_MISSING", "", false},
	}
	for _, tt := range getTests {
		if got, ok := e.Get(tt.key); got != tt.want || ok != tt.wantOK {
			t.Errorf("Get(%s) = %q, %v; want %q, %v", tt.key, got, ok, tt.want, tt.wantOK)
		}
	}

	defaultTests := []struct {
		key, want string
	}{
		{"COREGO_TEST_PRESENT", "value"},
		{"COREGO_TEST_PADDED", "padded"},
		{"COREGO_TEST_EMPTY", "default"},
		{"COREGO_TEST_BLANK", "default"},
		{"COREGO_TEST_MISSING", "default"},
	}
	for _, tt := range defaultTests {
		if got := e.GetOrDefault(tt.key, "default"); got != tt.want {
			t.Errorf("GetOrDefault(%s) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// envWith builds an Env from a literal variable map, bypassing the
// process environment, for getter tests.
func envWith(vars map[string]string) *Env {
	return newEnv(vars)
}

func TestInt(t *testing.T) {
	tests := []struct {
		name  string
		value *string // nil = unset
		def   int
		want  int
	}{
		{"unset", nil, 7, 7},
		{"empty", ptr(""), 7, 7},
		{"valid", ptr("42"), 7, 42},
		{"negative", ptr("-3"), 7, -3},
		{"zero", ptr("0"), 7, 0},
		{"padded", ptr(" 12 "), 7, 12},
		{"explicit plus", ptr("+5"), 7, 5},
		{"not a number", ptr("abc"), 7, 7},
		{"float", ptr("1.5"), 7, 7},
		{"trailing garbage", ptr("10s"), 7, 7},
		{"overflow", ptr("99999999999999999999999"), 7, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{}
			if tt.value != nil {
				vars["N"] = *tt.value
			}
			if got := envWith(vars).Int("N", tt.def); got != tt.want {
				t.Errorf("Int() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name  string
		value *string
		def   bool
		want  bool
	}{
		{"unset keeps default true", nil, true, true},
		{"unset keeps default false", nil, false, false},
		{"empty", ptr(""), true, true},
		{"true", ptr("true"), false, true},
		{"TRUE", ptr("TRUE"), false, true},
		{"1", ptr("1"), false, true},
		{"t", ptr("t"), false, true},
		{"false", ptr("false"), true, false},
		{"0", ptr("0"), true, false},
		{"padded", ptr(" true "), false, true},
		{"yes is invalid", ptr("yes"), false, false},
		{"on is invalid", ptr("on"), true, true},
		{"garbage", ptr("maybe"), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{}
			if tt.value != nil {
				vars["B"] = *tt.value
			}
			if got := envWith(vars).Bool("B", tt.def); got != tt.want {
				t.Errorf("Bool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	const def = 5 * time.Second
	tests := []struct {
		name  string
		value *string
		want  time.Duration
	}{
		{"unset", nil, def},
		{"empty", ptr(""), def},
		{"seconds", ptr("30s"), 30 * time.Second},
		{"compound", ptr("1h30m"), 90 * time.Minute},
		{"milliseconds", ptr("250ms"), 250 * time.Millisecond},
		{"zero", ptr("0"), 0},
		{"negative", ptr("-1m"), -time.Minute},
		{"padded", ptr(" 2m "), 2 * time.Minute},
		{"bare number has no unit", ptr("30"), def},
		{"garbage", ptr("soon"), def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{}
			if tt.value != nil {
				vars["D"] = *tt.value
			}
			if got := envWith(vars).Duration("D", def); got != tt.want {
				t.Errorf("Duration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStrings(t *testing.T) {
	tests := []struct {
		name  string
		value *string
		want  []string
	}{
		{"unset", nil, nil},
		{"empty", ptr(""), nil},
		{"only separators and spaces", ptr(" , ,, "), nil},
		{"single", ptr("https://a.example"), []string{"https://a.example"}},
		{"several", ptr("a,b,c"), []string{"a", "b", "c"}},
		{"trimmed", ptr("  a ,\tb  , c\n"), []string{"a", "b", "c"}},
		{"empties dropped", ptr("a,,b,"), []string{"a", "b"}},
		{"inner spaces kept", ptr("hello world, x"), []string{"hello world", "x"}},
		{"duplicates kept", ptr("a,a"), []string{"a", "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{}
			if tt.value != nil {
				vars["S"] = *tt.value
			}
			if got := envWith(vars).Strings("S"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Strings() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSnapshotImmutability(t *testing.T) {
	unsetEnv(t, "COREGO_TEST_LATE", "COREGO_TEST_FILE")
	t.Setenv("COREGO_TEST_LIST", "a,b")
	dir := inTempDir(t, ptr("COREGO_TEST_FILE=before\n"))

	e := mustLoad(t)

	t.Setenv("COREGO_TEST_LATE", "after")
	t.Setenv("COREGO_TEST_LIST", "x,y")
	writeFile(t, filepath.Join(dir, DefaultFile), "COREGO_TEST_FILE=after\n")

	if _, ok := e.Get("COREGO_TEST_LATE"); ok {
		t.Error("variable set after Load is visible; Env must be a snapshot")
	}
	if got := e.GetOrDefault("COREGO_TEST_FILE", ""); got != "before" {
		t.Errorf("COREGO_TEST_FILE = %q, want %q (.env edits after Load must be invisible)", got, "before")
	}

	list := e.Strings("COREGO_TEST_LIST")
	list[0] = "mutated"
	if got := e.Strings("COREGO_TEST_LIST"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Strings() = %v after mutating a previous result, want [a b]", got)
	}
}

func TestZeroEnvIsUsable(t *testing.T) {
	var e Env
	if _, ok := e.Get("ANY"); ok {
		t.Error("zero Env reported a variable as set")
	}
	if got := e.GetOrDefault("ANY", "d"); got != "d" {
		t.Errorf("GetOrDefault() = %q, want %q", got, "d")
	}
	if got := e.Int("ANY", 3); got != 3 {
		t.Errorf("Int() = %d, want 3", got)
	}
	if got := e.Strings("ANY"); got != nil {
		t.Errorf("Strings() = %v, want nil", got)
	}
}

func TestConcurrentReads(t *testing.T) {
	t.Setenv("COREGO_TEST_CONCURRENT", "1,2,3")
	inTempDir(t, nil)
	e := mustLoad(t)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = e.Get("COREGO_TEST_CONCURRENT")
				_ = e.Strings("COREGO_TEST_CONCURRENT")
				_ = e.Int("PORT", 0)
			}
		}()
	}
	wg.Wait()
}

// TestEnvExampleParses keeps the repository's .env.example template
// valid, both as shipped and with every example variable uncommented.
func TestEnvExampleParses(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if _, err := parseDotenv(content); err != nil {
		t.Fatalf(".env.example as shipped does not parse: %v", err)
	}

	commentedVar := regexp.MustCompile(`(?m)^# ([A-Z][A-Z0-9_]*=)`)
	vars, err := parseDotenv(commentedVar.ReplaceAll(content, []byte("$1")))
	if err != nil {
		t.Fatalf(".env.example with every variable uncommented does not parse: %v", err)
	}
	for _, key := range []string{
		"APP_URL", "PORT", "MONGODB_CONNECTION_URL", "MONGODB_DATABASE", "POSTGRES_CONNECTION_URL",
		"COREGO_AUTH_STORE", "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
		"CORS_ALLOWED_ORIGINS", "TRUST_PROXY_HEADERS", "COREGO_RESET_PASSWORD_URL",
		"COREGO_VERIFY_EMAIL_REDIRECT_URL", "COREGO_BANNER", "NO_COLOR",
	} {
		if _, ok := vars[key]; !ok {
			t.Errorf(".env.example does not document %s", key)
		}
	}
	if got := vars["SMTP_FROM"]; got != "My App <no-reply@example.com>" {
		t.Errorf("SMTP_FROM = %q, want the quoted display-name example", got)
	}
}
