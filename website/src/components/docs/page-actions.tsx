"use client";

import { Check, Copy, SquarePen } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

/**
 * The actions next to a page title: copy the page's markdown source
 * (fetched once from `markdownUrl`, exactly as it is in the repository)
 * and open the file on GitHub for editing.
 */
export function PageActions({ markdownUrl, githubUrl }: { markdownUrl: string; githubUrl: string }) {
  const [copied, setCopied] = useState(false);
  const cache = useRef<string | null>(null);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function copy() {
    try {
      let markdown = cache.current;
      if (markdown === null) {
        const res = await fetch(markdownUrl);
        if (!res.ok) throw new Error(`markdown request failed: ${res.status}`);
        markdown = await res.text();
        cache.current = markdown;
      }
      await navigator.clipboard.writeText(markdown);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied; the button simply does not confirm.
    }
  }

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2">
      <Button type="button" variant="outline" size="sm" onClick={copy} aria-live="polite">
        {copied ? <Check /> : <Copy />}
        {copied ? "Copied" : "Copy Markdown"}
      </Button>
      <Button asChild variant="outline" size="sm">
        <a href={githubUrl} target="_blank" rel="noreferrer">
          <SquarePen />
          Edit on GitHub
        </a>
      </Button>
    </div>
  );
}
