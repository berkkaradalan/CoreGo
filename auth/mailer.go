package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/berkkaradalan/CoreGo/mail"
)

// Mailer sends the transactional emails of the auth flows. The service
// calls it with the recipient address and the raw single use token;
// implementations decide how to render and deliver the message.
// Implementations must be safe for concurrent use.
//
// Most calls happen in the background (sign-up, email change and the
// public request flows), with a context that carries the request's
// values but not its cancellation. Implementations should therefore
// apply their own timeouts.
type Mailer interface {
	// SendVerificationEmail delivers an email verification token to the
	// user with the given address.
	SendVerificationEmail(ctx context.Context, to, token string) error

	// SendPasswordResetEmail delivers a password reset token to the
	// user with the given address.
	SendPasswordResetEmail(ctx context.Context, to, token string) error
}

// DefaultMailerConfig configures the links and wording of the
// DefaultMailer emails.
type DefaultMailerConfig struct {
	// AppName is shown in subjects and bodies, for example "Verify your
	// email for Acme". Optional; whitespace (including line breaks) is
	// collapsed to single spaces.
	AppName string

	// VerifyEmailURL is the absolute http(s) URL the verification link
	// points at; "token" and "email" query parameters are added to it
	// and any existing query parameters are kept. Required. Typically
	// the API's GET {base path}/verify-email route, which verifies the
	// address and redirects or renders a result page.
	VerifyEmailURL string

	// ResetPasswordURL is the absolute http(s) URL of the application's
	// "choose a new password" page; the link carries the same "token"
	// and "email" query parameters. Required. The page submits them,
	// together with the new password, to ResetPassword (POST
	// {base path}/reset-password over HTTP).
	ResetPasswordURL string
}

// DefaultMailer is the built-in Mailer. It renders multipart emails (a
// plain text part and a simple, inline-styled HTML part with every
// dynamic value HTML-escaped) whose links point at the configured
// URLs, and delivers them through a mail.Sender such as
// mail.SMTPSender, mail.LogSender or mail.CaptureSender.
//
// Configuration problems (no sender, an empty or non-absolute URL) are
// reported as errors when sending, never as panics. Errors never
// contain the token.
type DefaultMailer struct {
	sender mail.Sender
	cfg    DefaultMailerConfig
}

// Compile-time check that DefaultMailer implements Mailer.
var _ Mailer = (*DefaultMailer)(nil)

// NewDefaultMailer returns a DefaultMailer that delivers through
// sender using the links and wording from cfg.
//
//	mailer := auth.NewDefaultMailer(mail.NewSMTPSender(smtpCfg), auth.DefaultMailerConfig{
//		AppName:          "Acme",
//		VerifyEmailURL:   "https://api.acme.test/auth/verify-email",
//		ResetPasswordURL: "https://acme.test/reset-password",
//	})
func NewDefaultMailer(sender mail.Sender, cfg DefaultMailerConfig) *DefaultMailer {
	cfg.AppName = strings.Join(strings.Fields(cfg.AppName), " ")
	return &DefaultMailer{sender: sender, cfg: cfg}
}

// SendVerificationEmail implements Mailer. The link is VerifyEmailURL
// with the "token" and "email" query parameters added.
func (m *DefaultMailer) SendVerificationEmail(ctx context.Context, to, token string) error {
	content := emailContent{
		Subject: withAppName("Verify your email address", "Verify your email for %s", m.cfg.AppName),
		Heading: "Verify your email address",
		Intro: withAppName("Please confirm your email address by opening the link below.",
			"Please confirm your email address for %s by opening the link below.", m.cfg.AppName),
		Action: "Verify email address",
		Outro:  "If you did not create an account, you can safely ignore this email.",
	}
	if err := m.send(ctx, "VerifyEmailURL", m.cfg.VerifyEmailURL, to, token, content); err != nil {
		return fmt.Errorf("auth: sending verification email: %w", err)
	}
	return nil
}

