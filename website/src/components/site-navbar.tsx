import Link from "next/link";
import type { ReactNode } from "react";

import { GitHubIcon } from "@/components/GitHubIcon";
import { Logo } from "@/components/Logo";
import { SearchIconButton } from "@/components/search-button";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { REPO_URL } from "@/lib/site";

/** The navbar props. */
type SiteNavbarProps =
  /** Landing page and 404: a "Docs" link next to GitHub and the theme toggle. */
  | { variant: "landing" }
  /**
   * Documentation: no "Docs" link (the sidebar is the navigation). Below
   * the md breakpoint a search button and `sidebarTrigger` (the drawer
   * trigger, rendered last) are added.
   */
  | { variant: "docs"; sidebarTrigger: ReactNode };

/**
 * The top bar of every page: the logo on the left; on the right a few
 * icon controls. It never repeats the documentation sidebar.
 */
export function SiteNavbar(props: SiteNavbarProps) {
  const docs = props.variant === "docs";

  return (
    <header className="sticky top-0 z-40 h-(--navbar-height) border-b bg-background/90 backdrop-blur">
      <div className="flex h-full items-center px-4 md:px-6">
        <Link href="/" aria-label="CoreGo home" className="shrink-0">
          <Logo size={28} />
        </Link>

        <div className="ml-auto flex items-center gap-1">
          {!docs && (
            <Button asChild variant="ghost" size="sm">
              <Link href="/docs">Docs</Link>
            </Button>
          )}
          {docs && <SearchIconButton className="md:hidden" />}
          <Button asChild variant="ghost" size="icon">
            <a href={REPO_URL} target="_blank" rel="noreferrer" aria-label="CoreGo on GitHub">
              <GitHubIcon size={16} />
            </a>
          </Button>
          <ThemeToggle />
          {docs && <div className="flex md:hidden">{props.sidebarTrigger}</div>}
        </div>
      </div>
    </header>
  );
}
