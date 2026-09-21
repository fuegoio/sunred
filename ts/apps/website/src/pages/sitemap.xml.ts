import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

export const prerender = true;

// Served at https://sunred.app/sitemap.xml (prerendered assets win over the
// catch-all proxy, same as blog/rss.xml.ts). Covers the pages the website
// owns: the marketing home page and the blog. The docs sitemap is served by
// this app at /docs/sitemap.xml; both are listed in public/robots.txt.
export const GET: APIRoute = async ({ site }) => {
  const origin = (site ?? new URL("https://sunred.app")).toString().replace(/\/+$/, "");

  const posts = (await getCollection("blog")).sort(
    (a, b) => b.data.pubDate.valueOf() - a.data.pubDate.valueOf(),
  );

  const entries = [
    `  <url>
    <loc>${origin}/</loc>
    <changefreq>weekly</changefreq>
    <priority>1.0</priority>
  </url>`,
    `  <url>
    <loc>${origin}/blog</loc>
    <changefreq>weekly</changefreq>
    <priority>0.8</priority>
  </url>`,
    ...posts.map((post) => {
      return `  <url>
    <loc>${origin}/blog/${post.id}</loc>
    <lastmod>${post.data.pubDate.toISOString()}</lastmod>
  </url>`;
    }),
  ];

  const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${entries.join("\n")}
</urlset>
`;

  return new Response(xml, {
    headers: { "Content-Type": "application/xml; charset=utf-8" },
  });
};
