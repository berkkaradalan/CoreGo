// The markdown pipeline: turns one repository markdown file into a hast
// tree (rendered to React by src/components/docs/doc-body.tsx), its table
// of contents and the structured data the search index is built from.
// The source files stay plain GitHub markdown: no frontmatter, no MDX.

import path from "node:path";

import type { TOCItemType } from "fumadocs-core/toc";
import {
  rehypeCode,
  remarkCodeTab,
  remarkHeading,
  remarkStructure,
  type StructuredData,
} from "fumadocs-core/mdx-plugins";
import type { Element, ElementContent, Properties, Root as HastRoot, RootContent } from "hast";
import type { Blockquote, Code, Link, Paragraph, Root as MdastRoot, Text } from "mdast";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";
import { visit } from "unist-util-visit";
import { VFile } from "vfile";

import { codeTheme } from "../code-theme";
import { REPO_URL } from "../site";

/** The result of processing one markdown document. */
export type ProcessedMarkdown = {
  /** The document as hast, code already highlighted. */
  tree: HastRoot;
  /** Headings of level 2 and 3, for "On this page". */
  toc: TOCItemType[];
  /** Headings and text blocks, for the search index. */
  structuredData: StructuredData;
};

/** Resolves a repository path to a site route, or undefined. */
export type RouteResolver = (repoPath: string) => string | undefined;

/** Labels shown in the header of code blocks, by fence language. */
const CODE_TITLES: Record<string, string> = {
  go: "Go",
  bash: "Terminal",
  sh: "Terminal",
  shell: "Terminal",
  dotenv: ".env",
  http: "HTTP",
  js: "JavaScript",
  ts: "TypeScript",
  json: "JSON",
  sql: "SQL",
  yaml: "YAML",
  text: "Text",
};

/**
 * rewriteLinks maps repository-relative markdown links onto site routes,
 * and links to other repository files onto GitHub.
 */
function rewriteLinks(source: string, resolve: RouteResolver) {
  return () => (tree: MdastRoot) => {
    visit(tree, "link", (node: Link) => {
      const url = node.url;
      if (!url || url.startsWith("#") || /^[a-z][a-z0-9+.-]*:/i.test(url)) return;
      const [target, fragment] = url.split("#", 2) as [string, string | undefined];
      const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(source), target));
      const suffix = fragment ? `#${fragment}` : "";
      const route = resolve(resolved);
      if (route) {
        node.url = route + suffix;
      } else if (!resolved.startsWith("..")) {
        node.url = `${REPO_URL}/blob/main/${resolved}${suffix}`;
      }
    });
  };
}

type CalloutType = "info" | "warning" | "error" | "success" | "idea";

/** GitHub alert markers ("> [!NOTE]") and their callout type and title. */
const ALERTS: Record<string, [CalloutType, string]> = {
  NOTE: ["info", "Note"],
  TIP: ["idea", "Tip"],
  IMPORTANT: ["info", "Important"],
  WARNING: ["warning", "Warning"],
  CAUTION: ["error", "Caution"],
};

/** Words that turn a blockquote with a bold lead-in into a callout. */
const LEAD_TYPES: [RegExp, CalloutType][] = [
  [/^(warning|caution|danger)\b/i, "warning"],
  [/^(note|tip|important|check|heads up)\b/i, "info"],
];

/**
 * remarkCallouts renders two blockquote forms as callouts, both of which
 * also read well on GitHub:
 *   - GitHub alerts: "> [!WARNING]" on the first line;
 *   - a bold lead-in such as "> **Warning: ...**" or "> **Check ...**".
 * Other blockquotes stay blockquotes.
 */
function remarkCallouts() {
  return (tree: MdastRoot) => {
    visit(tree, "blockquote", (node: Blockquote) => {
      const first = node.children[0];
      if (first?.type !== "paragraph") return;
      const lead = first.children[0];
      let type: CalloutType | undefined;
      let title: string | undefined;

      if (lead?.type === "text") {
        const match = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\][ \t]*\n?/i.exec(lead.value);
        if (match) {
          [type, title] = ALERTS[match[1].toUpperCase()];
          lead.value = lead.value.slice(match[0].length);
          if (lead.value === "") (first as Paragraph).children.shift();
          if (first.children.length === 0) node.children.shift();
        }
      } else if (lead?.type === "strong") {
        const text = lead.children.map((c) => (c.type === "text" ? (c as Text).value : "")).join("");
        type = LEAD_TYPES.find(([re]) => re.test(text))?.[1];
      }

      if (!type) return;
      node.data = { ...node.data, hName: "Callout", hProperties: { type, ...(title ? { title } : {}) } };
    });
  };
}

