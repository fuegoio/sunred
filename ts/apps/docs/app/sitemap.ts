import type { MetadataRoute } from "next";
import { source } from "@/lib/source";

const ORIGIN = "https://sunred.app";

// Served at https://sunred.app/docs/sitemap.xml — Next applies the basePath to
// metadata routes. Page URLs from the source are relative to the basePath, so
// the product index (url "/") maps to https://sunred.app/docs.
export default function sitemap(): MetadataRoute.Sitemap {
  return source.getPages().map((page) => ({
    url: page.url === "/" ? `${ORIGIN}/docs` : `${ORIGIN}/docs${page.url}`,
  }));
}
