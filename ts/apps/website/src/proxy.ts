import type { APIContext } from "astro";
import { Readable } from "node:stream";
import http from "node:http";
import https from "node:https";

// Origin of the Next.js app the website proxies unknown paths to. Defaults
// to the local dev server; set APP_URL (e.g. http://web:3000) in production.
const APP_URL = process.env.APP_URL?.replace(/\/+$/, "") ?? "http://localhost:3000";

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
// `content-length` is recomputed by Node for the buffered body we send.
const DROP_REQUEST = new Set(["host", "content-length"]);

/**
 * proxyToApp forwards the incoming request to the Next.js app and returns its
 * response. It is used by the catch-all route (every path the website doesn't
 * own) and by the middleware (logged-in "/" so the app renders there).
 *
 * This uses Node's `http`/`https` modules rather than global `fetch` so the
 * upstream body is streamed through **byte-for-byte without decompression**.
 * `fetch` (undici) transparently gunzips responses but leaves the original
 * `content-encoding` header in place, so forwarding its `Response` makes the
 * browser try to decode an already-decoded body (ERR_CONTENT_DECODING_*).
 * Raw passthrough keeps gzip/brotli end-to-end and lets the browser decode.
 *
 * The app's absolute redirect Location headers are rewritten to the public
 * origin so redirects (e.g. unauth /feeds -> /login) stay on the public host
 * instead of leaking the internal app URL.
 */
export async function proxyToApp(context: APIContext): Promise<Response> {
  const { url, request } = context;
  const target = new URL(url.pathname + url.search, APP_URL);
  const transport = target.protocol === "https:" ? https : http;

  const reqHeaders: Record<string, string> = {};
  for (const [key, value] of request.headers) {
    const lower = key.toLowerCase();
    if (HOP_BY_HOP.has(lower) || DROP_REQUEST.has(lower)) continue;
    reqHeaders[key] = value;
  }
  // Let the app build public URLs (redirects, canonical links) from the
  // original request instead of the internal proxy address.
  reqHeaders["x-forwarded-host"] = url.host;
  reqHeaders["x-forwarded-proto"] = url.protocol.replace(":", "");
  reqHeaders["x-forwarded-for"] = request.headers.get("x-forwarded-for") ?? "";

  // Buffer the request body for non-GET/HEAD so server actions and form posts
  // pass through. Buffering (rather than streaming) lets Node set the correct
  // content-length on the upstream request.
  const hasBody = request.method !== "GET" && request.method !== "HEAD";
  const body = hasBody ? Buffer.from(await request.arrayBuffer()) : undefined;

  return new Promise<Response>((resolve, reject) => {
    const upstream = transport.request(
      target,
      { method: request.method, headers: reqHeaders },
      (res) => {
        const resHeaders = new Headers();
        for (const [key, value] of Object.entries(res.headers)) {
          if (value == null) continue;
          const lower = key.toLowerCase();
          if (HOP_BY_HOP.has(lower)) continue;
          const val = Array.isArray(value) ? value.join(", ") : value;
          if (lower === "location") {
            resHeaders.set(key, rewriteLocation(val, url));
          } else {
            resHeaders.set(key, val);
          }
        }

        // 304, 204, and 1xx responses must not have a body — the Web
        // Response constructor throws if one is provided. Drain the
        // upstream stream so it doesn't leak.
        const status = res.statusCode ?? 200;
        const noBody = status === 304 || status === 204 || (status >= 100 && status < 200);
        if (noBody) res.resume();

        resolve(
          new Response(noBody ? null : (Readable.toWeb(res) as ReadableStream), {
            status,
            statusText: res.statusMessage ?? "",
            headers: resHeaders,
          }),
        );
      },
    );
    upstream.on("error", reject);
    if (body) upstream.end(body);
    else upstream.end();
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
