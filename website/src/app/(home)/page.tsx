// The landing page: one centered column of sections (hero, example,
// features, security) and the footer. Copy and claims must match the
// documentation in ../docs.

import { ArrowRight, Database, HardDrive, KeyRound, Mail, Route, SlidersHorizontal } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { CodeSnippet } from "@/components/code-snippet";
import { InstallCommand } from "@/components/landing/install-command";
import { HELLO_WORLD } from "@/components/landing/snippets";
import { Button } from "@/components/ui/button";
import { INSTALL_COMMAND, TAGLINE } from "@/lib/site";

const FEATURES = [
  {
    title: "Authentication",
    icon: KeyRound,
    text: "Sign-up, sign-in and server-side sessions, email verification, password reset, email and password changes, bans. Seventeen ready-made routes under /auth.",
    href: "/docs/authentication",
  },
  {
    title: "Web framework",
    icon: Route,
    text: "A standard-library-only router: groups, middleware, CORS, rate limiting, request logging, timeouts and graceful shutdown.",
    href: "/docs/web-framework",
  },
  {
    title: "Databases",
    icon: Database,
    text: "MongoDB and PostgreSQL connected from one environment variable, with typed QueryAll[T] and QueryOne[T], transactions and a single ErrNotFound.",
    href: "/docs/database",
  },
  {
    title: "Email",
    icon: Mail,
    text: "SMTP with STARTTLS or implicit TLS, a log sender for development and a capture sender for tests. Verification and reset emails are wired for you.",
    href: "/docs/mail",
  },
  {
    title: "Configuration",
    icon: SlidersHorizontal,
    text: "A .env file plus the process environment. Every switch can be set from the environment, and one startup line tells you what was resolved.",
    href: "/docs/environment",
  },
  {
    title: "Your storage",
    icon: HardDrive,
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

/** A landing section: centered heading, an optional sentence, then the content. */
function Section({ id, title, lead, children }: { id: string; title: string; lead?: string; children: ReactNode }) {
  return (
    <section id={id} className="py-16 sm:py-20">
      <div className="mx-auto max-w-3xl text-center">
        <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">{title}</h2>
        {lead && <p className="mx-auto mt-3 max-w-2xl text-pretty text-muted-foreground">{lead}</p>}
      </div>
      <div className="mt-10">{children}</div>
    </section>
  );
}

export default function Home() {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 sm:px-6">
      <section className="pt-20 pb-12 sm:pt-28">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-semibold tracking-tight text-balance sm:text-5xl">{TAGLINE}</h1>
          <p className="mx-auto mt-5 max-w-2xl text-lg text-pretty text-muted-foreground">
            CoreGo gives your Go API sign-up, sessions, MongoDB and PostgreSQL, transactional email and a web
            framework. A protected endpoint takes about ten lines.
          </p>
          {/* One row: the install command and the way into the docs. GitHub is in the navbar. */}
          <div className="mt-10 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <InstallCommand command={INSTALL_COMMAND} />
            <Button asChild size="lg" className="w-full sm:w-auto">
              <Link href="/docs/getting-started">
                Get started <ArrowRight />
              </Link>
            </Button>
          </div>
        </div>
      </section>

      <Section
        id="example"
        title="A protected API in one file."
        lead="One call to corego.New reads your environment, connects your database and starts the auth service."
      >
        {/* The full file from md up; it fits without scrolling there. */}
        <div className="mx-auto hidden max-w-3xl md:block [&_[role=region]]:max-h-none [&_figure]:my-0">
          <CodeSnippet code={HELLO_WORLD} lang="go" title="main.go" />
        </div>
        <div className="flex justify-center md:hidden">
          <Button asChild size="sm" variant="outline">
            <Link href="/docs/getting-started#hello-world">Read the hello world</Link>
          </Button>
        </div>
      </Section>

      <Section id="features" title="Six building blocks, one import path.">
        {/* One bordered block divided by 1px lines; the gap shows the border color. */}
        <div className="grid gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-2 lg:grid-cols-3">
          {FEATURES.map((f) => (
            <Link
              key={f.title}
              href={f.href}
              className="group block bg-background p-6 transition-colors outline-none hover:bg-card focus-visible:bg-card"
            >
              <h3 className="flex items-center gap-2.5 text-base font-medium">
                <f.icon aria-hidden="true" className="size-4 text-muted-foreground transition-colors group-hover:text-foreground" />
                {f.title}
              </h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{f.text}</p>
            </Link>
          ))}
        </div>
      </Section>

      <Section id="security" title="Secure by default.">
        <ul className="grid gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
          {SECURITY.map(([title, text]) => (
            <li key={title} className="border-l-2 border-border pl-4">
              <p className="font-medium">{title}</p>
              <p className="mt-1 text-sm text-muted-foreground">{text}</p>
            </li>
          ))}
        </ul>
      </Section>

      <footer className="mt-12 flex flex-col items-center justify-between gap-4 border-t py-8 text-sm text-muted-foreground sm:flex-row">
        <span>MIT License</span>
        <nav aria-label="Footer" className="flex gap-6">
          <Link className="hover:text-foreground" href="/docs">
            Docs
          </Link>
          <Link className="hover:text-foreground" href="/docs/changelog">
            Changelog
          </Link>
          <Link className="hover:text-foreground" href="/docs/roadmap">
            Roadmap
          </Link>
        </nav>
      </footer>
    </div>
  );
}
