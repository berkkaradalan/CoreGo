"use client";

import { useSearchContext } from "fumadocs-ui/contexts/search";
import { Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** An icon button that opens the documentation search (Ctrl/Cmd+K). */
export function SearchIconButton({ className }: { className?: string }) {
  const { setOpenSearch } = useSearchContext();
  return (
    <Button
      variant="ghost"
      size="icon"
      className={className}
      onClick={() => setOpenSearch(true)}
      aria-label="Search the docs"
    >
      <Search />
    </Button>
  );
}

/** A full-width search field that opens the documentation search. */
export function SearchField({ className }: { className?: string }) {
  const { setOpenSearch } = useSearchContext();
  return (
    <button
      type="button"
      onClick={() => setOpenSearch(true)}
      className={cn(
        "flex h-9 w-full items-center gap-2 rounded-md border bg-card px-3 text-sm text-muted-foreground transition-colors hover:border-ring/60 hover:text-foreground",
        className,
      )}
    >
      <Search className="size-4" />
      <span className="flex-1 text-left">Search docs...</span>
      <kbd className="rounded border bg-muted px-1.5 py-px font-mono text-[10px] tracking-wide">Ctrl K</kbd>
    </button>
  );
}
