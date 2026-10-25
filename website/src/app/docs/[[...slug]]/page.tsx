import { DocsBody, DocsPage, DocsTitle } from "fumadocs-ui/layouts/docs/page";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { DocBody } from "@/components/docs/doc-body";
import { PageActions } from "@/components/docs/page-actions";
import { PageNav, type PageNavLink } from "@/components/docs/page-nav";
import { DOC_PAGES, type DocPage, docHref, findDoc, markdownHref } from "@/lib/docs/pages";
import { loadDoc } from "@/lib/docs/source";
import { REPO_URL } from "@/lib/site";

type Props = { params: Promise<{ slug?: string[] }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return DOC_PAGES.map((p) => ({ slug: p.slug }));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug } = await params;
  const page = findDoc(slug);
  if (!page) return {};
  const doc = await loadDoc(page);
  return {
    title: page.slug.length === 0 ? "Documentation" : doc.title,
    description: doc.description || undefined,
  };
}

/** The page at `index` in reading order (the sidebar order), if any. */
function neighbour(index: number): PageNavLink | undefined {
  const page: DocPage | undefined = DOC_PAGES[index];
  return page && { title: page.title, href: docHref(page) };
}

export default async function Page({ params }: Props) {
  const { slug } = await params;
  const page = findDoc(slug);
  if (!page) notFound();
  const doc = await loadDoc(page);
  const index = DOC_PAGES.indexOf(page);

  // Fumadocs' breadcrumb and footer cards are off: the page ends with the
  // compact previous / next row instead. The container's gap-4 adds 16px
  // to the margins below (32px under the title row, 64px above the row).
  return (
    <DocsPage toc={doc.toc} breadcrumb={{ enabled: false }} footer={{ enabled: false }}>
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <DocsTitle>{doc.title}</DocsTitle>
        <PageActions markdownUrl={markdownHref(page)} githubUrl={`${REPO_URL}/blob/main/${page.source}`} />
      </div>
      <DocsBody>
        <DocBody tree={doc.tree} />
      </DocsBody>
      <PageNav previous={neighbour(index - 1)} next={neighbour(index + 1)} />
    </DocsPage>
  );
}
