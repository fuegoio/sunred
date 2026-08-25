"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Repeat } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { getClient, shareArticle, unshareArticle } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { cn } from "@workspace/ui/lib/utils";
import type { Entry, Feed } from "@/lib/types";

/**
 * Toggles reposting of an entry to the user's social timeline.
 * On first repost, sends metadata to /api/v1/social/shares.
 * On unrepost, calls DELETE /api/v1/social/shares/{shareId}.
 *
 * `shareId` is null when the article is not yet reposted; after a successful
 * repost it becomes the server-assigned id so subsequent clicks unrepost.
 */
export function ShareToggle({
  entry,
  feed,
  shareId: initialShareId,
  size = "icon-sm",
  className,
}: {
  entry: Entry;
  feed?: Feed;
  shareId: number | null;
  size?: "icon-xs" | "icon-sm" | "icon";
  className?: string;
}) {
  const [shareId, setShareId] = useState<number | null>(initialShareId);
  const [pending, setPending] = useState(false);

  const shared = shareId !== null;

  async function handleToggle() {
    setPending(true);
    try {
      if (shared && shareId !== null) {
        const { error } = await unshareArticle({
          client: await getClient(),
          path: { shareId },
        });
        if (error) throw error;
        setShareId(null);
        toast.success("Removed from your reposts");
      } else {
        const { data, error } = await shareArticle({
          client: await getClient(),
          body: {
            article_url: entry.url,
            title: entry.title,
            description: entry.description ?? "",
            feed_url: feed?.feed_url ?? entry.feed?.feed_url ?? "",
            feed_title: feed?.title ?? entry.feed?.title ?? "",
            feed_site_url: feed?.site_url ?? entry.feed?.site_url ?? "",
            author: entry.author ?? "",
            published_at: entry.published_at ?? undefined,
          },
        });
        if (error) throw error;
        setShareId(data?.id ?? null);
        toast.success("Reposted to your timeline");
      }
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not update repost"));
    } finally {
      setPending(false);
    }
  }

  return (
    <Button
      variant="ghost"
      size={size}
      aria-label={shared ? "Remove from your reposts" : "Repost this article"}
      aria-pressed={shared}
      disabled={pending}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        handleToggle();
      }}
      className={cn(className)}
    >
      <Repeat
        className={cn(
          "transition-[color,fill] duration-200",
          shared ? "text-primary" : "text-muted-foreground",
        )}
      />
    </Button>
  );
}
