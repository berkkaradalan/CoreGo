import { createSearchAPI } from "fumadocs-core/search/server";

import { searchIndexes } from "@/lib/docs/source";

// The search index is built once at build time and served as a static
// file; the search dialog downloads it and searches in the browser.
export const dynamic = "force-static";
export const revalidate = false;

export const { staticGET: GET } = createSearchAPI("advanced", {
  indexes: searchIndexes,
});
