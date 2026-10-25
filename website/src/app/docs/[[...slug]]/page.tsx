import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Toc } from "@/components/Toc";
import { DOC_PAGES, docHref, findDoc, loadDoc } from "@/lib/docs";
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
  return { title: doc.slug.length === 0 ? "Documentation" : doc.heading, description: doc.description || undefined };
}

export default async function DocPage({ params }: Props) {
  const { slug } = await params;
  const page = findDoc(slug);
  if (!page) notFound();

  const doc = await loadDoc(page);
  const index = DOC_PAGES.indexOf(page);
  const prev = DOC_PAGES[index - 1];
  const next = DOC_PAGES[index + 1];

  return (
    <>
      <main className="doc">
        <p className="doc-section">{doc.section}</p>
        <h1 className="doc-title">{doc.heading}</h1>
        <article className="prose" dangerouslySetInnerHTML={{ __html: doc.html }} />
        <nav className="pager" aria-label="Previous and next page">
          {prev && (
            <Link className="prev" href={docHref(prev)}>
              <span>← Previous</span>
              {prev.title}
            </Link>
          )}
          {next && (
            <Link className="next" href={docHref(next)}>
              <span>Next →</span>
              {next.title}
            </Link>
          )}
        </nav>
        <footer className="doc-footer">
          Edit this page on{" "}
          <a href={`${REPO_URL}/blob/main/${doc.source}`} target="_blank" rel="noreferrer">
            GitHub
          </a>
          .
        </footer>
      </main>
      <Toc items={doc.headings} />
    </>
  );
}
