// Checks the documentation of a running site (`npm run build && npm run
// start`, then `npm run check:links [base-url]`):
//   - every documentation route and its markdown source answer 200,
//   - every internal link on every docs page points to an existing route,
//     and its #fragment to an existing id on the target page,
//   - every link to a repository file on GitHub names a file that exists.
// Exits with status 1 on the first run that finds a problem.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const base = (process.argv[2] ?? "http://localhost:3000").replace(/\/$/, "");
const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..");
const REPO_BLOB = "https://github.com/berkkaradalan/CoreGo/blob/main/";

// The registry is TypeScript; read the slugs from it instead of importing it.
const registry = fs.readFileSync(path.join(here, "..", "src", "lib", "docs", "pages.ts"), "utf8");
const slugs = [...registry.matchAll(/slug: \[([^\]]*)\]/g)].map((m) =>
  m[1]
    .split(",")
    .map((s) => s.trim().replace(/^"|"$/g, ""))
    .filter(Boolean),
);
const routes = slugs.map((s) => (s.length === 0 ? "/docs" : `/docs/${s.join("/")}`));
const markdownRoutes = slugs.map((s) => `/markdown/${s.length === 0 ? "index" : s.join("-")}.md`);

const pages = new Map();
let problems = 0;
let checked = 0;

function report(message) {
  problems++;
  console.error(`FAIL ${message}`);
}

async function fetchPage(route) {
  if (pages.has(route)) return pages.get(route);
  const res = await fetch(base + route);
  const html = res.ok ? await res.text() : "";
  const entry = { status: res.status, html, ids: new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => decode(m[1]))) };
  pages.set(route, entry);
  return entry;
}

function decode(s) {
  return s.replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#x27;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">");
}

/** The links inside the page body (the article), not the navigation. */
function articleLinks(html) {
  const start = html.indexOf('id="nd-page"');
  const end = html.indexOf('id="nd-toc"');
  const body = html.slice(start, end === -1 ? undefined : end);
  return [...body.matchAll(/<a\s[^>]*href="([^"]+)"/g)].map((m) => decode(m[1]));
}

for (const route of [...routes, ...markdownRoutes, "/", "/api/search"]) {
  const { status } = await fetchPage(route);
  checked++;
  if (status !== 200) report(`${route} answered ${status}`);
}

for (const route of routes) {
  const { html } = await fetchPage(route);
  for (const href of articleLinks(html)) {
    checked++;
    if (href.startsWith(REPO_BLOB)) {
      const file = href.slice(REPO_BLOB.length).split("#")[0];
      if (!fs.existsSync(path.join(repoRoot, file))) report(`${route}: ${href} names a missing repository file`);
      continue;
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(href)) continue; // other external links
    const url = new URL(href, base + route);
    const target = url.pathname === "/docs/" ? "/docs" : url.pathname;
    const page = await fetchPage(target);
    if (page.status !== 200) {
      report(`${route}: ${href} -> ${target} answered ${page.status}`);
      continue;
    }
    const fragment = decodeURIComponent(url.hash.slice(1));
    if (fragment && !page.ids.has(fragment)) report(`${route}: ${href} -> #${fragment} does not exist on ${target}`);
  }
}

console.log(`${checked} routes and links checked on ${base}, ${routes.length} documentation pages.`);
if (problems > 0) {
  console.error(`${problems} problem(s) found.`);
  process.exit(1);
}
console.log("No broken routes, links or anchors.");
