# CoreGo website

The landing page and documentation site of CoreGo, built with Next.js.

The documentation is **not** copied into this folder: every page is
rendered at build time from the markdown files of the repository
(`../docs/*.md`, `../CHANGELOG.md`, `../ROADMAP.md` and
`../examples/quickstart/README.md`). Edit those files and the site follows
on the next build.

## Develop

```bash
cd website
npm install
npm run dev        # http://localhost:3000
```

Other scripts:

| Script | What it does |
| --- | --- |
| `npm run build` | Production build; every page is prerendered. |
| `npm run start` | Serves the production build. |
| `npm run lint` | Type-checks the project. |

Requires Node.js 20.9 or newer.

## Structure

| Path | Purpose |
| --- | --- |
| `src/app/page.tsx` | Landing page. |
| `src/app/docs/[[...slug]]/page.tsx` | Documentation pages. |
| `src/app/search-index.json/route.ts` | Search index, generated at build time. |
| `src/lib/docs.ts` | Navigation, markdown rendering, link rewriting, search index. |
| `src/lib/site.ts` | Version, repository URL and other constants. |
| `src/components/` | Logo, search dialog, table of contents, theme toggle, docs shell. |
| `src/app/icon.svg`, `public/logo*.svg` | The logo and favicon. |

To add a documentation page, create the markdown file under `../docs/` and
add an entry to `DOC_PAGES` in `src/lib/docs.ts`. Relative links between
markdown files are rewritten to site routes automatically; links to other
repository files point to GitHub.

When releasing a new version, update `VERSION` in `src/lib/site.ts`.

## Deploy to Vercel

1. Import the repository in Vercel.
2. Set **Root Directory** to `website`. The framework preset is detected
   as Next.js; the default build settings work as they are.
3. Keep **Include files outside the root directory in the Build Step**
   enabled (the default): the build reads the markdown files one level up.
4. Deploy.
