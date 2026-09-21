import type { StaticSource } from "fumadocs-core/source";
import { loader } from "fumadocs-core/source";
import { getCollection, type CollectionEntry } from "astro:content";
import * as path from "node:path";
import { resolveIcon } from "./icons";
import { openapi } from "./openapi";

const DOCS_DIR = "src/content/docs";

// Three sections served from a single source so the sidebar can cross-link
// them:
//   /docs                 — product docs   (src/content/docs/product/)
//   /docs/self-hosting    — self-hosting   (src/content/docs/self-hosting/)
//   /docs/api-reference   — OpenAPI spec   (src/content/docs/openapi/)
async function createDocsSource(): Promise<
  StaticSource<{
    metaData: CollectionEntry<"meta">["data"];
    pageData: CollectionEntry<"docs">["data"] & {
      _raw: CollectionEntry<"docs">;
    };
  }>
> {
  const out: StaticSource<{
    metaData: CollectionEntry<"meta">["data"];
    pageData: CollectionEntry<"docs">["data"] & {
      _raw: CollectionEntry<"docs">;
    };
  }> = {
    files: [],
  };

  for (const page of await getCollection("docs")) {
    if (!page.filePath) continue;
    out.files.push({
      type: "page",
      path: path.relative(DOCS_DIR, page.filePath),
      data: {
        ...page.data,
        _raw: page,
      },
    });
  }

  for (const meta of await getCollection("meta")) {
    if (!meta.filePath) continue;
    out.files.push({
      type: "meta",
      path: path.relative(DOCS_DIR, meta.filePath),
      data: meta.data,
    });
  }

  return out;
}

export const source = loader(
  {
    docs: await createDocsSource(),
    openapi: await openapi.staticSource({
      baseDir: "openapi",
      per: "operation",
      groupBy: "tag",
    }),
  },
  {
    baseUrl: "/docs",
    icon: resolveIcon,
    url: (slugs) => {
      if (slugs[0] === "openapi") {
        const rest = slugs.slice(1);
        return rest.length ? `/docs/api-reference/${rest.join("/")}` : "/docs/api-reference";
      }
      if (slugs[0] === "self-hosting") {
        const rest = slugs.slice(1);
        return rest.length ? `/docs/self-hosting/${rest.join("/")}` : "/docs/self-hosting";
      }
      // Product docs live at the root of /docs — just the slug, no prefix.
      if (slugs[0] === "product") {
        const rest = slugs.slice(1);
        return rest.length ? `/docs/${rest.join("/")}` : "/docs";
      }
      return slugs.length ? `/docs/${slugs.join("/")}` : "/docs";
    },
    plugins: [openapi.loaderPlugin()],
  },
);
