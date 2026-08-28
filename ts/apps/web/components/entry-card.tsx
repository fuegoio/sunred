"use client";

import { useState, useOptimistic, startTransition } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { motion } from "motion/react";
import { MessageSquare } from "lucide-react";
import { StarToggle } from "@/components/star-toggle";
import { ShareToggle } from "@/components/share-toggle";
import { FeedIcon } from "@/components/feed-icon";
import { useIsMobile } from "@/hooks/use-mobile";
import { getClient, updateEntries, updateEntryStatusByUrl } from "@/lib/sunred";
import { markReadSession, removeReadSession } from "@/lib/entry-read-session";
import { getApiErrorMessage } from "@/lib/errors";
import { formatRelative, htmlSnippet } from "@/lib/format";
import { cn } from "@workspace/ui/lib/utils";
import type { Entry, Feed } from "@/lib/types";

const EASE = [0.25, 1, 0.5, 1] as const;

/**
 * Patch an entry's status across all cached `["entries"]` queries in place,
 * without refetching. On filtered views (e.g. Unread) this keeps the row
 * visible — dimmed to "read" — after it's marked read, so the user can still
 * star it or open comments before it leaves on the next genuine refetch
 * (navigation, manual refresh, or another article opening).
 */
function patchEntryStatus(queryClient: QueryClient, entryId: number, status: Entry["status"]) {
  queryClient.setQueriesData<InfiniteData<Entry[]>>({ queryKey: ["entries"] }, (data) => {
    if (!data?.pages) return data;
    return {
      ...data,
      pages: data.pages.map((page) => page.map((e) => (e.id === entryId ? { ...e, status } : e))),
    };
  });
}

/**
 * A single entry row in a timeline. The entire row is a link that opens
 * the original article in a new tab (and marks the entry as read). The
 * leading dot toggles read/unread on click. Unread entries get a solid
 * primary dot; read entries show a faded dot on hover so they can be
 * marked unread again.
 *
 * Pass `staggerIndex` (0–7) to stagger the entrance animation on first load.
 * Pass `animateExit` on filtered views (unread, starred) so the row collapses
 * out when it leaves the list after a mutation.
 */
