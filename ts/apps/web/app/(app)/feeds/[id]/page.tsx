import { notFound } from "next/navigation";
import { getClient, getFeed, listFolders } from "@/lib/sunred";
import { getApiErrorMessage, apiErrorStatus } from "@/lib/errors";
import { ApiError } from "@/components/api-error";
import { FeedDetail } from "@/components/feed-detail";
import type { Metadata } from "next";
import type { Feed, Folder } from "@/lib/types";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ id: string }>;
}): Promise<Metadata> {
  const { id } = await params;
  const feedId = Number(id);
  if (!Number.isFinite(feedId)) return { title: "Feed" };
  try {
    const { data } = await getFeed({ client: await getClient(), path: { feedId } });
    if (data) {
      const feed = data as Feed;
      return { title: feed.title || "Feed" };
    }
  } catch {
    // metadata is best-effort; fall through to default
  }
  return { title: "Feed" };
}

export default async function FeedPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const feedId = Number(id);
  if (!Number.isFinite(feedId)) notFound();

  const client = await getClient();
  const { data: feed, error } = await getFeed({
    client,
    path: { feedId },
  });
  if (error) {
    if (apiErrorStatus(error) === 404) notFound();
    return (
      <div className="p-4">
        <ApiError message={getApiErrorMessage(error)} status={apiErrorStatus(error)} />
      </div>
    );
  }
  if (!feed) notFound();

  const { data: foldersData } = await listFolders({ client });
  const folders = (foldersData ?? []) as Folder[];

  return <FeedDetail feed={feed as Feed} initialFolders={folders} />;
}
