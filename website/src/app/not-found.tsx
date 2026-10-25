import Link from "next/link";

import { LogoMark } from "@/components/Logo";

export default function NotFound() {
  return (
    <main className="section" style={{ minHeight: "80vh", display: "grid", placeItems: "center", textAlign: "center" }}>
      <div>
        <LogoMark size={48} />
        <p className="eyebrow" style={{ marginTop: 24 }}>
          404
        </p>
        <h1 style={{ margin: "0 0 16px", fontSize: 32 }}>This page is not part of the core.</h1>
        <p style={{ color: "var(--fg-soft)", marginBottom: 32 }}>The link may be old, or the page moved.</p>
        <div className="hero-actions">
          <Link className="button primary" href="/docs">
            Documentation
          </Link>
          <Link className="button" href="/">
            Home
          </Link>
        </div>
      </div>
    </main>
  );
}
