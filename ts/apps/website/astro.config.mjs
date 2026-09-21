// @ts-check
import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import react from "@astrojs/react";
import mdx from "@astrojs/mdx";
import cloudflare from "@astrojs/cloudflare";
import { unified } from "@astrojs/markdown-remark";
import {
  rehypeCode,
  remarkCodeTab,
  remarkHeading,
  remarkNpm,
  remarkStructure,
} from "fumadocs-core/mdx-plugins";

// Fumadocs processing is scoped to MDX (the docs under src/content/docs) so
// the blog's Markdown keeps Astro's default rendering.
const fumadocsRemarkPlugins = [
  remarkHeading,
  remarkCodeTab,
  remarkNpm,
  [remarkStructure, { exportAs: "structuredData" }],
];

export default defineConfig({
  site: "https://sunred.app",
  // The website is the front door for sunred.app: it serves its own pages
  // (marketing, blog, docs) and proxies every other path to the Next.js app
  // (see src/middleware.ts and src/pages/[...path].ts). Self-hosted
  // deployments that don't want the marketing site just run the Next.js app
  // directly.
  output: "server",
  // No Astro sessions; the site only checks for the app's session cookie.
  session: false,
  // Runs on Cloudflare Workers. Prerendered pages (docs, blog, sitemaps) are
  // served as static assets from the edge; "/" and the catch-all proxy run in
  // the Worker. Prerendering stays on Node because the docs build reads the
  // OpenAPI spec from disk.
  adapter: cloudflare({ prerenderEnvironment: "node" }),
  integrations: [
    react(),
    mdx({
      extendMarkdownConfig: false,
      // Docs MDX is processed by Fumadocs' remark/rehype plugins with
      // Fumadocs' own syntax highlighting; the blog's plain Markdown keeps
      // Astro's default processor.
      processor: unified({
        syntaxHighlight: false,
        remarkPlugins: fumadocsRemarkPlugins,
        rehypePlugins: [rehypeCode],
      }),
    }),
  ],
  vite: {
    plugins: [tailwindcss()],
  },
});
