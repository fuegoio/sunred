"use client";

import {
  ExternalLink,
  Trash2,
  Loader2,
  CheckCheck,
  RefreshCw,
  Rss,
} from "lucide-react";
import { FeedRenameDialog } from "@/components/feed-rename-dialog";
import { FolderPickerPopover } from "@/components/folder-picker-popover";
import { SubscribersDialog } from "@/components/subscribers-dialog";
import { SubscribeButton } from "@/components/subscribe-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button, buttonVariants } from "@workspace/ui/components/button";
import { FeedIcon } from "@/components/feed-icon";
import { PageHeader } from "@/components/page-header";
import { cn } from "@workspace/ui/lib/utils";
import type { Feed, Folder, FeedSubscribersResponse } from "@/lib/types";

/**
 * Shared feed header used by both the feed detail page (subscribed) and the
 * discovery view (preview). Renders the PageHeader (title, favicon,
 * open-site/XML links, description) and the toolbar row beneath it.
 *
 * When subscribed, the header actions show a rename button and an icon-only
 * unsubscribe (trash) button backed by a confirm dialog — the management
 * actions (mark all as read, refresh, folder picker) live in the toolbar.
 *
 * In preview mode (`preview`), the rename and management actions are hidden
 * and the Subscribe button takes the toolbar's left side; the subscriber
 * count (when available) always shows on the right, in the same position as
 * the feed page.
 */
export function FeedHeader({
  feed,
  subscribed,
  preview = false,
  subscribers,
  folders,
  onRefresh,
  refreshing,
  onMarkAllRead,
  marking,
  onMoveFolder,
  movingFolder,
  onUnsubscribe,
  unsubscribing,
}: {
  feed: Feed;
  subscribed: boolean;
  preview?: boolean;
  subscribers?: FeedSubscribersResponse;
  folders?: Folder[];
  onRefresh?: () => void;
  refreshing?: boolean;
  onMarkAllRead?: () => void;
  marking?: boolean;
  onMoveFolder?: (folderId: number | undefined) => void;
  movingFolder?: boolean;
  onUnsubscribe?: () => Promise<void> | void;
  unsubscribing?: boolean;
}) {
  return (
    <div className="shrink-0">
      <div className="mx-auto w-full max-w-3xl">
        <PageHeader
          className="static"
          title={feed.title || "Untitled feed"}
          icon={<FeedIcon siteUrl={feed.site_url} className="size-5 shrink-0 rounded-md" />}
          actions={
            <div className="flex items-center gap-1">
              {subscribed && !preview && <FeedRenameDialog feed={feed} />}
              {subscribed && !preview && onUnsubscribe && (
                <ConfirmDialog
                  trigger={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      disabled={unsubscribing}
                      className="text-muted-foreground hover:text-destructive"
                      aria-label="Unsubscribe from feed"
                    >
                      {unsubscribing ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        <Trash2 className="size-3.5" />
                      )}
                    </Button>
                  }
                  title="Unsubscribe from feed?"
                  description={`This removes "${feed.title}" and all its entries. This cannot be undone.`}
                  confirmLabel="Unsubscribe"
                  onConfirm={onUnsubscribe}
                />
              )}
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
              {feed.parsing_error && (
                <p className="mt-1 text-destructive">Last parse error: {feed.parsing_error}</p>
              )}
            </>
          }
        />
        <div className="flex items-center gap-2 border-b border-border px-4 py-2 pl-[52px] lg:pl-[48px]">
          {!preview && onMarkAllRead && (
            <Button variant="outline" size="sm" onClick={onMarkAllRead} disabled={marking}>
              {marking ? <Loader2 className="animate-spin" /> : <CheckCheck />}
              Mark all as read
            </Button>
          )}
          {!preview && onRefresh && (
            <Button variant="outline" size="sm" onClick={onRefresh} disabled={refreshing}>
              {refreshing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Refresh
            </Button>
          )}
          {!preview && folders && onMoveFolder && (
            <FolderPickerPopover
              folders={folders}
              currentFolderId={feed.folder_id}
              disabled={movingFolder}
              onSelect={onMoveFolder}
            />
          )}
          {preview && (
            <SubscribeButton
              feedUrl={feed.feed_url}
              feedTitle={feed.title}
            />
          )}
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
      </div>
    </div>
  );
}
