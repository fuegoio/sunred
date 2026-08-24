"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Rss } from "lucide-react";
import { FeedIcon } from "@/components/feed-icon";
import { SubscribeButton } from "@/components/subscribe-button";
import { SubscribersDialog } from "@/components/subscribers-dialog";
import { PageHeader } from "@/components/page-header";
import { EntryCard } from "@/components/entry-card";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { buttonVariants } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
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
 * detail page's layout (max-w-3xl, PageHeader, ScrollArea) so the two surfaces
 * read as the same chrome — mutualized via the shared `SubscribeButton` in the
 * header actions slot and the same `EntryCard` for articles (in preview mode,
 * which hides the read/unread dot, share, star, and comments actions).
 *
 * When a global `feedId` is available (passed from entry cards and profile
 * links), the subscriber count is fetched and shown via the same
 * `SubscribersDialog` as the feed page, in the same toolbar position.
 */
export function FeedDiscovery({
  preview,
  feedId,
}: {
  preview: PreviewFeedBody;
  feedId?: number;
}) {
  const items = preview.items ?? [];
  const siteUrl = preview.site_url || undefined;

  // The feed metadata passed to each EntryCard so the favicon + title render.
  const feed: Feed = {
    id: 0,
    title: preview.title,
    site_url: preview.site_url,
    feed_url: preview.feed_url,
  } as unknown as Feed;

  const entries = items.map((item, i) => toEntry(item, i));

  // Subscriber count is only available when the global feed ID is known
  // (the feed exists in the database because someone subscribes to it).
  const { data: subscribers } = useQuery<FeedSubscribersResponse>({
    queryKey: ["feed-subscribers", feedId],
    queryFn: async () =>
      unwrap(feedSubscribers({ client: await getClient(), path: { feedId: feedId! } })),
    enabled: feedId !== undefined,
  });

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="shrink-0">
        <div className="mx-auto w-full max-w-3xl">
          <PageHeader
            className="static"
            title={preview.title || "Untitled feed"}
            icon={<FeedIcon siteUrl={siteUrl} className="size-5 shrink-0 rounded-md" />}
            actions={
              <div className="flex items-center gap-1">
                <SubscribeButton
                  feedUrl={preview.feed_url}
                  feedTitle={preview.title}
                  subscribed={false}
                />
                {siteUrl && (
                  <a
                    href={siteUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label="Open website"
                    className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                  >
                    <ExternalLink className="size-3.5" />
                  </a>
                )}
                <a
                  href={preview.feed_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="Open feed XML"
                  className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                >
                  <Rss className="size-3.5" />
                </a>
              </div>
            }
            metadata={
              preview.description ? (
                <p className="line-clamp-2 text-sm text-muted-foreground">
                  {preview.description}
                </p>
              ) : undefined
            }
          />
        </div>
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <div className="flex items-center justify-between border-b border-border px-4 py-2.5 pl-[52px] lg:pl-[48px]">
            <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Recent articles
              {entries.length > 0 && (
                <span className="ml-1.5 text-muted-foreground/70">({entries.length})</span>
              )}
            </h2>
            {subscribers !== undefined && (
              <div className="ml-auto">
                <SubscribersDialog
                  count={subscribers.count}
                  globalCount={subscribers.global_count}
                  subscribers={subscribers.subscribers ?? []}
                />
              </div>
            )}
          </div>
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