export function EntryCard({
  entry,
  feed,
  staggerIndex,
  animateExit = false,
  shareId = null,
  preview = false,
}: {
  entry: Entry;
  feed?: Feed;
  staggerIndex?: number;
  animateExit?: boolean;
  /** ID of the SharedArticle row if this entry is already shared; null otherwise. */
  shareId?: number | null;
  /**
   * Preview mode for discovery: the row renders as a link with read/unread
   * dot, share, and star actions using URL-based endpoints (no entry ID
   * required). Comments are hidden since preview items don't carry a
   * comments URL. Used by the feed discovery view to show a preview feed's
   * recent articles with the same layout and actions as real entries.
   */
  preview?: boolean;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const [previewRead, setPreviewRead] = useState(entry.status === "read");
  const [readOptimistic, setReadOptimistic] = useOptimistic(entry.status === "read");
  const [pending, setPending] = useState(false);

  const isRead = preview ? previewRead : readOptimistic;
  const unread = !isRead;
  const snippet = htmlSnippet(entry.description, 200);

  // The entry carries its source feed on `entry.feed`, with the viewer's
  // title override already applied by the API. The `feed` prop is the same
  // object for real entries (and a synthetic preview feed in discovery mode),
  // so prefer it and fall back to `entry.feed` defensively.
  const feedTitle = feed?.title || entry.feed?.title || "Unknown feed";
  const feedSiteUrl = feed?.site_url || entry.feed?.site_url;
  // Link target for the feed title: real entries go to their canonical feed
  // page; preview articles (synthetic feed with id 0) go to the discovery
  // view, which previews them and offers a one-click subscribe.
  const feedFeedURL = feed?.feed_url || entry.feed?.feed_url;
  const feedHref =
    feed && feed.id > 0
      ? `/feeds/${feed.id}`
      : feedFeedURL
        ? `/feeds/discover?url=${encodeURIComponent(feedFeedURL)}`
        : null;
  const sharerName = entry.shared_by_name?.trim()
    ? entry.shared_by_name
    : entry.shared_by
      ? `@${entry.shared_by}`
      : null;

  function toggleRead(e: React.MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const current = isRead ? "read" : "unread";
    const next = isRead ? "unread" : "read";
    if (preview) {
      setPreviewRead(next === "read");
      setPending(true);
      void (async () => {
        const { error } = await updateEntryStatusByUrl({
          client: await getClient(),
          body: { article_url: entry.url, status: next },
        });
        if (error) {
          setPreviewRead(current === "read");
          toast.error(getApiErrorMessage(error, "Could not update entry"));
        }
      })().finally(() => setPending(false));
      return;
    }
    startTransition(() => {
      setReadOptimistic(next === "read");
      setPending(true);
    });
    // Manual toggle does not pin the row into the session store — only opening
    // the article does. A manual mark-read should leave the Unread view on the
    // next refetch instead of being merged back as visible. Removing on
    // toggle-unread covers entries previously added by opening the article.
    if (next === "unread") removeReadSession(entry.id);
    // Patch the cache in place so the base value matches the optimistic
    // state. Without this, invalidating would revert useOptimistic to the
    // stale server value mid-refetch and the dot would flash back to its
    // previous state until the refetch lands.
    patchEntryStatus(queryClient, entry.id, next);
    void (async () => {
      const { error } = await updateEntries({
        client: await getClient(),
        body: { entry_ids: [entry.id], status: next },
      });
      if (error) {
        patchEntryStatus(queryClient, entry.id, current);
        toast.error(getApiErrorMessage(error, "Could not update entry"));
        return;
      }
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
    })().finally(() => setPending(false));
  }

  function handleClick(e: React.MouseEvent<HTMLAnchorElement>) {
    if (!entry.url) {
      e.preventDefault();
      return;
    }
    if (!unread) return;
    if (preview) {
      setPreviewRead(true);
      void (async () => {
        const { error } = await updateEntryStatusByUrl({
          client: await getClient(),
          body: { article_url: entry.url, status: "read" },
        });
        if (error) {
          toast.error(getApiErrorMessage(error, "Could not mark as read"));
        }
      })();
      return;
    }
    startTransition(() => setReadOptimistic(true));
    markReadSession(entry);
    void (async () => {
      const { error } = await updateEntries({
        client: await getClient(),
        body: { entry_ids: [entry.id], status: "read" },
      });
      if (error) {
        toast.error(getApiErrorMessage(error, "Could not mark as read"));
        return;
      }
      // Patch the cache in place rather than refetching: refetching the
      // server-filtered Unread list would drop this row immediately, before
      // the user can star it or open comments. It leaves on the next real
      // refetch (navigation, manual refresh, opening another article).
      patchEntryStatus(queryClient, entry.id, "read");
    })();
  }

  const inner = (
    <motion.a
      href={entry.url ?? "#"}
      target={entry.url ? "_blank" : undefined}
      rel={entry.url ? "noopener noreferrer" : undefined}
      onClick={handleClick}
      initial={{ opacity: 0, y: 5 }}
      animate={{ opacity: 1, y: 0 }}
      // "row-hover" variant propagates to child motion elements that declare it.
      whileHover="row-hover"
      transition={{
        opacity: { duration: 0.3, ease: EASE },
        y: { duration: 0.22, ease: EASE, delay: Math.min(staggerIndex ?? 0, 8) * 0.03 },
      }}
      className="group flex flex-col px-4 py-3 hover:bg-muted/50"
    >
      <div className="flex gap-3">
        {preview ? (
          <button
            type="button"
            onClick={toggleRead}
            disabled={pending}
            aria-label={unread ? "Mark as read" : "Mark as unread"}
            aria-pressed={unread}
            className="flex size-5 shrink-0 items-start justify-center pt-1"
          >
            <motion.span
              animate={{
                scale: unread ? 1 : 0.75,
                opacity: unread ? 1 : 0,
                backgroundColor: unread ? "var(--color-primary)" : "var(--color-muted-foreground)",
              }}
              variants={unread ? undefined : { "row-hover": { scale: 1, opacity: 0.4 } }}
              transition={{ duration: 0.15, ease: EASE }}
              className="size-2 rounded-full"
            />
          </button>
        ) : (
          <button
            type="button"
            onClick={toggleRead}
            disabled={pending}
            aria-label={unread ? "Mark as read" : "Mark as unread"}
            aria-pressed={unread}
            className="flex size-5 shrink-0 items-start justify-center pt-1"
          >
            <motion.span
              animate={{
                scale: unread ? 1 : 0.75,
                opacity: unread ? 1 : 0,
                backgroundColor: unread ? "var(--color-primary)" : "var(--color-muted-foreground)",
              }}
              // When the row is hovered and the entry is read, show a ghost dot
              // so users know they can click to mark it unread again.
              variants={unread ? undefined : { "row-hover": { scale: 1, opacity: 0.4 } }}
              transition={{ duration: 0.15, ease: EASE }}
              className="size-2 rounded-full"
            />
          </button>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FeedIcon siteUrl={feedSiteUrl} className="size-3.5 rounded-sm" />
            {feedHref ? (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  router.push(feedHref);
                }}
                className="truncate text-muted-foreground transition-colors hover:text-foreground hover:underline"
              >
                {feedTitle}
              </button>
            ) : (
              <span className="truncate">{feedTitle}</span>
            )}
            {sharerName && entry.shared_by && (
              <>
                <span aria-hidden>·</span>
                <span className="truncate">
                  reposted by{" "}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      e.preventDefault();
                      router.push(`/users/${entry.shared_by}`);
                    }}
                    className="font-medium text-foreground hover:underline"
                  >
                    {sharerName}
                  </button>
                </span>
              </>
            )}
            <span aria-hidden>·</span>
            <time className="shrink-0">{formatRelative(entry.published_at)}</time>
          </div>
          <h3
            className={cn(
              "mt-1 line-clamp-1 text-sm",
              unread ? "font-semibold text-foreground" : "font-medium text-foreground/80",
            )}
          >
            {entry.title || "Untitled"}
          </h3>
          <p
            className={cn(
              "mt-1 line-clamp-4 min-h-[5rem] text-sm sm:line-clamp-2 sm:min-h-[2.5rem]",
              unread ? "text-muted-foreground" : "text-muted-foreground/80",
            )}
          >
            {snippet}
          </p>
        </div>
        <div className="hidden shrink-0 items-start gap-0.5 sm:flex" onClick={(e) => e.stopPropagation()}>
          {entry.comments_url && (
            <button
              type="button"
              onClick={(e) => {
                e.preventDefault();
                window.open(entry.comments_url!, "_blank", "noopener,noreferrer");
              }}
              aria-label="View comments"
              className="flex size-8 items-center justify-center rounded-4xl text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <MessageSquare className="size-3.5" />
            </button>
          )}
        </div>
      </div>
      {/* Footer: each count sits right next to its action button, bottom
       * left of the card. pl-8 (32px = read-dot 20 + gap 12) aligns the
       * actions under the content above. On desktop the comments button
       * stays in its own top-right column; on mobile it moves here, next
       * to the star button, so the top row gives the title and description
       * the full width. Counts render only when > 0 so the footer stays
       * quiet for entries with no social signal. */}
      <div
        className="mt-2 flex items-center justify-start gap-0.5 pl-7"
        onClick={(e) => e.stopPropagation()}
      >
        <ShareToggle entry={entry} feed={feed} shareId={shareId} size="icon-sm" />
        {(entry.repost_count ?? 0) > 0 && (
          <span className="text-sm text-muted-foreground">{entry.repost_count}</span>
        )}
        <StarToggle
          entryId={entry.id}
          starred={entry.starred}
          entry={entry}
          feed={feed}
          size="icon-sm"
          className="ml-2"
        />
        {(entry.star_count ?? 0) > 0 && (
          <span className="text-sm text-muted-foreground">{entry.star_count}</span>
        )}
        {entry.comments_url && (
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              window.open(entry.comments_url!, "_blank", "noopener,noreferrer");
            }}
            aria-label="View comments"
            className="ml-2 flex size-8 items-center justify-center rounded-4xl text-muted-foreground hover:bg-muted hover:text-foreground sm:hidden"
          >
            <MessageSquare className="size-3.5" />
          </button>
        )}
      </div>
    </motion.a>
  );

  if (!animateExit) return inner;

  // On filtered views (unread, starred) wrap in a container that animates
  // both height and opacity simultaneously so the card collapses while fading
  // and siblings translate up in the same motion. The fixed height matches
  // the card's natural row height: a top row (dot + metadata + title +
  // snippet — 4 snippet lines on mobile, 2 on >= sm) plus a bottom footer
  // row (counts + repost/star, ~40px). The footer is always present now, so
  // the height no longer depends on whether the entry has social counts.
  const rowHeight = isMobile ? 188 : 148;
  return (
    <motion.div
      initial={{ height: rowHeight, opacity: 0 }}
      animate={{ height: rowHeight, opacity: 1 }}
      exit={{ height: 0, opacity: 0 }}
      transition={{ duration: 0.25, ease: EASE }}
      style={{ overflow: "hidden" }}
    >
      {inner}
    </motion.div>
  );
}
