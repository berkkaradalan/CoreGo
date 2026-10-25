"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

type Entry = {
  href: string;
  title: string;
  section: string;
  headings: { id: string; text: string }[];
  text: string;
};

type Result = { href: string; label: string; context: string; score: number };

let indexPromise: Promise<Entry[]> | null = null;

/** Loads the search index once, on first use. */
function loadIndex(): Promise<Entry[]> {
  indexPromise ??= fetch("/search-index.json")
    .then((r) => (r.ok ? r.json() : []))
    .catch(() => []);
  return indexPromise;
}

function search(entries: Entry[], query: string): Result[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return [];
  const matches = (s: string) => {
    const lower = s.toLowerCase();
    return terms.every((t) => lower.includes(t));
  };
  const results: Result[] = [];
  for (const e of entries) {
    if (matches(e.title)) results.push({ href: e.href, label: e.title, context: e.section, score: 100 });
    for (const h of e.headings) {
      if (h.id && matches(h.text)) results.push({ href: `${e.href}#${h.id}`, label: h.text, context: e.title, score: 50 });
    }
    if (matches(e.text)) {
      const lower = e.text.toLowerCase();
      const at = lower.indexOf(terms[0]);
      const snippet = e.text.slice(Math.max(0, at - 40), at + 90).trim();
      // Pages that use the term more often are more likely to be about it.
      const occurrences = lower.split(terms[0]).length - 1;
      results.push({ href: e.href, label: e.title, context: `…${snippet}…`, score: 10 + Math.min(occurrences, 30) });
    }
  }
  const seen = new Set<string>();
  return results
    .sort((a, b) => b.score - a.score)
    .filter((r) => {
      const key = r.href + r.label;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .slice(0, 12);
}

/** The search button in the header and its Ctrl/Cmd+K dialog. */
export function Search() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const results = useMemo(() => search(entries, query), [entries, query]);

  const show = useCallback(() => {
    setOpen(true);
    void loadIndex().then(setEntries);
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const typing = /^(INPUT|TEXTAREA)$/.test((e.target as HTMLElement)?.tagName ?? "");
      if ((e.key === "k" && (e.metaKey || e.ctrlKey)) || (e.key === "/" && !typing)) {
        e.preventDefault();
        show();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [show]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  useEffect(() => setSelected(0), [query]);

  function go(href: string) {
    setOpen(false);
    setQuery("");
    router.push(href);
  }

  function onInputKey(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Escape") {
      setOpen(false);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (results.length === 0) return;
      setSelected((s) => (s + (e.key === "ArrowDown" ? 1 : -1) + results.length) % results.length);
    } else if (e.key === "Enter" && results[selected]) {
      go(results[selected].href);
    }
  }

  return (
    <>
      <button type="button" className="search-button" onClick={show} aria-label="Search the docs">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </svg>
        <span>Search docs…</span>
        <kbd>Ctrl K</kbd>
      </button>
      {/* The dialog is portaled to <body>: the sticky header uses
          backdrop-filter, which would otherwise become the containing block
          of the fixed overlay and clip it to the header. */}
      {open &&
        createPortal(
          <div className="search-overlay" role="dialog" aria-modal="true" aria-label="Search" onMouseDown={() => setOpen(false)}>
            <div className="search-dialog" onMouseDown={(e) => e.stopPropagation()}>
              <div className="search-input-row">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <circle cx="11" cy="11" r="7" />
                  <path d="m20 20-3.5-3.5" />
                </svg>
                <input
                  ref={inputRef}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={onInputKey}
                  placeholder="Search guides, functions, environment variables…"
                  aria-label="Search query"
                />
                <kbd>Esc</kbd>
              </div>
              <div className="search-results">
                {query.trim() === "" ? (
                  <p className="search-hint">Try “RequireAuth”, “CORS”, “QueryAll” or “SMTP_HOST”.</p>
                ) : results.length === 0 ? (
                  <p className="search-hint">No results for “{query}”.</p>
                ) : (
                  results.map((r, i) => (
                    <a
                      key={r.href + r.label}
                      href={r.href}
                      className={i === selected ? "selected" : undefined}
                      onMouseEnter={() => setSelected(i)}
                      onClick={(e) => {
                        e.preventDefault();
                        go(r.href);
                      }}
                    >
                      <span className="result-label">{r.label}</span>
                      <span className="result-context">{r.context}</span>
                    </a>
                  ))
                )}
              </div>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
