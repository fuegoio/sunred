import { createSunredClient, type SunredClient } from "@sunred/api-client";
import { attachApiLogger } from "./logger";
import { env } from "./env";

const fetchNoStore: typeof fetch = (input, init) => fetch(input, { ...init, cache: "no-store" });

/**
 * Adapts the SDK's `{ data, error }` discriminated union to react-query's
 * throw-on-error contract: returns `data` on success, throws the parsed huma
 * `ErrorModel` body on failure so `getApiErrorMessage(query.error)` keeps
 * working in components.
 *
 *   const folders = await unwrap(
 *     listFolders({ client: await getClient() }),
 *   );
 */
export async function unwrap<T>(
  result: Promise<{ data: T | null | undefined; error: unknown }>,
): Promise<T> {
  const { data, error } = await result;
  if (error) throw error;
  return data as T;
}

export async function getClient(cookieHeader?: string): Promise<SunredClient> {
  const isServer = typeof window === "undefined";
  const headers: Record<string, string> = {};

  if (isServer) {
    const { cookies } = await import("next/headers");
    const ch = cookieHeader ?? (await cookies()).toString();
    if (ch) headers.Cookie = ch;
  }

  const client = createSunredClient({
    baseUrl: isServer ? env.SUNRED_API_URL : env.NEXT_PUBLIC_SUNRED_API_URL,
    fetch: fetchNoStore,
    headers: Object.keys(headers).length > 0 ? headers : undefined,
    credentials: isServer ? undefined : "include",
  });

  attachApiLogger(client, { isServer });

  return client;
}

export * from "@sunred/api-client";

/**
 * Build the proxy URL for a user's avatar or banner. The API proxies and
 * caches the PDS blob so the browser never touches the PDS directly.
 * Returns "" when the user has no image (caller should fall back to initials).
 */
export function avatarUrl(handle: string, hasAvatar?: boolean): string {
  if (!hasAvatar) return "";
  return `${publicApiBase()}/api/v1/users/${encodeURIComponent(handle)}/avatar`;
}

export function bannerUrl(handle: string, hasBanner?: boolean): string {
  if (!hasBanner) return "";
  return `${publicApiBase()}/api/v1/users/${encodeURIComponent(handle)}/banner`;
}

function publicApiBase(): string {
  return env.NEXT_PUBLIC_SUNRED_API_URL;
}
