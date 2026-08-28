import type { Entry } from "@/lib/types";

/**
 * In-memory set of entries the user marked read during this browser session by
 * clicking an article link on the Unread view. The Unread list is
 * server-filtered to unread entries, so the moment an entry is marked read it
 * falls out of every background refetch (the 30s `refetchInterval` and
 * `refetchOnWindowFocus` when the user returns to the tab). To keep the
 * just-read row visible — dimmed, still actionable — until the user leaves the
 * page, the timeline merges these session-read entries back into the fresh
 * unread result.
 *
 * Only opening an article adds to the session; manually toggling the read dot
 * does not, so a manual mark-read leaves the Unread view on the next refetch
 * rather than being pinned visible.
 *
 * The store is module-scoped: it survives background refetches but is wiped by
 * a full page reload or a client-side navigation (see `clearReadSession`,
 * invoked from the app shell on route change).
 */
const readEntries = new Map<number, Entry>();

export function markReadSession(entry: Entry) {
  readEntries.set(entry.id, { ...entry, status: "read" });
}

export function removeReadSession(entryId: number) {
  readEntries.delete(entryId);
}

export function clearReadSession() {
  readEntries.clear();
}

export function getReadSessionEntries(): Entry[] {
  return [...readEntries.values()];
}
