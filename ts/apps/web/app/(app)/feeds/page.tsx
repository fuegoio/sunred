import { redirect } from "next/navigation";
import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { FeedDiscovery } from "@/components/feed-discovery";
import { ApiError } from "@/components/api-error";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { getClient, listFeeds, previewFeed } from "@/lib/sunred";
import { getApiErrorMessage, apiErrorStatus } from "@/lib/errors";
import { subscribeFeedSchema, normalizeFeedURL } from "@/lib/schemas";
import type { Feed, PreviewFeedBody } from "@/lib/types";

export const metadata = { title: "Feeds" };

/**
 * The feeds timeline. When `?url=` is present, this route instead renders the
 * discovery view for that feed (checking the viewer's subscriptions first and
 * redirecting to `/feeds/[id]` if already subscribed).
 */
export default async function FeedsPage({
  searchParams,
}: {
  searchParams: Promise<{ url?: string; id?: string }>;
}) {
  const sp = await searchParams;
  if (!sp.url) return <FeedsTimeline />;

  // Validate the URL before hitting the API.
  const parsed = subscribeFeedSchema.safeParse({ feed_url: sp.url });
  if (!parsed.success) {
    return (
      <div className="mx-auto w-full max-w-3xl p-4">
        <ApiError message="No feed URL provided. Open a feed from an article or a profile to preview it." />
      </div>
    );
  }
  const feedUrl = normalizeFeedURL(parsed.data.feed_url);

  const client = await getClient();

  // If the viewer already subscribes to this feed, go to the canonical page.
  const { data: feedsData } = await listFeeds({ client });
  const feeds = (feedsData ?? []) as Feed[];
  const existing = feeds.find((f) => f.feed_url === feedUrl);
  if (existing) redirect(`/feeds/${existing.id}`);

  const { data: preview, error } = await previewFeed({
    client,
    body: { feed_url: feedUrl },
  });
  if (error) {
    return (
      <div className="mx-auto w-full max-w-3xl p-4">
        <ApiError message={getApiErrorMessage(error)} status={apiErrorStatus(error)} />
      </div>
    );
  }
  if (!preview) {
    return (
      <div className="mx-auto w-full max-w-3xl p-4">
        <ApiError message="Could not preview this feed." />
      </div>
    );
  }

  // An optional global feed ID (passed from entry cards and profile links)
  // lets the discovery view show the subscriber count.
  const feedId = Number(sp.id);
  const globalFeedId = Number.isFinite(feedId) && feedId > 0 ? feedId : undefined;

  return <FeedDiscovery preview={preview as PreviewFeedBody} feedId={globalFeedId} />;
}

function FeedsTimeline() {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="Feeds" />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ source: "feeds" }}
            emptyTitle="No articles from your feeds"
            emptyDescription="Subscribe to RSS feeds and the latest articles from your subscriptions will appear here."
          />
        </div>
      </ScrollArea>
    </div>
  );
}
