"use client";

import { useState, useOptimistic, startTransition } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, MessageSquare, Circle, CheckCircle, Share2 } from "lucide-react";
import { Button, buttonVariants } from "@workspace/ui/components/button";
import { StarToggle } from "@/components/star-toggle";
import { FeedIcon } from "@/components/feed-icon";
import { PageHeader } from "@/components/page-header";
import { getClient, articleShareCount, unwrap, updateEntries } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { cn } from "@workspace/ui/lib/utils";
import type { Entry, Feed } from "@/lib/types";

/**
 * Fetches the globally accurate share count for an article URL from the
 * relay aggregates. Returns null while loading or when the article has no URL.
 */
function useArticleShareCount(articleURL: string | undefined) {
  return useQuery<{ count: number }>({
    queryKey: ["article-share-count", articleURL],
    enabled: !!articleURL,
    queryFn: async () => {
      const data = await unwrap(
        articleShareCount({
          client: await getClient(),
          query: { article_url: articleURL! },
        }),
      );
      return { count: data?.count ?? 0 };
    },
  });
}

/**
 * Reading view for a single entry. Hacker News-style: just the title,
 * metadata, and a short description. No full article body is rendered.
 * Does NOT auto-mark as read on open; the user must explicitly mark or
 * open the link.
 */
export function EntryReader({ entry: initialEntry, feed }: { entry: Entry; feed?: Feed }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState(initialEntry.status);
  const [optimisticStatus, setOptimisticStatus] = useOptimistic(status);
  const [pending, setPending] = useState(false);
  const { data: shareData } = useArticleShareCount(initialEntry.url);
  const shareCount = shareData?.count ?? 0;

  function handleToggleRead() {
    const next = optimisticStatus === "unread" ? "read" : "unread";
    startTransition(() => {
      setOptimisticStatus(next);
      setPending(true);
    });
    void (async () => {
      const { error } = await updateEntries({
        client: await getClient(),
        body: { entry_ids: [initialEntry.id], status: next },
      });
      if (error) {
        toast.error(getApiErrorMessage(error, "Could not update entry"));
        return;
      }
      setStatus(next);
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
    })().finally(() => setPending(false));
  }

  const isUnread = optimisticStatus === "unread";

  return (
    <article className="mx-auto w-full max-w-3xl">
      <PageHeader
        title={initialEntry.title || "Untitled"}
        actions={
          <>
            {initialEntry.url && (
              <a
                href={initialEntry.url}
                target="_blank"
                rel="noopener noreferrer"
                className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
              >
                <ExternalLink className="size-3.5" />
                Open
              </a>
            )}
            {initialEntry.comments_url && (
              <a
                href={initialEntry.comments_url}
                target="_blank"
                rel="noopener noreferrer"
                className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
              >
                <MessageSquare className="size-3.5" />
                Comments
              </a>
            )}
            <Button variant="ghost" size="sm" onClick={handleToggleRead} disabled={pending}>
              {isUnread ? (
                <>
                  <Circle className="size-3.5" />
                  Mark as read
                </>
              ) : (
                <>
                  <CheckCircle className="size-3.5" />
                  Mark unread
                </>
              )}
            </Button>
            <div className="flex items-center gap-0.5">
              <StarToggle entryId={initialEntry.id} starred={initialEntry.starred} size="icon-sm" />
            </div>
          </>
        }
        metadata={
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            {feed && (
              <span className="inline-flex items-center gap-1">
                <FeedIcon siteUrl={feed.site_url} className="size-3.5 rounded-sm" />
                <span className="truncate font-medium text-foreground">{feed.title}</span>
              </span>
            )}
            {feed && (initialEntry.author || initialEntry.published_at) && (
              <span aria-hidden>·</span>
            )}
            {initialEntry.author && <span>{initialEntry.author}</span>}
            {initialEntry.author && initialEntry.published_at && <span aria-hidden>·</span>}
            {initialEntry.published_at && <time>{formatDateTime(initialEntry.published_at)}</time>}
            {initialEntry.url && shareCount > 0 && (
              <>
                <span aria-hidden>·</span>
                <span className="inline-flex items-center gap-1">
                  <Share2 className="size-3.5" />
                  {shareCount} {shareCount === 1 ? "share" : "shares"}
                </span>
              </>
            )}
          </div>
        }
      />

      {initialEntry.tags && initialEntry.tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5 px-4 pt-4">
          {initialEntry.tags.map((tag) => (
            <span
              key={tag}
              className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
            >
              {tag}
            </span>
          ))}
        </div>
      )}

      {initialEntry.description && (
        <p className="px-4 py-4 text-base leading-relaxed text-muted-foreground">
          {initialEntry.description}
        </p>
      )}
    </article>
  );
}
