import { redirect } from "next/navigation";
import type { Metadata } from "next";
import { getClient, listFeeds, previewFeed } from "@/lib/sunred";
import { getApiErrorMessage, apiErrorStatus } from "@/lib/errors";
import { ApiError } from "@/components/api-error";
import { FeedDiscovery } from "@/components/feed-discovery";
import { subscribeFeedSchema, normalizeFeedURL } from "@/lib/schemas";
import type { Feed, PreviewFeedBody } from "@/lib/types";

export const metadata: Metadata = { title: "Discover feed" };

/**
 * Discovery view for a feed the viewer is not subscribed to. The feed URL is
 * supplied via the `?url=` query parameter. Before previewing, the viewer's
 * subscriptions are checked by `feed_url`; if a match exists we redirect to the
 * canonical feed page (`/feeds/[id]`) so there is one consistent feed view per
 * feed. Otherwise the `preview-feed` endpoint fetches and parses the feed
 * without persisting anything.
 */
export default async function FeedPreviewPage({
  searchParams,
}: {
  searchParams: Promise<{ url?: string }>;
}) {
  const { url } = await searchParams;

  // Validate the URL before hitting the API.
  const parsed = subscribeFeedSchema.safeParse({ feed_url: url ?? "" });
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
        <ApiError
          message={getApiErrorMessage(error)}
          status={apiErrorStatus(error)}
        />
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

  return <FeedDiscovery preview={preview as PreviewFeedBody} />;
}
