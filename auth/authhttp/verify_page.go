package authhttp

import (
	"bytes"
	"html/template"
	"net/http"
)

// verifyPage is the content of one built-in GET /verify-email result
// page.
type verifyPage struct {
	code    int
	Title   string
	Message string
	Success bool
}

// verifyPages maps each verification status to its page. The pages never
// echo anything from the request (neither the address nor the token).
var verifyPages = map[string]verifyPage{
	verifyStatusVerified: {
		code:    http.StatusOK,
		Title:   "Email address verified",
		Message: "Thank you, your email address has been verified. You can close this page and sign in.",
		Success: true,
	},
	// Email security scanners often open links before the user does,
	// which consumes the single-use token. The wording covers that case
	// without revealing whether the address is registered or verified.
	verifyStatusInvalid: {
		code:    http.StatusBadRequest,
		Title:   "Invalid verification link",
		Message: "This verification link is invalid or has already been used. If you already verified your email address, you can simply sign in.",
	},
	verifyStatusExpired: {
		code:    http.StatusBadRequest,
		Title:   "Verification link expired",
		Message: "This verification link has expired. Request a new verification email from the app.",
	},
}

// verifyPageTemplate renders a result page. html/template escapes every
// value for its context, and the inline style is the only resource the
// page uses, matching its Content-Security-Policy.
var verifyPageTemplate = template.Must(template.New("verify").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Title}}</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#f4f5f7;color:#1f2933;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
main{max-width:28rem;margin:1rem;padding:2rem;background:#fff;border-radius:12px;box-shadow:0 1px 3px rgba(0,0,0,.12);text-align:center}
h1{margin:0 0 .75rem;font-size:1.375rem}
p{margin:0;line-height:1.5;color:#52606d}
.icon{font-size:2.5rem;line-height:1;margin-bottom:1rem}
.ok{color:#2f855a}
.fail{color:#c53030}
@media (prefers-color-scheme:dark){body{background:#1a202c;color:#f7fafc}main{background:#2d3748;box-shadow:none}p{color:#cbd5e0}}
</style>
</head>
<body>
<main>
{{if .Success}}<div class="icon ok" aria-hidden="true">&#10003;</div>{{else}}<div class="icon fail" aria-hidden="true">&#10007;</div>{{end}}
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
</main>
</body>
</html>
`))

// renderVerifyPage renders page to HTML. Execution only fails for
// programming errors in the template, which the package tests cover;
// the plain-text fallback keeps the response meaningful regardless.
func renderVerifyPage(page verifyPage) string {
	var buf bytes.Buffer
	if err := verifyPageTemplate.Execute(&buf, page); err != nil {
		return template.HTMLEscapeString(page.Title)
	}
	return buf.String()
}
