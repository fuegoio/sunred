"use client";

import { useQuery } from "@tanstack/react-query";
import { FeedHeader } from "@/components/feed-header";
import { EntryCard } from "@/components/entry-card";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { getClient, feedSubscribers, unwrap } from "@/lib/sunred";
import type { Entry, Feed, PreviewFeedBody, PreviewFeedItem, FeedSubscribersResponse } from "@/lib/types";

/**
 * Map a preview item to the Entry shape EntryCard expects. Preview items
 * have no id, status, or starred state — those fields are stubbed and never
 * read because `preview` mode gates the actions that use them.
 */
function toEntry(item: PreviewFeedItem, index: number): Entry {
  return {
    id: index,
    feed_id: 0,
    hash: "",
    changed_at: item.published_at,
    published_at: item.published_at,
    status: "unread",
    starred: false,
    title: item.title,
    url: item.url,
    description: item.description ?? item.content,
    author: item.author,
    tags: item.tags,
  } as unknown as Entry;
}

/**
 * Discovery view for a feed the viewer is not subscribed to. Mirrors the feed
 * detail page's layout via the shared FeedHeader (with the Subscribe button in
 * the toolbar) and the same EntryCard for articles (in preview mode, which
 * hides the read/unread dot, share, star, and comments actions).
 *
 * The preview endpoint returns the global feed ID when the feed exists in the
 * database, so the subscriber count is always available via the same
 * SubscribersDialog as the feed page.
 */
export function FeedDiscovery({ preview }: { preview: PreviewFeedBody }) {
  const items = preview.items ?? [];
  const feedId = preview.id;

  // The feed metadata passed to each EntryCard so the favicon + title render.
  const feed: Feed = {
    id: 0,
    title: preview.title,
    site_url: preview.site_url,
    feed_url: preview.feed_url,
  } as unknown as Feed;

  const entries = items.map((item, i) => toEntry(item, i));

  const { data: subscribers } = useQuery<FeedSubscribersResponse>({
    queryKey: ["feed-subscribers", feedId],
    queryFn: async () =>
      unwrap(feedSubscribers({ client: await getClient(), path: { feedId: feedId! } })),
    enabled: feedId !== undefined && feedId > 0,
  });

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
