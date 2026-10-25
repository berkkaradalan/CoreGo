// The documentation registry: which repository markdown file is rendered
// at which route, under which sidebar group, in reading order.

import {
  ArrowUpRight,
  BookText,
  Database,
  FlaskConical,
  Globe,
  HardDrive,
  History,
  House,
  KeyRound,
  Mail,
  Map as MapIcon,
  Rocket,
  Route,
  SlidersHorizontal,
  SquareTerminal,
  type LucideIcon,
} from "lucide-react";

/** Sidebar groups, in order. */
export const SECTIONS = ["Introduction", "Guides", "Reference"] as const;

/** A sidebar group. */
export type Section = (typeof SECTIONS)[number];

/** One documentation page. */
export type DocPage = {
  /** URL segments under /docs; [] is /docs itself. */
  slug: string[];
  /** Sidebar label. */
  title: string;
  /** Sidebar group. */
  section: Section;
  /** Markdown source, relative to the repository root. */
  source: string;
  /** Sidebar icon. */
  icon: LucideIcon;
};

/** The navigation, in reading order. */
export const DOC_PAGES: DocPage[] = [
  { slug: [], title: "Welcome", section: "Introduction", source: "docs/index.md", icon: House },
  { slug: ["getting-started"], title: "Getting started", section: "Introduction", source: "docs/getting-started.md", icon: Rocket },
  { slug: ["quickstart"], title: "Quickstart example", section: "Introduction", source: "examples/quickstart/README.md", icon: SquareTerminal },
  { slug: ["web-framework"], title: "Web framework", section: "Guides", source: "docs/web-framework.md", icon: Route },
  { slug: ["authentication"], title: "Authentication", section: "Guides", source: "docs/authentication.md", icon: KeyRound },
  { slug: ["http-api"], title: "Auth HTTP API", section: "Guides", source: "docs/http-api.md", icon: Globe },
  { slug: ["auth-storage"], title: "Auth storage", section: "Guides", source: "docs/auth-storage.md", icon: HardDrive },
  { slug: ["database"], title: "Database", section: "Guides", source: "docs/database.md", icon: Database },
  { slug: ["mail"], title: "Mail", section: "Guides", source: "docs/mail.md", icon: Mail },
  { slug: ["environment"], title: "Environment", section: "Guides", source: "docs/environment.md", icon: SlidersHorizontal },
  { slug: ["testing"], title: "Testing", section: "Guides", source: "docs/testing.md", icon: FlaskConical },
  { slug: ["api-reference"], title: "API reference", section: "Reference", source: "docs/api-reference.md", icon: BookText },
  { slug: ["migrating-from-0.1"], title: "Migrating from 0.1", section: "Reference", source: "docs/migrating-from-0.1.md", icon: ArrowUpRight },
  { slug: ["changelog"], title: "Changelog", section: "Reference", source: "CHANGELOG.md", icon: History },
  { slug: ["roadmap"], title: "Roadmap", section: "Reference", source: "ROADMAP.md", icon: MapIcon },
];

/** The URL of a documentation page. */
export function docHref(page: Pick<DocPage, "slug">): string {
  return page.slug.length === 0 ? "/docs" : `/docs/${page.slug.join("/")}`;
}

/** The URL of a page's raw markdown (served by src/app/markdown/[file]). */
export function markdownHref(page: Pick<DocPage, "slug">): string {
  return `/markdown/${markdownFileName(page)}`;
}

/** The file name a page's raw markdown is served under. */
export function markdownFileName(page: Pick<DocPage, "slug">): string {
  return `${page.slug.length === 0 ? "index" : page.slug.join("-")}.md`;
}

/** Repository files that are not documentation pages but have a route. */
const EXTRA_ROUTES: Record<string, string> = { "README.md": "/" };

const routeBySource = new Map<string, string>([
  ...DOC_PAGES.map((p) => [p.source, docHref(p)] as [string, string]),
  ...Object.entries(EXTRA_ROUTES),
]);

/** The site route of a repository file, if it has one. */
export function routeForSource(repoPath: string): string | undefined {
  return routeBySource.get(repoPath);
}

/** Finds the page registered for the given URL segments. */
export function findDoc(slug: string[] | undefined): DocPage | undefined {
  const key = (slug ?? []).join("/");
  return DOC_PAGES.find((p) => p.slug.join("/") === key);
}
