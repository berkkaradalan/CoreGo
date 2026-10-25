"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

import { GitHubIcon } from "./GitHubIcon";
import { Logo } from "./Logo";
import { Search } from "./Search";
import { ThemeToggle } from "./ThemeToggle";

type NavSection = { name: string; pages: { href: string; title: string }[] };

/**
 * The documentation frame: the header, the sidebar (a drawer on small
 * screens, opened from the header) and the page content.
 */
export function DocsShell({
  sections,
  repoUrl,
  children,
}: {
  sections: NavSection[];
  repoUrl: string;
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);

  useEffect(() => setOpen(false), [pathname]);

  return (
    <>
      <header className="docs-header">
        <div className="docs-header-inner">
          <button
            type="button"
            className="icon-button nav-toggle"
            onClick={() => setOpen((o) => !o)}
            aria-label="Toggle navigation"
            aria-expanded={open}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M4 7h16M4 12h16M4 17h16" />
            </svg>
          </button>
          <Link href="/docs" aria-label="CoreGo documentation home">
            <Logo />
          </Link>
          <Search />
          <nav className="header-links" aria-label="Site">
            <Link className="header-link hide-mobile" href="/">
              Home
            </Link>
            <a className="header-link hide-mobile" href={repoUrl} target="_blank" rel="noreferrer">
              <GitHubIcon />
              GitHub
            </a>
            <ThemeToggle />
          </nav>
        </div>
      </header>
      <div className="docs-layout">
        <aside className={`sidebar${open ? " open" : ""}`} aria-label="Documentation">
          {sections.map((section) => (
            <div key={section.name} className="nav-section">
              <p className="nav-title">{section.name}</p>
              {section.pages.map((page) => {
                const active = pathname === page.href;
                return (
                  <Link
                    key={page.href}
                    href={page.href}
                    className={active ? "active" : undefined}
                    aria-current={active ? "page" : undefined}
                  >
                    {page.title}
                  </Link>
                );
              })}
            </div>
          ))}
        </aside>
        {open && <div className="sidebar-backdrop" onClick={() => setOpen(false)} />}
        {children}
      </div>
    </>
  );
}
