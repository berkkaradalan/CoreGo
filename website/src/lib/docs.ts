// Build-time documentation loading. Only server components import this
// module: it reads the markdown files of the repository from disk.

import fs from "node:fs/promises";
import path from "node:path";
import { cache } from "react";

import rehypeShiki from "@shikijs/rehype";
import type { Element, ElementContent, Root as HastRoot } from "hast";
import { toString as hastToString } from "hast-util-to-string";
import type { Link, Root as MdastRoot } from "mdast";
import rehypeSlug from "rehype-slug";
import rehypeStringify from "rehype-stringify";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";
import { visit } from "unist-util-visit";

import { REPO_URL } from "./site";

/** The repository root: the website lives in <repo>/website. */
const REPO_ROOT = path.resolve(process.cwd(), "..");

/** One entry of the documentation navigation. */
export type DocPage = {
  /** URL segments under /docs; [] is /docs itself. */
  slug: string[];
  /** Sidebar label. */
  title: string;
  /** Sidebar group. */
  section: string;
  /** Markdown source, relative to the repository root. */
  source: string;
};

/** A heading of a rendered page, used for the table of contents and search. */
export type Heading = { id: string; text: string; depth: number };

/** A rendered documentation page. */
export type RenderedDoc = DocPage & {
  html: string;
  headings: Heading[];
  /** The text of the first-level heading, or the sidebar title. */
  heading: string;
  /** The first paragraph, used as the page description. */
  description: string;
  plainText: string;
};

/** The navigation, in reading order. */
export const DOC_PAGES: DocPage[] = [
  { slug: [], title: "Welcome", section: "Introduction", source: "docs/index.md" },
  { slug: ["getting-started"], title: "Getting started", section: "Introduction", source: "docs/getting-started.md" },
  { slug: ["quickstart"], title: "Quickstart example", section: "Introduction", source: "examples/quickstart/README.md" },
  { slug: ["web-framework"], title: "Web framework", section: "Guides", source: "docs/web-framework.md" },
  { slug: ["authentication"], title: "Authentication", section: "Guides", source: "docs/authentication.md" },
  { slug: ["http-api"], title: "Auth HTTP API", section: "Guides", source: "docs/http-api.md" },
  { slug: ["auth-storage"], title: "Auth storage", section: "Guides", source: "docs/auth-storage.md" },
  { slug: ["database"], title: "Database", section: "Guides", source: "docs/database.md" },
  { slug: ["mail"], title: "Mail", section: "Guides", source: "docs/mail.md" },
  { slug: ["environment"], title: "Environment", section: "Guides", source: "docs/environment.md" },
  { slug: ["testing"], title: "Testing", section: "Guides", source: "docs/testing.md" },
  { slug: ["api-reference"], title: "API reference", section: "Reference", source: "docs/api-reference.md" },
  { slug: ["migrating-from-0.1"], title: "Migrating from 0.1", section: "Reference", source: "docs/migrating-from-0.1.md" },
  { slug: ["changelog"], title: "Changelog", section: "Reference", source: "CHANGELOG.md" },
  { slug: ["roadmap"], title: "Roadmap", section: "Reference", source: "ROADMAP.md" },
];

/** The URL of a documentation page. */
export function docHref(page: Pick<DocPage, "slug">): string {
  return page.slug.length === 0 ? "/docs" : `/docs/${page.slug.join("/")}`;
}

/** Repository files that are not documentation pages but have a route. */
const EXTRA_ROUTES: Record<string, string> = { "README.md": "/" };

const routeBySource = new Map<string, string>([
  ...DOC_PAGES.map((p) => [p.source, docHref(p)] as [string, string]),
  ...Object.entries(EXTRA_ROUTES),
]);

/** Finds the page registered for the given URL segments. */
export function findDoc(slug: string[] | undefined): DocPage | undefined {
  const key = (slug ?? []).join("/");
  return DOC_PAGES.find((p) => p.slug.join("/") === key);
}

/** The sidebar, grouped by section in navigation order. */
export function navSections(): { name: string; pages: DocPage[] }[] {
  const sections: { name: string; pages: DocPage[] }[] = [];
  for (const page of DOC_PAGES) {
    const last = sections.at(-1);
    if (last && last.name === page.section) {
      last.pages.push(page);
    } else {
      sections.push({ name: page.section, pages: [page] });
    }
  }
  return sections;
}

/**
 * rewriteLinks maps repository-relative markdown links onto site routes
 * (or onto GitHub for repository files without a page).
 */
function rewriteLinks(source: string) {
  return (tree: MdastRoot) => {
    visit(tree, "link", (node: Link) => {
      const url = node.url;
      if (!url || url.startsWith("#") || /^[a-z][a-z0-9+.-]*:/i.test(url)) return;
      const [target, fragment] = url.split("#", 2) as [string, string | undefined];
      const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(source), target));
      const route = routeBySource.get(resolved);
      const suffix = fragment ? `#${fragment}` : "";
      if (route) {
        node.url = route + suffix;
      } else if (!resolved.startsWith("..")) {
        node.url = `${REPO_URL}/blob/main/${resolved}${suffix}`;
      }
    });
  };
}

