import { DOC_PAGES, markdownFileName } from "@/lib/docs/pages";
import { readRepoFile } from "@/lib/docs/source";

// Serves the markdown source of every documentation page, exactly as it is
// in the repository, for the "Copy Markdown" button. Generated at build time.
export const dynamic = "force-static";
export const dynamicParams = false;

export function generateStaticParams() {
  return DOC_PAGES.map((p) => ({ file: markdownFileName(p) }));
}

export async function GET(_request: Request, { params }: { params: Promise<{ file: string }> }) {
  const { file } = await params;
  const page = DOC_PAGES.find((p) => markdownFileName(p) === file);
  if (!page) return new Response("Not found", { status: 404 });
  return new Response(await readRepoFile(page.source), {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
