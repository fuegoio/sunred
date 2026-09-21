import type { APIRoute } from "astro";
import { source } from "../../lib/source";

// Served at https://sunred.app/docs/sitemap.xml (prerendered). Covers every
// docs section; the site-level sitemap at /sitemap.xml covers the marketing
// pages and the blog. Both are listed in public/robots.txt.
export const prerender = true;

const ORIGIN = "https://sunred.app";

export const GET: APIRoute = () => {
  const entries = source
    .getPages()
    .map(
      (page) =>
        `  <url>\n    <loc>${ORIGIN}${page.url}</loc>\n    <changefreq>weekly</changefreq>\n  </url>`,
    )
    .join("\n");

  const xml = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries}\n</urlset>`;

  return new Response(xml, { headers: { "Content-Type": "application/xml" } });
};