/** Human readable labels for code block languages. */
const LANGUAGE_LABELS: Record<string, string> = {
  go: "Go",
  bash: "Shell",
  sh: "Shell",
  shell: "Shell",
  dotenv: ".env",
  http: "HTTP",
  js: "JavaScript",
  json: "JSON",
  sql: "SQL",
  text: "Text",
};

function codeLanguage(code: Element): string | undefined {
  const classes = code.properties?.className;
  const list = Array.isArray(classes) ? classes : [];
  for (const c of list) {
    if (typeof c === "string" && c.startsWith("language-")) return c.slice("language-".length);
  }
  return undefined;
}

/**
 * frameCodeBlocks wraps every code block in a frame with a header that
 * shows the language and a copy button, and gives plain blocks the
 * "text" language so they are styled like the rest.
 */
function frameCodeBlocks() {
  return (tree: HastRoot) => {
    visit(tree, "element", (node: Element, index, parent) => {
      if (node.tagName !== "pre" || !parent || index === undefined) return;
      const code = node.children.find((c): c is Element => c.type === "element" && c.tagName === "code");
      if (!code) return;
      let lang = codeLanguage(code);
      if (!lang) {
        lang = "text";
        code.properties = { ...code.properties, className: ["language-text"] };
      }
      const label = LANGUAGE_LABELS[lang] ?? lang;
      const frame: Element = {
        type: "element",
        tagName: "div",
        properties: { className: ["code-frame"] },
        children: [
          {
            type: "element",
            tagName: "div",
            properties: { className: ["code-head"] },
            children: [
              { type: "element", tagName: "span", properties: { className: ["code-lang"] }, children: [{ type: "text", value: label }] },
              {
                type: "element",
                tagName: "button",
                properties: { type: "button", className: ["copy-button"], ariaLabel: "Copy code" },
                children: [{ type: "text", value: "Copy" }],
              },
            ],
          },
          node,
        ],
      };
      parent.children[index] = frame as ElementContent;
    });
  };
}

/** wrapTables puts every table in a horizontally scrollable frame. */
function wrapTables() {
  return (tree: HastRoot) => {
    visit(tree, "element", (node: Element, index, parent) => {
      if (node.tagName !== "table" || !parent || index === undefined) return;
      parent.children[index] = {
        type: "element",
        tagName: "div",
        properties: { className: ["table-frame"] },
        children: [node],
      };
    });
  };
}

/** collectHeadings records h2 and h3 headings into the given array. */
function collectHeadings(out: Heading[]) {
  return () => (tree: HastRoot) => {
    visit(tree, "element", (node: Element) => {
      const depth = { h1: 1, h2: 2, h3: 3 }[node.tagName as "h1" | "h2" | "h3"];
      if (!depth) return;
      const id = typeof node.properties?.id === "string" ? node.properties.id : "";
      out.push({ id, text: hastToString(node), depth });
    });
  };
}

/** Renders markdown into HTML with highlighted, framed code blocks. */
async function renderMarkdown(markdown: string, source: string) {
  const headings: Heading[] = [];
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(() => rewriteLinks(source))
    .use(remarkRehype)
    .use(rehypeSlug)
    .use(collectHeadings(headings))
    .use(wrapTables)
    .use(frameCodeBlocks)
    .use(rehypeShiki, {
      themes: { light: "github-light-default", dark: "github-dark-default" },
      defaultColor: false,
      langs: ["go", "bash", "dotenv", "http", "js", "json", "sql"],
      fallbackLanguage: "text",
    })
    .use(rehypeStringify)
    .process(markdown);
  return { html: String(file), headings };
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

/** Loads and renders a documentation page. Results are cached per build. */
export const loadDoc = cache(async (page: DocPage): Promise<RenderedDoc> => {
  // Every page is prerendered, so the markdown is only read at build time;
  // the ignore comment keeps the bundler from tracing the whole repository.
  const raw = await fs.readFile(path.join(/*turbopackIgnore: true*/ REPO_ROOT, page.source), "utf8");
  const { title, body } = stripTitle(raw);
  const { html, headings } = await renderMarkdown(body, page.source);
  return {
    ...page,
    html,
    headings: headings.filter((h) => h.depth > 1),
    heading: title ?? page.title,
    description: firstParagraph(body),
    plainText: body
      .replace(/```[\s\S]*?```/g, " ")
      .replace(/[#>*_`|[\]()-]/g, " ")
      .replace(/\s+/g, " ")
      .trim(),
  };
});

/** Highlights a standalone code sample (used by the landing page). */
export async function highlight(code: string, lang: string): Promise<string> {
  const { html } = await renderMarkdown("```" + lang + "\n" + code.trimEnd() + "\n```\n", "docs/index.md");
  return html;
}

/** One entry of the client-side search index. */
export type SearchEntry = {
  href: string;
  title: string;
  section: string;
  headings: { id: string; text: string }[];
  text: string;
};

/** Builds the search index over every documentation page. */
export async function buildSearchIndex(): Promise<SearchEntry[]> {
  const docs = await Promise.all(DOC_PAGES.map((p) => loadDoc(p)));
  return docs.map((d) => ({
    href: docHref(d),
    title: d.title,
    section: d.section,
    headings: d.headings.map((h) => ({ id: h.id, text: h.text })),
    text: d.plainText,
  }));
}
