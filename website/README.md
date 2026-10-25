# CoreGo website

The landing page and documentation site of CoreGo, built with Next.js,
Tailwind CSS v4, shadcn/ui and Fumadocs UI.

The documentation is **not** copied into this folder: every page is
rendered at build time from the markdown files of the repository
(`../docs/*.md`, `../CHANGELOG.md`, `../ROADMAP.md` and
`../examples/quickstart/README.md`). Edit those files and the site follows
on the next build. They stay plain GitHub markdown: no frontmatter, no MDX.

## Develop

```bash
cd website
npm install
npm run dev        # http://localhost:3000
```

| Script | What it does |
| --- | --- |
| `npm run build` | Production build; every page is prerendered. |
| `npm run start` | Serves the production build. |
| `npm run lint` | Type-checks the project. |
| `npm run check:contrast` | Checks the WCAG contrast of the site colors (see below). |
| `npm run check:links [url]` | Against a running site: every docs route answers 200, every internal link and `#anchor` resolves, every GitHub file link names an existing file. |

Requires Node.js 20.9 or newer.

## Structure

| Path | Purpose |
| --- | --- |
| `src/app/(home)/` | Landing page and its layout (the shared navbar on top). |
| `src/app/docs/` | Documentation layout and pages (`/docs`, `/docs/<page>`). |
| `src/app/api/search/route.ts` | Search index, exported at build time; searched in the browser. |
| `src/app/markdown/[file]/route.ts` | Raw markdown of every page (`/markdown/<page>.md`), used by "Copy Markdown". |
| `src/lib/docs/pages.ts` | The page registry: route, sidebar group, icon, source file. |
| `src/lib/docs/markdown.ts` | The markdown pipeline (links, callouts, headings, code). |
| `src/lib/docs/source.ts` | Loads pages, builds the sidebar tree and the search index. |
| `src/styles/` | The color tokens (see below). |
| `src/lib/code-theme.ts` | The Shiki theme; its colors are CSS variables from the color tokens. |
| `src/components/site-navbar.tsx` | The top navbar of every page: logo; a Docs link (landing and 404 only), GitHub, theme toggle; in the docs below `md` also search and the sidebar drawer trigger. |
| `src/components/docs/` | Docs frame (`docs-shell.tsx`), page actions, previous/next row (`page-nav.tsx`), markdown renderer. |
| `src/components/ui/` | shadcn/ui primitives in use (button, dialog). |
| `src/components/code-snippet.tsx` | A highlighted code block with copy button for any page. |
| `src/app/icon.png`, `src/app/apple-icon.png`, `public/logo-mark.png` | Favicon, Apple touch icon and the navbar mark. |
| `src/app/opengraph-image.png`, `src/app/twitter-image.png` | The link preview image (1200x630). |

## How the documentation is rendered

The site uses **Fumadocs UI** for the documentation frame (sidebar,
search dialog, table of contents, code blocks, callouts, mobile drawer;
the previous/next row is `src/components/docs/page-nav.tsx`) with **fumadocs-core and its own markdown
pipeline** instead of `fumadocs-mdx`:

- the sources are plain `.md` files that must render cleanly on GitHub,
  so they cannot gain frontmatter or MDX syntax;
- several tables contain `{...}`, which MDX would parse as expressions.

`src/lib/docs/markdown.ts` parses each file with remark (GFM), then:

1. rewrites relative links between the markdown files to site routes
   (`getting-started.md#install` becomes `/docs/getting-started#install`)
   and links to other repository files to GitHub;
2. turns GitHub alerts (`> [!NOTE]`, `> [!WARNING]`, ...) and blockquotes
   with a bold lead-in (`> **Warning: ...**`) into callouts;
3. adds GitHub-compatible heading ids (so anchors from GitHub keep
   working) and collects the table of contents; links inside headings
   render as plain text, because the heading itself links to its anchor;
4. labels each code block with its language and supports code tabs with
   the fence meta `tab="..."` (consecutive blocks with a `tab` become one
   tabbed block; GitHub ignores the meta);
5. extracts structured data (headings and text) for the search index;
6. highlights code with Shiki using `src/lib/code-theme.ts`.

The result is a hast tree that `src/components/docs/doc-body.tsx` renders
with the Fumadocs UI components. The sidebar tree is built from the
registry in `src/lib/docs/pages.ts`.

To add a documentation page, create the markdown file (usually under
`../docs/`) and add an entry to `DOC_PAGES` in `src/lib/docs/pages.ts`
with its route, group and a lucide icon.

## Colors

The site has one palette, sage: a soft green accent on an off-white
background in light mode and on pure black in dark mode.

- `src/styles/colors.css` defines the base tokens in the shadcn/ui
  convention (`--background`, `--foreground`, `--primary`,
  `--muted-foreground`, `--border`, `--ring`, ...) plus `--prose`,
  `--link`, `--selection`, `--code-background` and `--syntax-*`: the
  light variant on `:root`, the dark variant on `.dark`.
- `src/styles/tokens.css` derives the remaining tokens and maps them onto
  Fumadocs UI and Shiki; `src/app/globals.css` exposes them to Tailwind
  (`bg-background`, `text-link`, `border-border`, ...). Every color in the
  UI comes from these tokens.
- The color mode follows the system by default; the theme toggle
  switches it and next-themes applies it before the first paint, so
  there is no flash. `?theme=dark|light` on any URL sets it.
- `npm run check:contrast` verifies that body text, muted text, links and
  every syntax color reach WCAG AA (4.5:1) in both variants.

## Deploy to Vercel

1. Import the repository in Vercel.
2. Set **Root Directory** to `website`. The framework preset is detected
   as Next.js; the default build settings work as they are.
3. Keep **Include files outside the root directory in the Build Step**
   enabled (the default): the build reads the markdown files one level up.
4. Deploy.

When releasing a new version, update `VERSION` in `src/lib/site.ts`.