// SendPasswordResetEmail implements Mailer. The link is
// ResetPasswordURL with the "token" and "email" query parameters
// added.
func (m *DefaultMailer) SendPasswordResetEmail(ctx context.Context, to, token string) error {
	content := emailContent{
		Subject: withAppName("Reset your password", "Reset your password for %s", m.cfg.AppName),
		Heading: "Reset your password",
		Intro: withAppName("We received a request to reset your password. Open the link below to choose a new one.",
			"We received a request to reset your %s password. Open the link below to choose a new one.", m.cfg.AppName),
		Action: "Choose a new password",
		Outro:  "If you did not request a password reset, you can safely ignore this email; your password stays unchanged.",
	}
	if err := m.send(ctx, "ResetPasswordURL", m.cfg.ResetPasswordURL, to, token, content); err != nil {
		return fmt.Errorf("auth: sending password reset email: %w", err)
	}
	return nil
}

// emailContent is the wording of one email; Link is filled by send.
type emailContent struct {
	Subject string
	Heading string
	Intro   string
	Action  string
	Outro   string
	Link    string
}

// send builds the link, renders both parts and hands the message to
// the sender. field names the config field of base for error messages.
func (m *DefaultMailer) send(ctx context.Context, field, base, to, token string, content emailContent) error {
	if m.sender == nil {
		return errors.New("DefaultMailer has no mail.Sender")
	}
	link, err := buildEmailLink(field, base, to, token)
	if err != nil {
		return err
	}
	content.Link = link

	var html bytes.Buffer
	if err := emailHTMLTemplate.Execute(&html, content); err != nil {
		return fmt.Errorf("rendering HTML body: %w", err)
	}
	return m.sender.Send(ctx, mail.Message{
		To:      []string{to},
		Subject: content.Subject,
		Text:    renderEmailText(content),
		HTML:    html.String(),
	})
}

// buildEmailLink adds the token and email query parameters to base.
// Existing query parameters are kept (an existing "token" or "email"
// parameter is replaced); the result is encoded with url.Values, so
// both values are escaped. base must be an absolute http or https URL.
func buildEmailLink(field, base, email, token string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("DefaultMailerConfig.%s is empty; set it to the absolute URL the email link should open", field)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("DefaultMailerConfig.%s is not a valid URL: %w", field, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("DefaultMailerConfig.%s must be an absolute http or https URL, got %q", field, base)
	}
	query := u.Query()
	query.Set("token", token)
	query.Set("email", email)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// withAppName returns plain when appName is empty and the formatted
// variant otherwise.
func withAppName(plain, format, appName string) string {
	if appName == "" {
		return plain
	}
	return fmt.Sprintf(format, appName)
}

// renderEmailText renders the plain text part.
func renderEmailText(c emailContent) string {
	return "Hello,\n\n" +
		c.Intro + "\n\n" +
		c.Link + "\n\n" +
		c.Outro + "\n"
}

// emailHTMLTemplate renders the HTML part. html/template escapes every
// value for its context (text, attribute and URL), so an AppName or a
// link containing markup can never inject HTML.
var emailHTMLTemplate = template.Must(template.New("email").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background-color:#ffffff;border-radius:8px;padding:32px;">
<tr><td style="font-size:20px;font-weight:600;color:#18181b;padding-bottom:16px;">{{.Heading}}</td></tr>
<tr><td style="font-size:15px;line-height:1.6;color:#3f3f46;padding-bottom:24px;">{{.Intro}}</td></tr>
<tr><td style="padding-bottom:24px;"><a href="{{.Link}}" style="display:inline-block;background-color:#18181b;color:#ffffff;text-decoration:none;font-size:15px;font-weight:600;padding:12px 20px;border-radius:6px;">{{.Action}}</a></td></tr>
<tr><td style="font-size:13px;line-height:1.6;color:#71717a;padding-bottom:4px;">If the button does not work, copy this link into your browser:</td></tr>
<tr><td style="font-size:13px;line-height:1.6;word-break:break-all;padding-bottom:24px;"><a href="{{.Link}}" style="color:#2563eb;">{{.Link}}</a></td></tr>
<tr><td style="font-size:13px;line-height:1.6;color:#71717a;">{{.Outro}}</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))
