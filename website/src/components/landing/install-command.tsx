"use client";

import { Check, Copy } from "lucide-react";
import { Fragment, useState } from "react";

import { Button } from "@/components/ui/button";

/**
 * The install command of the landing hero on one line with a copy button.
 * It is a plain element, not a code block: on narrow screens the command
 * wraps instead of scrolling, preferably after a "/" or before the "@".
 */
export function InstallCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied; the button simply does not confirm.
    }
  }

  return (
    <div className="inline-flex w-full max-w-full items-center gap-3 rounded-md border bg-card px-3 py-1.5 font-mono text-xs sm:h-10 sm:w-auto sm:py-0 sm:pr-1.5 sm:pl-4 sm:text-[13px]">
      <span aria-hidden="true" className="text-muted-foreground select-none">
        $
      </span>
      <code className="min-w-0 text-left [overflow-wrap:anywhere]">{withBreaks(command)}</code>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-7 shrink-0 text-muted-foreground hover:text-foreground"
        onClick={copy}
        aria-label={copied ? "Copied" : "Copy the install command"}
      >
        {copied ? <Check /> : <Copy />}
      </Button>
    </div>
  );
}

/** Splits a command into parts with line break opportunities after "/" and before "@". */
function withBreaks(command: string) {
  return command.split(/(?<=\/)|(?=@)/).map((part, i) => (
    <Fragment key={i}>
      {i > 0 && <wbr />}
      {part}
    </Fragment>
  ));
}
