import { ChevronLeft, ChevronRight } from "lucide-react";
import Link from "next/link";

import { cn } from "@/lib/utils";

/** A neighbouring documentation page. */
export type PageNavLink = { title: string; href: string };

/**
 * The previous / next links under a documentation page: one compact row
 * of two plain links. A missing side renders nothing.
 */
export function PageNav({ previous, next }: { previous?: PageNavLink; next?: PageNavLink }) {
  if (!previous && !next) return null;
  return (
    <nav aria-label="Pages" className="mt-12 flex items-start justify-between gap-6 border-t pt-6">
      {previous && <NavLink link={previous} direction="previous" />}
      {next && <NavLink link={next} direction="next" />}
    </nav>
  );
}

function NavLink({ link, direction }: { link: PageNavLink; direction: "previous" | "next" }) {
  const next = direction === "next";
  return (
    <Link
      href={link.href}
      rel={next ? "next" : "prev"}
      className={cn("group flex max-w-[48%] min-w-0 flex-col gap-0.5", next && "ml-auto items-end text-right")}
    >
      <span className="text-xs text-muted-foreground">{next ? "Next" : "Previous"}</span>
      <span className="flex max-w-full min-w-0 items-center gap-1 text-sm font-medium text-foreground transition-colors group-hover:text-link">
        {!next && <ChevronLeft className="size-3.5 shrink-0" aria-hidden="true" />}
        <span className="truncate">{link.title}</span>
        {next && <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />}
      </span>
    </Link>
  );
}
