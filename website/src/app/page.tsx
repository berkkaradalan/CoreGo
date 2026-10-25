import Link from "next/link";

import { GitHubIcon } from "@/components/GitHubIcon";
import { Logo } from "@/components/Logo";
import { ThemeToggle } from "@/components/ThemeToggle";
import { highlight } from "@/lib/docs";
import { INSTALL_COMMAND, REPO_URL, VERSION } from "@/lib/site";

const HELLO_WORLD = `package main

import (
	"log"
	"net/http"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

func main() {
	core, err := corego.New(&corego.Config{Auth: &corego.AuthOptions{}})
	if err != nil {
		log.Fatal(err)
	}

	app := core.Server() // the /auth routes are already mounted
	api := app.Group("/api", core.RequireAuth())
	api.Get("/hello", func(c *web.Context) error {
		user := auth.MustUser(c)
		return c.JSON(http.StatusOK, map[string]string{"hello": user.Name})
	})

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}`;

const FEATURES = [
  {
    title: "Authentication",
    text: "Sign-up, sign-in and server-side sessions, email verification, password reset, email and password changes, bans. Seventeen ready-made routes under /auth.",
    href: "/docs/authentication",
  },
  {
    title: "Web framework",
    text: "A standard-library-only router: groups, middleware, CORS, rate limiting, request logging, timeouts and graceful shutdown.",
    href: "/docs/web-framework",
  },
  {
    title: "Databases",
    text: "MongoDB and PostgreSQL connected from one environment variable, with typed QueryAll[T] and QueryOne[T], transactions and a single ErrNotFound.",
    href: "/docs/database",
  },
  {
    title: "Email",
    text: "SMTP with STARTTLS or implicit TLS, a log sender for development and a capture sender for tests. Verification and reset emails are wired for you.",
    href: "/docs/mail",
  },
  {
    title: "Configuration",
    text: "A .env file plus the process environment. Every switch can be set from the environment, and one startup line tells you what was resolved.",
    href: "/docs/environment",
  },
  {
    title: "Your storage",
    text: "Keep users and sessions in memory, MongoDB or PostgreSQL. Indexes and versioned migrations run on startup, or bring your own store.",
    href: "/docs/auth-storage",
  },
];

const SECURITY = [
  ["Tokens hashed at rest", "Session and email tokens are stored as SHA-256 digests; passwords use bcrypt."],
  ["Single-use, purpose-bound tokens", "A verification link can never reset a password, and no token works twice."],
  ["No account enumeration", "Password reset and verification answer the same way, in the same time, for every address."],
  ["CSRF protection", "Cookie sessions reject cross-site requests unless the origin is trusted."],
  ["Rate limits built in", "Per client IP and per email address on every sensitive route."],
  ["Safe under concurrency", "Optimistic locking means parallel requests never overwrite each other's changes."],
];

export default async function Home() {
  const helloHtml = await highlight(HELLO_WORLD, "go");

  return (
    <div className="landing">
      <header className="landing-header">
        <div className="landing-header-inner">
          <Link href="/" aria-label="CoreGo home">
            <Logo />
          </Link>
          <nav className="landing-nav" aria-label="Sections">
            <a href="#features">Features</a>
            <a href="#example">Example</a>
            <a href="#security">Security</a>
            <Link href="/docs">Docs</Link>
          </nav>
          <div className="landing-actions">
            <ThemeToggle />
            <a className="button" href={REPO_URL} target="_blank" rel="noreferrer">
              <GitHubIcon />
              GitHub
            </a>
          </div>
        </div>
      </header>

      <section className="hero">
        <div className="hero-content">
          <Link className="hero-badge" href="/docs/changelog">
            <span className="hero-badge-version">v{VERSION}</span>
            Backend SDK for Go · see what is new →
          </Link>
          <h1>
            Auth. Data. Email.
            <br />
            <span className="accent">One facade.</span>
          </h1>
          <p className="hero-lead">
            CoreGo gives your Go API sign-up, sessions, MongoDB and PostgreSQL, transactional email and a web
            framework. A protected endpoint takes about ten lines.
          </p>
          <div className="hero-actions">
            <Link className="button primary" href="/docs/getting-started">
              Get started →
            </Link>
            <a className="button" href={REPO_URL} target="_blank" rel="noreferrer">
              <GitHubIcon />
              Star on GitHub
            </a>
          </div>
          <div className="install copy-frame">
            <code>
              <span className="prompt">$</span> <span data-copy={INSTALL_COMMAND}>{INSTALL_COMMAND}</span>
            </code>
            <button type="button" className="copy-button" aria-label="Copy install command">
              Copy
            </button>
          </div>
        </div>
      </section>

      <section className="section" id="example">
        <div className="showcase">
          <div>
            <p className="eyebrow">Ten lines to a real API</p>
            <div className="section-head">
              <h2>Everything a backend needs, already wired.</h2>
              <p>One call to corego.New reads your environment, connects your database and starts the auth service.</p>
            </div>
            <ul className="checklist">
              <li>
                <code>core.Server()</code> mounts sign-up, sign-in and sessions
              </li>
              <li>
                <code>core.RequireAuth()</code> protects a whole group
              </li>
              <li>
                <code>auth.MustUser(c)</code> returns the signed-in user
              </li>
              <li>
                <code>return c.JSON(...)</code> ends every handler
              </li>
              <li>
                <code>core.Run()</code> serves and shuts down gracefully
              </li>
            </ul>
          </div>
          <div dangerouslySetInnerHTML={{ __html: helloHtml }} />
        </div>
      </section>

      <section className="section" id="features">
        <p className="eyebrow">What is inside</p>
        <div className="section-head">
          <h2>Six building blocks, one import path.</h2>
          <p>Use them together through the facade, or pick any package on its own.</p>
        </div>
        <div className="features">
          {FEATURES.map((f, i) => (
            <article key={f.title} className="feature">
              <span className="feature-index">{String(i + 1).padStart(2, "0")}</span>
              <h3>{f.title}</h3>
              <p>{f.text}</p>
              <Link href={f.href}>Read the guide →</Link>
            </article>
          ))}
        </div>
      </section>

      <section className="section" id="security">
        <p className="eyebrow">Secure by default</p>
        <div className="section-head">
          <h2>The details you would otherwise forget.</h2>
        </div>
        <ul className="security">
          {SECURITY.map(([title, text]) => (
            <li key={title}>
              <strong>{title}</strong>
              {text}
            </li>
          ))}
        </ul>
      </section>

      <section className="section">
        <div className="cta">
          <h2>Ship the boring parts today.</h2>
          <p>Install CoreGo, add your first endpoint and let the facade handle auth, data and email.</p>
          <div className="hero-actions">
            <Link className="button primary" href="/docs/getting-started">
              Get started →
            </Link>
            <Link className="button" href="/docs/api-reference">
              API reference
            </Link>
          </div>
        </div>
      </section>

      <footer className="site-footer">
        <div className="site-footer-inner">
          <Logo size={22} />
          <nav aria-label="Footer">
            <Link href="/docs">Docs</Link>
            <Link href="/docs/changelog">Changelog</Link>
            <Link href="/docs/roadmap">Roadmap</Link>
            <a className="footer-github" href={REPO_URL} target="_blank" rel="noreferrer">
              <GitHubIcon size={14} />
              GitHub
            </a>
          </nav>
          <span>MIT License</span>
        </div>
      </footer>
    </div>
  );
}
