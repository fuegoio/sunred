"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { FeedHeader } from "@/components/feed-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import {
  getClient,
  markFeedRead,
  refreshFeed,
  updateFeed,
  listFolders,
  feedSubscribers,
  unwrap,
} from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import type { Feed, Folder, FeedSubscribersResponse } from "@/lib/types";

/**
 * Feed detail view: header with site link, refresh, mark-all-read, and delete
 * actions, plus the feed's entry timeline. The feed is refreshed server-side
 * before this component renders; the refresh button here is for on-demand use.
 */
export function FeedDetail({ feed, initialFolders }: { feed: Feed; initialFolders?: Folder[] }) {
  const queryClient = useQueryClient();
  const [marking, setMarking] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [movingFolder, setMovingFolder] = useState(false);

  const { data: folders } = useQuery<Folder[]>({
    queryKey: ["folders"],
    queryFn: async () => unwrap(listFolders({ client: await getClient() })),
    initialData: initialFolders,
  });

  const { data: subscribers } = useQuery<FeedSubscribersResponse>({
    queryKey: ["feed-subscribers", feed.id],
    queryFn: async () =>
      unwrap(feedSubscribers({ client: await getClient(), path: { feedId: feed.id } })),
  });

  async function handleRefresh() {
    setRefreshing(true);
    try {
      const { error } = await refreshFeed({
        client: await getClient(),
        path: { feedId: feed.id },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      toast.success("Feed refreshed");
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not refresh feed"));
    } finally {
      setRefreshing(false);
    }
  }

  async function handleMarkAllRead() {
    setMarking(true);
    try {
      const { error } = await markFeedRead({
        client: await getClient(),
        path: { feedId: feed.id },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success(`Marked all entries in "${feed.title}" as read`);
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not mark entries as read"));
    } finally {
      setMarking(false);
    }
  }

  async function handleMoveFolder(folderId: number | undefined) {
    setMovingFolder(true);
    try {
      const { error } = await updateFeed({
        client: await getClient(),
        path: { feedId: feed.id },
        body: { folder_id: folderId },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success("Feed moved");
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not move feed"));
    } finally {
      setMovingFolder(false);
    }
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <FeedHeader
        feed={feed}
        subscribed
        folders={folders}
        subscribers={subscribers}
        onRefresh={handleRefresh}
        refreshing={refreshing}
        onMarkAllRead={handleMarkAllRead}
        marking={marking}
        onMoveFolder={handleMoveFolder}
        movingFolder={movingFolder}
      />
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ feed_id: feed.id }}
            emptyTitle="No articles yet"
            emptyDescription="This feed hasn't produced any entries. It may not have been refreshed yet."
          />
        </div>
      </ScrollArea>
    </div>
  );
}
