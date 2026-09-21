import type { APIRoute } from "astro";
import { generateOGImage } from "fumadocs-ui/og/takumi";
import { source } from "../../../../lib/source";
import { createElement } from "react";
import { Logo } from "../../../../components/logo";

// One OG image per docs page, generated at build time from the page's title
// and description. URL shape: /og/docs/<page slugs>/image.webp (see
// lib/shared.ts). Brand orange matches the product's primary color.
export const prerender = true;

export function getStaticPaths() {
  return source.getPages().map((page) => ({
    params: { slug: page.slugs.join("/") },
  }));
}

export const GET: APIRoute = ({ params }) => {
  const slugs = params.slug?.split("/").filter((item) => item.length > 0) ?? [];
  const page = source.getPage(slugs);

  if (!page) return new Response(undefined, { status: 404 });

  return generateOGImage({
    title: page.data.title,
    description: page.data.description,
    icon: createElement(Logo, { style: { width: 64, height: 64 } }),
    site: "Sunred",
    primaryColor: "rgba(255, 105, 35, 0.3)",
    primaryTextColor: "rgb(255, 105, 35)",
    format: "webp",
  });
};
