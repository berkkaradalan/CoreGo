import Link from "next/link";

import { LogoMark } from "@/components/Logo";
import { SiteNavbar } from "@/components/site-navbar";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <>
      <SiteNavbar variant="landing" />
      <main className="flex min-h-[80vh] flex-1 flex-col items-center justify-center gap-4 px-4 text-center">
        <LogoMark size={48} />
        <p className="font-mono text-sm text-muted-foreground">404</p>
        <h1 className="text-3xl font-semibold tracking-tight">This page does not exist.</h1>
        <p className="text-muted-foreground">The link may be old, or the page moved.</p>
        <div className="mt-4 flex gap-3">
          <Button asChild size="sm">
            <Link href="/docs">Documentation</Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link href="/">Home</Link>
          </Button>
        </div>
      </main>
    </>
  );
}
