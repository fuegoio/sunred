"use client";

import { FeedHeader } from "@/components/feed-header";
import { EntryCard } from "@/components/entry-card";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import type { Entry, Feed, PreviewFeedBody, PreviewFeedItem, FeedSubscribersResponse } from "@/lib/types";

/**
 * Map a preview item to the Entry shape EntryCard expects. Preview items
 * have no real entry id — id is stubbed with the array index, which
 * triggers URL-based star/read in EntryCard's preview mode. The feed
 * metadata is attached so ShareToggle and StarToggle can send it to the
 * URL-based endpoints.
 */
function toEntry(item: PreviewFeedItem, index: number, feed: Feed): Entry {
  return {
    id: index,
    feed_id: 0,
    hash: "",
    changed_at: item.published_at,
    published_at: item.published_at,
    status: item.status || "unread",
    starred: item.starred || false,
    title: item.title,
    url: item.url,
    description: item.description ?? item.content,
    author: item.author,
    tags: item.tags,
    feed,
  } as unknown as Entry;
}

/**
 * Discovery view for a feed the viewer is not subscribed to. Mirrors the feed
 * detail page's layout via the shared FeedHeader (with the Subscribe button in
 * the toolbar) and the same EntryCard for articles. In preview mode, EntryCard
 * uses URL-based endpoints for star/read/share so users can interact with
 * articles from feeds they haven't subscribed to yet.
 *
 * The preview endpoint includes the subscriber summary directly in its
 * response (looked up by feed URL on the server), so the SubscribersDialog
 * renders without a separate round trip.
 */
export function FeedDiscovery({ preview }: { preview: PreviewFeedBody }) {
  const items = preview.items ?? [];

  const feed: Feed = {
    id: 0,
    title: preview.title,
    site_url: preview.site_url,
    feed_url: preview.feed_url,
  } as unknown as Feed;

  const entries = items.map((item, i) => toEntry(item, i, feed));

  // The preview response embeds the subscriber summary when the feed is
  // known to the instance; when it's not (no one subscribes), default to a
  // zeroed summary so the count always renders.
  const subscribers: FeedSubscribersResponse = preview.subscribers
    ? {
        count: preview.subscribers.count,
        global_count: preview.subscribers.global_count,
        subscribers: preview.subscribers.subscribers ?? [],
      }
    : { count: 0, global_count: 0, subscribers: [] };

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <FeedHeader
        feed={feed}
        subscribed={false}
        preview
        subscribers={subscribers}
      />
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          {entries.length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted-foreground">
              No articles found in this feed.
            </p>
          ) : (
            <div className="divide-y divide-border">
              {entries.map((entry, i) => (
                <EntryCard
                  key={i}
                  entry={entry}
                  feed={feed}
                  staggerIndex={i}
                  preview
                />
              ))}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