/**
 * remarkCodeTitles gives each fenced block a title from its language, so
 * every block has a header with the copy button (blocks without a header
 * would show the button over their first line). Untagged blocks are text.
 */
function remarkCodeTitles() {
  return (tree: MdastRoot) => {
    visit(tree, "code", (node: Code) => {
      node.lang ??= "text";
      const title = CODE_TITLES[node.lang];
      // Tabs already name each block, so tabbed blocks get no header.
      if (!title || /(^|\s)(title|tab)=/.test(node.meta ?? "")) return;
      node.meta = `${node.meta ?? ""} title="${title}"`.trim();
    });
  };
}

/** The shape of the MDX JSX nodes remarkCodeTab creates. */
type JsxNode = {
  type: "mdxJsxFlowElement" | "mdxJsxTextElement";
  name: string | null;
  attributes: { type: string; name?: string; value?: unknown }[];
  children: ElementContent[];
};

/**
 * rehypeJsxToElements turns the MDX JSX nodes that remarkCodeTab creates
 * (CodeBlockTabs, CodeBlockTab, ...) into plain hast elements with literal
 * properties, so they render through the `components` map without an MDX
 * runtime.
 */
function rehypeJsxToElements() {
  return (tree: HastRoot) => {
    const convert = (nodes: (RootContent | ElementContent)[]) => {
      nodes.forEach((child, index) => {
        const node = child as unknown as JsxNode;
        if ((node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement") && node.name) {
          const properties: Properties = {};
          for (const attribute of node.attributes) {
            if (attribute.type !== "mdxJsxAttribute" || !attribute.name) continue;
            const value = attribute.value;
            if (value === null || value === undefined) properties[attribute.name] = true;
            else if (typeof value === "string") properties[attribute.name] = value;
          }
          const element: Element = { type: "element", tagName: node.name, properties, children: node.children };
          nodes[index] = element;
        }
        const replaced = nodes[index];
        if ("children" in replaced) convert(replaced.children);
      });
    };
    convert(tree.children);
  };
}

/** Heading elements; the renderer wraps their content in an anchor. */
const HEADING_TAGS = new Set(["h1", "h2", "h3", "h4", "h5", "h6"]);

/**
 * rehypeUnwrapHeadingLinks replaces links inside headings with their
 * content. The heading renderer wraps the whole heading in a link to its
 * own anchor, and a link must not contain another link (for example the
 * changelog's "## [0.2.0] - Unreleased", whose version links to a compare
 * view). The text stays, so the heading ids, computed from the text
 * earlier in the pipeline, do not change.
 */
function rehypeUnwrapHeadingLinks() {
  const unwrap = (nodes: ElementContent[]): ElementContent[] =>
    nodes.flatMap((node) => {
      if (node.type !== "element") return [node];
      const children = unwrap(node.children);
      return node.tagName === "a" ? children : [{ ...node, children }];
    });
  return (tree: HastRoot) => {
    visit(tree, "element", (node: Element) => {
      if (!HEADING_TAGS.has(node.tagName)) return;
      node.children = unwrap(node.children);
      return "skip";
    });
  };
}

/**
 * Processes a markdown document. `source` is the path of the file
 * relative to the repository root; relative links are resolved from it.
 */
export async function processMarkdown(
  markdown: string,
  source: string,
  resolve: RouteResolver,
): Promise<ProcessedMarkdown> {
  const processor = unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(rewriteLinks(source, resolve))
    .use(remarkCallouts)
    // GitHub-compatible heading ids, so anchors from GitHub keep working.
    .use(remarkHeading, { customId: false, generateToc: true })
    .use(remarkCodeTitles)
    .use(remarkCodeTab)
    .use(remarkStructure)
    .use(remarkRehype, { passThrough: ["mdxJsxFlowElement", "mdxJsxTextElement"] })
    .use(rehypeUnwrapHeadingLinks)
    .use(rehypeCode, {
      themes: { light: codeTheme, dark: codeTheme },
      defaultColor: false,
      // The docs are plain GitHub markdown, so "// [!code ...]" comments
      // have no special meaning and must be shown as written.
      transformers: [],
      langs: ["go", "bash", "dotenv", "http", "js", "json", "sql"],
      fallbackLanguage: "text",
    })
    .use(rehypeJsxToElements);

  const vfile = new VFile({ value: markdown, path: source });
  const mdast = processor.parse(vfile);
  const tree = (await processor.run(mdast, vfile)) as HastRoot;

  const toc = ((vfile.data.toc as TOCItemType[] | undefined) ?? []).filter((t) => t.depth >= 2 && t.depth <= 3);
  const structuredData = (vfile.data.structuredData as StructuredData | undefined) ?? { headings: [], contents: [] };
  return { tree, toc, structuredData };
}
