"use client";

import type * as PageTree from "fumadocs-core/page-tree";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { Container } from "fumadocs-ui/layouts/docs/slots/container";
import { SidebarTrigger } from "fumadocs-ui/layouts/docs/slots/sidebar";
import { PanelLeft } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { SearchField } from "@/components/search-button";
import { SiteNavbar } from "@/components/site-navbar";
import { buttonVariants } from "@/components/ui/button";

/** Renders nothing; replaces Fumadocs slots the site does not use. */
function Empty() {
  return null;
}

/**
 * The docs grid with the site navbar on top. It replaces the Fumadocs
 * container slot so the navbar sits inside the sidebar provider and can
 * open the sidebar drawer on small screens. The navbar height is passed
 * to Fumadocs as --fd-banner-height (see globals.css), which keeps the
 * sticky sidebar and table of contents below it.
 */
function DocsContainer(props: ComponentProps<"div">) {
  return (
    <>
      <SiteNavbar
        variant="docs"
        sidebarTrigger={
          <SidebarTrigger
            className={buttonVariants({ variant: "ghost", size: "icon" })}
            aria-label="Open the documentation menu"
          >
            <PanelLeft />
          </SidebarTrigger>
        }
      />
      <Container {...props} />
    </>
  );
}

/**
 * The documentation frame: the top navbar, the sidebar (the search field
 * on top, then the collapsible groups with icons) and the page. GitHub and
 * the theme toggle live in the navbar only, so the sidebar has no footer.
 */
export function DocsShell({ tree, children }: { tree: PageTree.Root; children: ReactNode }) {
  return (
    <DocsLayout
      tree={tree}
      nav={{ enabled: false }}
      themeSwitch={{ enabled: false }}
      searchToggle={{ enabled: false }}
      sidebar={{ collapsible: false, banner: <SearchField /> }}
      slots={{ container: DocsContainer, navTitle: Empty }}
    >
      {children}
    </DocsLayout>
  );
}
