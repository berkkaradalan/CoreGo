package corego

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/mail"
)

func TestSMTPConfigFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantOK      bool
		want        mail.SMTPConfig
		wantWarning bool
	}{
		{
			name:   "no host disables SMTP",
			env:    map[string]string{"SMTP_PORT": "465", "SMTP_USERNAME": "user"},
			wantOK: false,
		},
		{
			name:   "whitespace host disables SMTP",
			env:    map[string]string{"SMTP_HOST": "   "},
			wantOK: false,
		},
		{
			name:   "host only uses port 587 with STARTTLS",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com"},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
		},
		{
			name: "every variable",
			env: map[string]string{
				"SMTP_HOST":     " smtp.example.com ",
				"SMTP_PORT":     "2525",
				"SMTP_USERNAME": "no-reply@example.com",
				"SMTP_PASSWORD": "app-password",
				"SMTP_FROM":     "My App <no-reply@example.com>",
			},
			wantOK: true,
			want: mail.SMTPConfig{
				Host: "smtp.example.com", Port: 2525,
				Username: "no-reply@example.com", Password: "app-password",
				From: "My App <no-reply@example.com>", TLSMode: mail.TLSStartTLS,
			},
		},
		{
			name:   "port 465 selects implicit TLS",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "465"},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 465, TLSMode: mail.TLSImplicit},
		},
		{
			name:   "port 587 explicitly",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "587"},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
		},
		{
			name:   "port 25 uses STARTTLS",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "25"},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 25, TLSMode: mail.TLSStartTLS},
		},
		{
			name:   "port with surrounding whitespace",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": " 465 "},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 465, TLSMode: mail.TLSImplicit},
		},
		{
			name:   "empty port uses the default silently",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": ""},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
		},
		{
			name:        "non-numeric port falls back to 587",
			env:         map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "smtps"},
			wantOK:      true,
			want:        mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
			wantWarning: true,
		},
		{
			name:        "zero port falls back to 587",
			env:         map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "0"},
			wantOK:      true,
			want:        mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
			wantWarning: true,
		},
		{
			name:        "negative port falls back to 587",
			env:         map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "-465"},
			wantOK:      true,
			want:        mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
			wantWarning: true,
		},
		{
			name:        "port above 65535 falls back to 587",
			env:         map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "65536"},
			wantOK:      true,
			want:        mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
			wantWarning: true,
		},
		{
			name:        "fractional port falls back to 587",
			env:         map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "465.0"},
			wantOK:      true,
			want:        mail.SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: mail.TLSStartTLS},
			wantWarning: true,
		},
		{
			name:   "highest valid port",
			env:    map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "65535"},
			wantOK: true,
			want:   mail.SMTPConfig{Host: "smtp.example.com", Port: 65535, TLSMode: mail.TLSStartTLS},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			e, err := env.Load()
			if err != nil {
				t.Fatal(err)
			}
			logger, logs := newTestLogger()

			got, ok := smtpConfigFromEnv(e, logger)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("config = %+v, want %+v", got, tt.want)
			}
			gotWarning := strings.Contains(logs.String(), "ignoring invalid SMTP_PORT")
			if gotWarning != tt.wantWarning {
				t.Errorf("warning logged = %v, want %v; logs:\n%s", gotWarning, tt.wantWarning, logs)
			}
			if tt.wantWarning && !strings.Contains(logs.String(), "port=587") {
				t.Errorf("warning does not name the fallback port; logs:\n%s", logs)
			}
		})
	}
}

// TestSMTPMailerWiredFromEnv proves the documented zero-code composition
// end to end: SMTP_* variables -> SMTPSender -> DefaultMailer -> auth
// service. A fake server that does not offer STARTTLS receives the
// connection; the sender then refuses to continue in plaintext, and the
// failure reaches Config.Logger through the auth service.
func TestSMTPMailerWiredFromEnv(t *testing.T) {
	isolateEnv(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	greetings := make(chan string, 1)
	go fakeSMTPWithoutSTARTTLS(ln, greetings)

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_FROM", "no-reply@example.com")
	logger, logs := newTestLogger()

	core := newCore(t, &Config{Auth: &AuthOptions{}, Logger: logger})
	if strings.Contains(logs.String(), "auth emails are disabled") {
		t.Fatalf("no-mailer warning logged although SMTP_HOST is set; logs:\n%s", logs)
	}

	// SignUp sends the verification email in the background.
	if _, err := core.Auth.SignUp(t.Context(), auth.SignUpInput{
		Name: "Ada", Email: "ada@example.com", Password: "analytical-engine",
	}); err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	if err := core.Auth.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case ehlo := <-greetings:
		if !strings.HasPrefix(ehlo, "EHLO ") {
			t.Errorf("first client command = %q, want EHLO (STARTTLS mode waits for the greeting)", ehlo)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the facade's mailer never connected to SMTP_HOST:SMTP_PORT")
	}
	if missing := missingSubstrings(logs.String(), "level=ERROR", "STARTTLS"); len(missing) > 0 {
		t.Errorf("delivery failure not logged through Config.Logger (missing %q); logs:\n%s", missing, logs)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs leak the SMTP password:\n%s", logs)
	}
}

// fakeSMTPWithoutSTARTTLS accepts one connection, greets, reports the
// client's first command and answers EHLO without advertising STARTTLS.
func fakeSMTPWithoutSTARTTLS(ln net.Listener, firstCommand chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	if _, err := conn.Write([]byte("220 fake.test ESMTP\r\n")); err != nil {
		return
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	firstCommand <- strings.TrimSpace(line)
	_, _ = conn.Write([]byte("250-fake.test\r\n250 AUTH PLAIN\r\n"))
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(strings.ToUpper(line), "QUIT") {
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		}
		_, _ = conn.Write([]byte("502 not implemented\r\n"))
	}
}
