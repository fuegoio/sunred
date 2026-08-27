import type { Entry } from "@/lib/types";

/**
 * In-memory set of entries the user marked read during this browser session by
 * clicking an article link on the Unread view. The Unread list is
 * server-filtered to unread entries, so the moment an entry is marked read it
 * falls out of every background refetch (the 30s `refetchInterval` and
 * `refetchOnWindowFocus` when the user returns to the tab). To keep the
 * just-read row visible — dimmed, still actionable — until a real page reload,
 * the timeline merges these session-read entries back into the fresh unread
 * result.
 *
 * The store is module-scoped: it survives client-side navigations and
 * background refetches, but is wiped by a full page reload — the only moment a
 * read row is expected to leave the Unread view.
 */
const readEntries = new Map<number, Entry>();

export function markReadSession(entry: Entry) {
  readEntries.set(entry.id, { ...entry, status: "read" });
}

export function removeReadSession(entryId: number) {
  readEntries.delete(entryId);
}

export function getReadSessionEntries(): Entry[] {
  return [...readEntries.values()];
}
