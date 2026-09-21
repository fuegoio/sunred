import { defineMiddleware } from "astro:middleware";
import { proxyToApp } from "./proxy";

// Cloudflare never caches HTML from cache-control alone — a Cache Rule must
// mark these pages eligible and "respect origin", which makes s-maxage the
// edge TTL. max-age=0 keeps the browser revalidating so a visitor who just
// logged in always reaches the origin on "/" instead of a stale marketing
// page; the rule must also bypass cache when the request carries
// sunred_session. Prerendered pages (blog, sitemap, rss) are served from disk
// without this middleware, so their edge TTL comes from the rule's fallback.
const CACHE_CONTROL = "public, max-age=0, must-revalidate, s-maxage=3600";

// Route pattern of the catch-all that proxies to the Next.js app. Every other
// route is a page this website owns.
const PROXY_ROUTE = "/[...path]";

export const onRequest = defineMiddleware(async (context, next) => {
  // Logged-in visitors of "/" get the app instead of the marketing page. The
  // session cookie is only a presence signal here; the app layout validates
  // it and redirects to /login if it's expired — no loop, since /login is a
  // public route that the catch-all proxies straight back to the app.
  if (context.url.pathname === "/" && context.cookies.get("sunred_session")?.value) {
    return proxyToApp(context);
  }
  const response = await next();
  if (context.routePattern !== PROXY_ROUTE && !response.headers.has("cache-control")) {
    response.headers.set("Cache-Control", CACHE_CONTROL);
  }
  return response;
});
