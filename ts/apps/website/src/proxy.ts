import type { APIContext } from "astro";

// Hop-by-hop headers that must not be forwarded across a proxy boundary.
const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

// Request headers that describe the incoming body and must not be copied
// verbatim onto the upstream request: `host` must point at the app, and
// content-length is recomputed by the runtime for the forwarded body.
const DROP_REQUEST = new Set(["host", "content-length"]);

// Response headers from the upstream that describe its on-the-wire encoding.
// The Workers runtime decodes the body on fetch and re-encodes the response
// to the client itself, so forwarding these would make the browser try to
// decode an already-decoded body.
const DROP_RESPONSE = new Set(["content-encoding", "content-length"]);

// Origin of the Next.js app the website proxies unknown paths to. Reads the
// APP_URL var from the Worker environment (wrangler.jsonc); falls back to
// the local dev server when running outside Workers (astro preview).
async function getAppUrl(): Promise<string> {
  try {
    const { env } = (await import("cloudflare:workers")) as {
      env: Record<string, string | undefined>;
    };
    if (env.APP_URL) return env.APP_URL;
  } catch {
    // Not running on Workers (local dev / prerendering).
  }
  return process.env.APP_URL ?? "http://localhost:3000";
}

/**
 * proxyToApp forwards the incoming request to the Next.js app and returns its
 * response. It is used by the catch-all route (every path the website doesn't
 * own) and by the middleware (logged-in "/" so the app renders there).
 *
 * The app's absolute redirect Location headers are rewritten to the public
 * origin so redirects (e.g. unauth /feeds -> /login) stay on the public host
 * instead of leaking the internal app URL.
 */
export async function proxyToApp(context: APIContext): Promise<Response> {
  const { url, request } = context;
  const appUrl = (await getAppUrl()).replace(/\/+$/, "");
  const target = new URL(url.pathname + url.search, appUrl);

  const reqHeaders = new Headers();
  for (const [key, value] of request.headers) {
    const lower = key.toLowerCase();
    if (HOP_BY_HOP.has(lower) || DROP_REQUEST.has(lower)) continue;
    reqHeaders.set(key, value);
  }
  // Let the app build public URLs (redirects, canonical links) from the
  // original request instead of the internal proxy address.
  reqHeaders.set("x-forwarded-host", url.host);
  reqHeaders.set("x-forwarded-proto", url.protocol.replace(":", ""));
  reqHeaders.set("x-forwarded-for", request.headers.get("x-forwarded-for") ?? "");

  const hasBody = request.method !== "GET" && request.method !== "HEAD";
  const upstream = await fetch(target, {
    method: request.method,
    headers: reqHeaders,
    body: hasBody ? request.body : undefined,
    // Surface the app's own redirects (3xx + Location) instead of following
    // them here, so Location can be rewritten to the public origin below.
    redirect: "manual",
    // Required when sending a stream body per the fetch spec; ignored by
    // runtimes that don't implement it.
    duplex: "half",
  }).catch((error: unknown) => {
    throw new Error(`website proxy: failed to reach the app at ${appUrl}: ${String(error)}`, {
      cause: error,
    });
  });

  const resHeaders = new Headers();
  for (const [key, value] of upstream.headers) {
    const lower = key.toLowerCase();
    if (HOP_BY_HOP.has(lower) || DROP_RESPONSE.has(lower) || lower === "set-cookie") continue;
    resHeaders.set(key, value);
  }
  // Set-Cookie must round-trip individually (the app sets its session cookie
  // through the proxy); Header iteration would merge them.
  for (const cookie of upstream.headers.getSetCookie()) {
    resHeaders.append("set-cookie", cookie);
  }
  const location = upstream.headers.get("location");
  if (location) resHeaders.set("location", rewriteLocation(location, url));

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: resHeaders,
  });
}

/** Rewrite an absolute redirect Location to the public origin. */
function rewriteLocation(location: string, publicUrl: URL): string {
  if (!/^https?:\/\//.test(location)) return location;
  try {
    const loc = new URL(location);
    loc.protocol = publicUrl.protocol;
    loc.host = publicUrl.host;
    return loc.toString();
  } catch {
    return location;
  }
}
