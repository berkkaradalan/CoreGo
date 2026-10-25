import { DocsShell } from "@/components/DocsShell";
import { docHref, navSections } from "@/lib/docs";
import { REPO_URL } from "@/lib/site";

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  const sections = navSections().map((s) => ({
    name: s.name,
    pages: s.pages.map((p) => ({ href: docHref(p), title: p.title })),
  }));

  return (
    <DocsShell sections={sections} repoUrl={REPO_URL}>
      {children}
    </DocsShell>
  );
}
