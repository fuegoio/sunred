import { redirect } from "next/navigation";
import { FeedDiscovery } from "@/components/feed-discovery";
import { ApiError } from "@/components/api-error";
import { getClient, listFeeds, previewFeed } from "@/lib/sunred";
import { getApiErrorMessage, apiErrorStatus } from "@/lib/errors";
import { subscribeFeedSchema, normalizeFeedURL } from "@/lib/schemas";
import type { Feed, PreviewFeedBody } from "@/lib/types";

export const metadata = { title: "Discover feed" };

/**
 * Discovery view for a feed the viewer is not subscribed to. Validates the
 * `?url=` query, redirects to `/feeds/[id]` if the viewer already subscribes,
 * otherwise previews the feed and offers a one-click subscribe.
 */
export default async function DiscoverFeedPage({
  searchParams,
}: {
  searchParams: Promise<{ url?: string }>;
}) {
  const sp = await searchParams;
  if (!sp.url) {
    return (
      <div className="mx-auto w-full max-w-3xl p-4">
        <ApiError message="No feed URL provided. Open a feed from an article or a profile to preview it." />
      </div>
    );
  }

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

  return <FeedDiscovery preview={preview as PreviewFeedBody} />;
}
