import { SiteNavbar } from "@/components/site-navbar";

/** The frame of the landing page: the shared navbar, then the page. */
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <>
      <SiteNavbar variant="landing" />
      <main className="flex flex-1 flex-col">{children}</main>
    </>
  );
}
