import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

const blog = defineCollection({
  loader: glob({ pattern: "**/*.md", base: "./src/content/blog" }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    pubDate: z.coerce.date(),
  }),
});

// Docs pages and sidebar metadata, rendered by Fumadocs (see src/lib/source.ts).
// Sections: product (served at /docs), self-hosting (/docs/self-hosting),
// openapi (/docs/api-reference, spec-generated).
const docs = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/docs" }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
    icon: z.string().optional(),
  }),
});

const meta = defineCollection({
  loader: glob({ pattern: "**/{meta.json,meta.yaml}", base: "./src/content/docs" }),
  schema: z.object({
    title: z.string().optional(),
    description: z.string().optional(),
    pages: z.array(z.string()).optional(),
    icon: z.string().optional(),
    root: z.boolean().optional(),
    separator: z.string().optional(),
    defaultOpen: z.boolean().optional(),
  }),
});

export const collections: Record<string, ReturnType<typeof defineCollection>> = {
  blog,
  docs,
  meta,
};
