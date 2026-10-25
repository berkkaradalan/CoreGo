package corego

import (
	"log/slog"
	"strconv"

	"github.com/berkkaradalan/CoreGo/env"
	"github.com/berkkaradalan/CoreGo/mail"
)

// SMTP ports the facade knows: 587 (submission, upgraded with STARTTLS)
// is the default and 465 speaks TLS from the first byte.
const (
	defaultSMTPPort     = 587
	implicitTLSSMTPPort = 465
)

// smtpConfigFromEnv builds the SMTP configuration of the facade's mailer
// from SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM.
// It reports false when SMTP_HOST is unset or empty.
//
// SMTP_PORT defaults to 587; a value that is not a valid port number is
// logged and replaced by the default. Port 465 selects implicit TLS,
// every other port STARTTLS, so credentials never travel in plaintext.
func smtpConfigFromEnv(e *env.Env, logger *slog.Logger) (mail.SMTPConfig, bool) {
	host := e.GetOrDefault("SMTP_HOST", "")
	if host == "" {
		return mail.SMTPConfig{}, false
	}

	port := defaultSMTPPort
	if raw := e.GetOrDefault("SMTP_PORT", ""); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			logger.Warn("corego: ignoring invalid SMTP_PORT, using the default", "value", raw, "port", defaultSMTPPort)
		} else {
			port = parsed
		}
	}

	tlsMode := mail.TLSStartTLS
	if port == implicitTLSSMTPPort {
		tlsMode = mail.TLSImplicit
	}

	return mail.SMTPConfig{
		Host:     host,
		Port:     port,
		Username: e.GetOrDefault("SMTP_USERNAME", ""),
		Password: e.GetOrDefault("SMTP_PASSWORD", ""),
		From:     e.GetOrDefault("SMTP_FROM", ""),
		TLSMode:  tlsMode,
	}, true
}
