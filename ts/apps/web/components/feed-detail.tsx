"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ExternalLink,
  Trash2,
  CheckCheck,
  Loader2,
  RefreshCw,
  Rss,
} from "lucide-react";
import { FeedRenameDialog } from "@/components/feed-rename-dialog";
import { FolderPickerPopover } from "@/components/folder-picker-popover";
import { SubscribersDialog } from "@/components/subscribers-dialog";
import { Button, buttonVariants } from "@workspace/ui/components/button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedIcon } from "@/components/feed-icon";
import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import {
  getClient,
  markFeedRead,
  deleteFeed,
  refreshFeed,
  updateFeed,
  listFolders,
  feedSubscribers,
  unwrap,
} from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { cn } from "@workspace/ui/lib/utils";
import type { Feed, Folder, FeedSubscribersResponse } from "@/lib/types";

/**
 * Feed detail view: header with site link, refresh, mark-all-read, and delete
 * actions, plus the feed's entry timeline. The feed is refreshed server-side
 * before this component renders; the refresh button here is for on-demand use.
 */
export function FeedDetail({ feed, initialFolders }: { feed: Feed; initialFolders?: Folder[] }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [marking, setMarking] = useState(false);
  const [deleting, setDeleting] = useState(false);
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

  async function handleDelete() {
    setDeleting(true);
    try {
      const { error } = await deleteFeed({
        client: await getClient(),
        path: { feedId: feed.id },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success(`Unsubscribed from "${feed.title}"`);
      router.push("/");
      router.refresh();
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not delete feed"));
    } finally {
      setDeleting(false);
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
      <div className="shrink-0">
        <div className="mx-auto w-full max-w-3xl">
          <PageHeader
            className="static"
            title={feed.title || "Untitled feed"}
            icon={<FeedIcon siteUrl={feed.site_url} className="size-5 shrink-0 rounded-md" />}
            actions={
              <div className="flex items-center gap-1">
                <FeedRenameDialog feed={feed} />
                <ConfirmDialog
                  trigger={
                    <Button variant="ghost" size="icon-sm" disabled={deleting} className="text-muted-foreground hover:text-destructive" aria-label="Unsubscribe from feed">
                      {deleting ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                    </Button>
                  }
                  title="Unsubscribe from feed?"
                  description={`This removes "${feed.title}" and all its entries. This cannot be undone.`}
                  confirmLabel="Unsubscribe"
                  onConfirm={handleDelete}
                />
                {feed.site_url && (
                  <a
                    href={feed.site_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label="Open website"
                    className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                  >
                    <ExternalLink className="size-3.5" />
                  </a>
                )}
                {feed.feed_url && (
                  <a
                    href={feed.feed_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label="Open feed XML"
                    className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                  >
                    <Rss className="size-3.5" />
                  </a>
                )}
              </div>
            }
            metadata={
              <>
                {feed.description && <p>{feed.description}</p>}
                {subscribers !== undefined && (
                  <SubscribersDialog
                    count={subscribers.count}
                    globalCount={subscribers.global_count}
                    subscribers={subscribers.subscribers ?? []}
                  />
                )}
                {feed.parsing_error && (
                  <p className="mt-1 text-destructive">Last parse error: {feed.parsing_error}</p>
                )}
              </>

            }
          />
          <div className="flex items-center gap-2 border-b border-border px-4 py-2 pl-[52px] lg:pl-[48px]">
            <Button variant="outline" size="sm" onClick={handleMarkAllRead} disabled={marking}>
              {marking ? <Loader2 className="animate-spin" /> : <CheckCheck />}
              Mark all as read
            </Button>
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={refreshing}>
              {refreshing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Refresh
            </Button>
            <FolderPickerPopover
              folders={folders}
              currentFolderId={feed.folder_id}
              disabled={movingFolder}
              onSelect={(folderId) => handleMoveFolder(folderId)}
            />
          </div>
        </div>
      </div>
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
