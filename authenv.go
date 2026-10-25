package corego

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/berkkaradalan/CoreGo/env"
)

// Environment variables that configure the auth service when Config.Auth
// is set. Code settings are documented on AuthOptions; see authEnv for
// how the two combine.
const (
	envRequireVerifiedEmail = "COREGO_REQUIRE_VERIFIED_EMAIL"
	envAuthCookie           = "COREGO_AUTH_COOKIE"
	envAuthTablePrefix      = "COREGO_AUTH_TABLE_PREFIX"
	envMail                 = "COREGO_MAIL"
)

// Values of the COREGO_MAIL environment variable. The empty string
// selects the automatic mailer (MailSender, then SMTP_HOST, then none
// with a warning).
const (
	mailModeAuto = ""
	mailModeSMTP = "smtp"
	mailModeLog  = "log"
	mailModeNone = "none"
)

// authEnv holds the auth settings read from the environment.
//
// The booleans can only turn a feature on: the effective setting is the
// code value OR the environment value, so an environment can enable
// mandatory email verification or the session cookie for an application
// that does not set them in code, but never disable what the code turned
// on. tablePrefix applies only when AuthOptions.TablePrefix is empty, and
// mailMode only when neither AuthOptions.Service.Mailer nor
// AuthOptions.MailSender is set.
type authEnv struct {
	requireVerifiedEmail bool   // COREGO_REQUIRE_VERIFIED_EMAIL
	cookie               bool   // COREGO_AUTH_COOKIE
	tablePrefix          string // COREGO_AUTH_TABLE_PREFIX, trimmed
	mailMode             string // COREGO_MAIL, trimmed and lowercased
}

// readAuthEnv reads and validates the auth environment variables. Every
// variable is validated even when a code setting makes it irrelevant, so
// a typo in the deployment configuration never goes unnoticed.
func readAuthEnv(e *env.Env) (authEnv, error) {
	var (
		out authEnv
		err error
	)
	if out.requireVerifiedEmail, err = envBool(e, envRequireVerifiedEmail); err != nil {
		return authEnv{}, err
	}
	if out.cookie, err = envBool(e, envAuthCookie); err != nil {
		return authEnv{}, err
	}
	out.tablePrefix = e.GetOrDefault(envAuthTablePrefix, "")

	out.mailMode = strings.ToLower(e.GetOrDefault(envMail, ""))
	switch out.mailMode {
	case mailModeAuto, mailModeSMTP, mailModeLog, mailModeNone:
	default:
		return authEnv{}, fmt.Errorf("corego: invalid %s %q: valid values are %q, %q, %q or empty for the automatic selection",
			envMail, e.GetOrDefault(envMail, ""), mailModeSMTP, mailModeLog, mailModeNone)
	}
	return out, nil
}

// envBool parses a boolean environment variable with strconv.ParseBool.
// Unset, empty and whitespace-only values are false. Unlike env.Env.Bool,
// which falls back to the default, an invalid value is an error: a
// mistyped security switch must stop the application instead of being
// silently ignored.
func envBool(e *env.Env, key string) (bool, error) {
	raw := e.GetOrDefault(key, "")
	if raw == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("corego: invalid %s %q: valid values are true or false (also 1, t, T, TRUE, True, 0, f, F, FALSE, False)", key, raw)
	}
	return b, nil
}
