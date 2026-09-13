import { loader } from "fumadocs-core/source";
import { docs } from "collections/server";
import { resolveIcon } from "./icons";
import { openapi } from "./openapi";

// Three sections served from a single source so the sidebar can cross-link them:
//   /              — product docs   (content/docs/product/)
//   /self-hosting  — self-hosting   (content/docs/self-hosting/)
//   /api-reference — OpenAPI spec   (content/docs/openapi/)
// All URLs are relative to the basePath ("/docs") — next/link adds the prefix.
export const source = loader(
  {
    docs: docs.toFumadocsSource(),
    openapi: await openapi.staticSource({
      baseDir: "openapi",
      per: "operation",
      groupBy: "tag",
    }),
  },
  {
    baseUrl: "/",
    icon: resolveIcon,
    url: (slugs) => {
      if (slugs[0] === "openapi") {
        const rest = slugs.slice(1);
        return rest.length ? `/api-reference/${rest.join("/")}` : "/api-reference";
      }
      if (slugs[0] === "self-hosting") {
        const rest = slugs.slice(1);
        return rest.length ? `/self-hosting/${rest.join("/")}` : "/self-hosting";
      }
      // Product docs live at the root of the basePath — just the slug, no prefix.
      if (slugs[0] === "product") {
        const rest = slugs.slice(1);
        return rest.length ? `/${rest.join("/")}` : "/";
      }
      return slugs.length ? `/${slugs.join("/")}` : "/";
    },
    plugins: [openapi.loaderPlugin()],
  },
);
