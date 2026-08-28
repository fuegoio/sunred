"use client";

import { useState, useOptimistic, startTransition } from "react";
import { useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { toast } from "sonner";
import { Star } from "lucide-react";
import { motion, useAnimationControls } from "motion/react";
import { Button } from "@workspace/ui/components/button";
import { getClient, toggleEntryStarred, toggleEntryStarredByUrl } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { cn } from "@workspace/ui/lib/utils";
import type { Entry, Feed } from "@/lib/types";

/**
 * Toggles the starred flag on an entry. Optimistically flips the star
 * immediately and rolls back on error. The star_count is relay-aggregated
 * and eventually consistent, so we do NOT invalidate entries after the
 * mutation — the optimistic star_count stays until the 30s background
 * refetch reconciles it (by then the relay has caught up). Invalidating
 * immediately would clobber the optimistic count with the stale relay
 * value and make it flicker back down.
 *
 * Plays a scale-pop via framer-motion on every toggle for tactile feedback.
 *
 * When `entryId` is 0 (preview mode), uses the URL-based endpoint with
 * article metadata so the star persists without a materialized entry.
 */
export function StarToggle({
  entryId,
  starred: starredProp,
  entry,
  feed,
  size = "icon-sm",
  className,
}: {
  entryId: number;
  starred: boolean;
  /** Full entry for URL-based starring when entryId is 0 (preview mode). */
  entry?: Entry;
  feed?: Feed;
  size?: "icon-xs" | "icon-sm" | "icon";
  className?: string;
}) {
  const queryClient = useQueryClient();
  const [starred, setOptimistic] = useOptimistic(starredProp);
  const [pending, setPending] = useState(false);
  const controls = useAnimationControls();

  const isByUrl = entryId === 0 && entry != null;

  async function handleToggle() {
    const next = !starred;
    // Fire the pop sequence immediately — no state needed
    void controls.start({ scale: [1, 0.72, 1.22, 1], transition: { duration: 0.2, ease: "easeOut" } });
    startTransition(() => {
      setOptimistic(next);
      setPending(true);
    });
    // Optimistically patch star_count alongside `starred` so the inline
    // count next to the star button flips immediately. The base value is
    // captured before the patch so the catch block can revert cleanly.
    const baseCount = entry?.star_count ?? 0;
    const nextCount = Math.max(0, baseCount + (next ? 1 : -1));
    queryClient.setQueriesData<InfiniteData<Entry[]>>({ queryKey: ["entries"] }, (data) => {
      if (!data?.pages) return data;
      return {
        ...data,
        pages: data.pages.map((page) =>
          page.map((e) => (e.id === entryId ? { ...e, starred: next, star_count: nextCount } : e)),
        ),
      };
    });
    try {
      if (isByUrl && entry) {
        const { error } = await toggleEntryStarredByUrl({
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
            starred: next,
          },
        });
        if (error) throw error;
      } else {
        const { error } = await toggleEntryStarred({
          client: await getClient(),
          path: { entryId },
          body: { starred: next },
        });
        if (error) throw error;
      }
    } catch (err) {
      // Revert the optimistic cache patch so the count and star state
      // fall back to the server value.
      queryClient.setQueriesData<InfiniteData<Entry[]>>({ queryKey: ["entries"] }, (data) => {
        if (!data?.pages) return data;
        return {
          ...data,
          pages: data.pages.map((page) =>
            page.map((e) => (e.id === entryId ? { ...e, starred: starredProp, star_count: baseCount } : e)),
          ),
        };
      });
      toast.error(getApiErrorMessage(err, "Could not update entry"));
    } finally {
      setPending(false);
    }
  }

  return (
    <Button
      variant="ghost"
      size={size}
      aria-label={starred ? "Remove from starred" : "Add to starred"}
      aria-pressed={starred}
      disabled={pending}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        handleToggle();
      }}
      className={cn(className)}
    >
      <motion.span animate={controls} className="flex">
        <Star
          className={cn(
            "transition-[color,fill] duration-200",
            starred ? "fill-primary text-primary" : "text-muted-foreground",
          )}
        />
      </motion.span>
    </Button>
  );
}
