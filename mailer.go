package corego

import (
	"errors"

	"github.com/berkkaradalan/CoreGo/mail"
)

// Mailer kinds reported in the "corego: auth ready" startup log line.
const (
	mailKindSMTP   = "smtp"
	mailKindLog    = "log"
	mailKindCustom = "custom"
	mailKindNone   = "none"
)

// errMailSMTPWithoutHost is returned by New for COREGO_MAIL=smtp without
// SMTP_HOST.
var errMailSMTPWithoutHost = errors.New("corego: COREGO_MAIL=smtp needs SMTP_HOST (and the other SMTP_* variables) to be set")

// authMailSender resolves the sender the facade wraps in an
// auth.DefaultMailer when AuthOptions.Service.Mailer is nil: custom
// (AuthOptions.MailSender) when set, otherwise the one COREGO_MAIL
// selects. It returns the sender, nil when no email is sent, and its
// kind for the startup log.
//
// COREGO_MAIL modes:
//   - "" (automatic): SMTP when SMTP_HOST is set, otherwise none with a
//     warning;
//   - "smtp": SMTP, and an error when SMTP_HOST is not set;
//   - "log": a mail.LogSender on the facade logger (it logs each
//     email's links at Info level), with a warning;
//   - "none": no email, and no warning.
func (c *Core) authMailSender(custom mail.Sender, mode string) (mail.Sender, string, error) {
	if custom != nil {
		return custom, mailKindCustom, nil
	}
	switch mode {
	case mailModeNone:
		return nil, mailKindNone, nil
	case mailModeLog:
		c.logger().Warn("corego: COREGO_MAIL=log: auth emails are written to the log instead of being sent, " +
			"and their links contain live tokens; use it for local development only")
		return mail.NewLogSender(c.logger()), mailKindLog, nil
	}

	if smtpCfg, ok := smtpConfigFromEnv(c.Env, c.logger()); ok {
		return mail.NewSMTPSender(smtpCfg), mailKindSMTP, nil
	}
	if mode == mailModeSMTP {
		return nil, "", errMailSMTPWithoutHost
	}
	c.logger().Warn("corego: auth emails are disabled: no mailer is configured, " +
		"so verification and password reset emails are never sent " +
		"(set SMTP_HOST and the other SMTP_* variables, COREGO_MAIL=log for local development, " +
		"COREGO_MAIL=none to silence this warning, AuthOptions.MailSender or AuthOptions.Service.Mailer)")
	return nil, mailKindNone, nil
}
