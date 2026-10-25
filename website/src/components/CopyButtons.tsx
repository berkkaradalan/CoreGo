"use client";

import { useEffect } from "react";

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fallback for insecure contexts without the async clipboard API.
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

/**
 * Makes every rendered ".copy-button" copy the code of its frame (or the
 * text of the element named by data-copy-target). One delegated listener
 * covers the server-rendered markdown.
 */
export function CopyButtons() {
  useEffect(() => {
    async function onClick(e: MouseEvent) {
      const button = (e.target as HTMLElement).closest<HTMLButtonElement>(".copy-button");
      if (!button) return;
      const frame = button.closest(".code-frame, .copy-frame");
      const source = frame?.querySelector("pre code, [data-copy]");
      const text = source?.getAttribute("data-copy") ?? source?.textContent ?? "";
      if (!(await copyText(text.replace(/\n$/, "")))) return;
      const label = button.textContent;
      button.textContent = "Copied";
      button.classList.add("copied");
      window.setTimeout(() => {
        button.textContent = label;
        button.classList.remove("copied");
      }, 1400);
    }
    document.addEventListener("click", onClick);
    return () => document.removeEventListener("click", onClick);
  }, []);
  return null;
}
