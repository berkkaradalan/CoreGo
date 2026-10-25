package console

import (
	"os"
	"path/filepath"
	"testing"
)

// envMap returns a getenv function over a fixed map.
func envMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestColorEnabled(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	tests := []struct {
		name string
		file *os.File
		env  map[string]string
		want bool
	}{
		{name: "regular file", file: file, want: false},
		{name: "nil file", file: nil, want: false},
		{name: "FORCE_COLOR on a file", file: file, env: map[string]string{"FORCE_COLOR": "1"}, want: true},
		{name: "NO_COLOR beats FORCE_COLOR", file: file, env: map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"}, want: false},
		{name: "empty NO_COLOR is ignored", file: file, env: map[string]string{"NO_COLOR": "", "FORCE_COLOR": "1"}, want: true},
		{name: "TERM=dumb", file: file, env: map[string]string{"TERM": "dumb"}, want: false},
		{name: "FORCE_COLOR beats TERM=dumb", file: file, env: map[string]string{"TERM": "DUMB", "FORCE_COLOR": "true"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ColorEnabled(tt.file, envMap(tt.env)); got != tt.want {
				t.Fatalf("ColorEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if IsTerminal(file) {
		t.Error("a regular file is not a terminal")
	}
	if IsTerminal(nil) {
		t.Error("nil is not a terminal")
	}
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed.log"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if IsTerminal(closed) {
		t.Error("a closed file is not a terminal")
	}
}

func TestPalette(t *testing.T) {
	off := NewPalette(false)
	on := NewPalette(true)
	if off.Enabled() || !on.Enabled() {
		t.Fatal("Enabled does not reflect the constructor argument")
	}

	styles := map[string]struct {
		fn   func(Palette, string) string
		code string
	}{
		"Bold":    {Palette.Bold, bold},
		"Dim":     {Palette.Dim, dim},
		"Red":     {Palette.Red, red},
		"Green":   {Palette.Green, green},
		"Yellow":  {Palette.Yellow, yellow},
		"Blue":    {Palette.Blue, blue},
		"Magenta": {Palette.Magenta, magenta},
		"Cyan":    {Palette.Cyan, cyan},
	}
	for name, s := range styles {
		t.Run(name, func(t *testing.T) {
			if got := s.fn(off, "x"); got != "x" {
				t.Errorf("disabled = %q, want plain text", got)
			}
			if got := s.fn(on, "x"); got != s.code+"x"+reset {
				t.Errorf("enabled = %q, want %q", got, s.code+"x"+reset)
			}
			if got := s.fn(on, ""); got != "" {
				t.Errorf("empty string = %q, want no escape codes", got)
			}
		})
	}
}

func TestPaletteMethod(t *testing.T) {
	p := NewPalette(true)
	tests := map[string]string{
		"GET": green, "HEAD": green, "POST": cyan, "PUT": yellow,
		"PATCH": yellow, "DELETE": red, "OPTIONS": magenta, "PROPFIND": magenta,
	}
	for method, code := range tests {
		if got := p.Method(method); got != code+method+reset {
			t.Errorf("Method(%q) = %q, want color %q", method, got, code)
		}
	}
	if got := NewPalette(false).Method("GET"); got != "GET" {
		t.Errorf("disabled Method = %q", got)
	}
}

func TestPaletteStatus(t *testing.T) {
	p := NewPalette(true)
	tests := []struct {
		code int64
		want string
	}{
		{100, "100"},
		{200, green + "200" + reset},
		{204, green + "204" + reset},
		{301, cyan + "301" + reset},
		{404, yellow + "404" + reset},
		{429, yellow + "429" + reset},
		{500, red + "500" + reset},
		{503, red + "503" + reset},
	}
	for _, tt := range tests {
		if got := p.Status(tt.code); got != tt.want {
			t.Errorf("Status(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
	if got := NewPalette(false).Status(500); got != "500" {
		t.Errorf("disabled Status = %q", got)
	}
}
