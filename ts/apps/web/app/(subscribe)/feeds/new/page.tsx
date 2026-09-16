import { SubscribeFeedView } from "@/components/subscribe-feed-view";

/**
 * Subscribe page. Accepts an optional `?url=` query param so other views
 * (e.g. the entry card's subscribe-to-source action) can deep-link here
 * with the URL prefilled and the feed preview already fetching.
 */
export default async function SubscribeFeedPage({
  searchParams,
}: {
  searchParams: Promise<{ url?: string }>;
}) {
  const { url } = await searchParams;
  return <SubscribeFeedView initialUrl={url ?? ""} />;
}
