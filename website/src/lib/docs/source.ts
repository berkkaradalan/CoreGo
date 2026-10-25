// Build-time documentation loading. Only server code imports this module:
// it reads the markdown files of the repository from disk.

import fs from "node:fs/promises";
import path from "node:path";

import type { AdvancedIndex } from "fumadocs-core/search/server";
import type * as PageTree from "fumadocs-core/page-tree";
import { createElement } from "react";

import { processMarkdown, type ProcessedMarkdown } from "./markdown";
import { DOC_PAGES, SECTIONS, docHref, routeForSource, type DocPage } from "./pages";

/** The repository root: the website lives in <repo>/website. */
const REPO_ROOT = path.resolve(process.cwd(), "..");

/** A loaded documentation page. */
export type LoadedDoc = ProcessedMarkdown & {
  page: DocPage;
  /** The text of the first-level heading, or the sidebar title. */
  title: string;
  /** The first prose paragraph, flattened to plain text. */
  description: string;
  /** The markdown file exactly as it is in the repository. */
  raw: string;
};

/** Reads a repository file as UTF-8 text. */
export async function readRepoFile(source: string): Promise<string> {
  // Every page is prerendered, so the markdown is only read at build time;
  // the ignore comment keeps the bundler from tracing the whole repository.
  return fs.readFile(path.join(/*turbopackIgnore: true*/ REPO_ROOT, source), "utf8");
}

/** Removes the first level-one heading; the page renders its own title. */
function stripTitle(markdown: string): { title?: string; body: string } {
  const match = /^#\s+(.+)\n/m.exec(markdown);
  if (!match || markdown.slice(0, match.index).trim() !== "") return { body: markdown };
  return { title: match[1].trim(), body: markdown.slice(match.index + match[0].length) };
}

/** The first prose paragraph, flattened to plain text. */
function firstParagraph(markdown: string): string {
  for (const block of markdown.split(/\n\s*\n/)) {
    const text = block.trim();
    if (!text || /^(#|```|\||-|\*|>|\d+\.|<)/.test(text)) continue;
    return text
      .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
      .replace(/[`*_]/g, "")
      .replace(/\s+/g, " ")
      .trim();
  }
  return "";
}

// One promise per page and build: generateMetadata, the page, the search
// index and the markdown route all share the same work.
const loaded = new Map<string, Promise<LoadedDoc>>();

/** Loads and renders a documentation page. */
export function loadDoc(page: DocPage): Promise<LoadedDoc> {
  let promise = loaded.get(page.source);
  if (!promise) {
    promise = (async () => {
      const raw = await readRepoFile(page.source);
      const { title, body } = stripTitle(raw);
      const processed = await processMarkdown(body, page.source, routeForSource);
      return { ...processed, page, raw, title: title ?? page.title, description: firstParagraph(body) };
    })();
    loaded.set(page.source, promise);
    // A failed load must not be cached: the next caller retries.
    promise.catch(() => loaded.delete(page.source));
  }
  return promise;
}

/** The sidebar tree: one collapsible folder per section. */
export function pageTree(): PageTree.Root {
  return {
    name: "CoreGo",
    children: SECTIONS.map(
      (section): PageTree.Folder => ({
        type: "folder",
        name: section,
        defaultOpen: true,
        collapsible: true,
        children: DOC_PAGES.filter((p) => p.section === section).map(
          (p): PageTree.Item => ({
            type: "page",
            name: p.title,
            url: docHref(p),
            icon: createElement(p.icon),
          }),
        ),
      }),
    ),
  };
}

/** The search index entries of every documentation page. */
export async function searchIndexes(): Promise<AdvancedIndex[]> {
  const docs = await Promise.all(DOC_PAGES.map((p) => loadDoc(p)));
  return docs.map((doc) => ({
    id: docHref(doc.page),
    url: docHref(doc.page),
    title: doc.title,
    description: doc.description,
    breadcrumbs: [doc.page.section],
    structuredData: doc.structuredData,
  }));
}
