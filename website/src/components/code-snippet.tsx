import { processMarkdown } from "@/lib/docs/markdown";

import { DocBody } from "./docs/doc-body";

/**
 * A highlighted code block with a copy button, rendered at build time
 * with the same pipeline and color theme as the documentation.
 * `title` replaces the default language label ("Go", "Terminal", ...).
 */
export async function CodeSnippet({ code, lang, title }: { code: string; lang: string; title?: string }) {
  const meta = title ? ` title="${title.replace(/"/g, "'")}"` : "";
  const fence = "````";
  const { tree } = await processMarkdown(`${fence}${lang}${meta}\n${code.trimEnd()}\n${fence}\n`, "website", () => undefined);
  return <DocBody tree={tree} />;
}
