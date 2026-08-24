// Date and time formatting helpers shared across entry views.

/** Absolute date, e.g. "Aug 14, 2026". */
export function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return "";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

/** Absolute date + time, e.g. "Aug 14, 2026, 3:45 PM". */
export function formatDateTime(dateStr: string): string {
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return "";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Relative time, e.g. "3h ago", "2d ago". Falls back to formatDate for >7d. */
export function formatRelative(dateStr: string): string {
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return "";
  const diffMs = Date.now() - date.getTime();
  const sec = Math.round(diffMs / 1000);
  const min = Math.round(sec / 60);
  const hr = Math.round(min / 60);
  const day = Math.round(hr / 24);

  if (sec < 60) return "just now";
  if (min < 60) return `${min}m ago`;
  if (hr < 24) return `${hr}h ago`;
  if (day < 7) return `${day}d ago`;
  return formatDate(dateStr);
}

/** Google favicon service URL for a site. */
export function faviconUrl(siteUrl: string): string {
  return `https://www.google.com/s2/favicons?domain=${siteUrl}&sz=64`;
}

/** Hostname for a site URL, falling back to the raw string. */
export function siteDomain(siteUrl: string | undefined | null): string {
  if (!siteUrl) return "";
  try {
    return new URL(siteUrl).hostname.replace(/^www\./, "");
  } catch {
    return siteUrl;
  }
}

/** Strip HTML tags to produce a plain-text snippet of `maxLen` chars. */
export function htmlSnippet(html: string | undefined | null, maxLen = 240): string {
  if (!html) return "";
  const text = html
    .replace(/<[^>]*>/g, " ")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, " ")
    .trim();
  if (text.length <= maxLen) return text;
  return text.slice(0, maxLen).trimEnd() + "…";
}
