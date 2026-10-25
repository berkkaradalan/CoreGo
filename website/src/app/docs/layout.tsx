import { DocsShell } from "@/components/docs/docs-shell";
import { pageTree } from "@/lib/docs/source";

/** The documentation frame: navbar, sidebar, search and mobile drawer. */
export default function Layout({ children }: { children: React.ReactNode }) {
  return <DocsShell tree={pageTree()}>{children}</DocsShell>;
}
