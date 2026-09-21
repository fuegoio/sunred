import { createGetUrl } from "fumadocs-core/source";

// OG images are generated at build time by the endpoint under
// src/pages/og/docs (one image per docs page), served at /og/docs/<slug>/image.webp.
export const docsImageRoute = "/og/docs";

const getImageUrl = createGetUrl(docsImageRoute);

export function getPageImageUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, "image.webp"];

  return { segments, url: getImageUrl(segments, page.locale) };
}
