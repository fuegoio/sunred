import { createFromSource } from "fumadocs-core/search/server";
import { structure } from "fumadocs-core/mdx-plugins";
import type { APIRoute } from "astro";
import { source } from "../../../lib/source";

// The static search index the docs search dialog fetches (see
// src/components/docs/search.tsx). Prerendered, so searching is entirely
// client-side: no worker invocation per query.
export const prerender = true;

const server = createFromSource(source, {
  async buildIndex(page) {
    // OpenAPI pages carry pre-built structured data; MDX pages get theirs
    // parsed from the raw body.
    let structuredData = page.data.structuredData;
    if (!structuredData && page.data._raw) structuredData = structure(page.data._raw.body);

    return {
      id: page.url,
      title: page.data.title,
      description: page.data.description,
      url: page.url,
      structuredData,
    };
  },
});

export const GET: APIRoute = () => server.staticGET();
