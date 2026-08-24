"use client";

import { useState, useOptimistic, startTransition } from "react";
import Link from "next/link";
import { useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { motion } from "motion/react";
import { MessageSquare, Plus } from "lucide-react";
import { StarToggle } from "@/components/star-toggle";
import { ShareToggle } from "@/components/share-toggle";
import { FeedIcon } from "@/components/feed-icon";
import { useIsMobile } from "@/hooks/use-mobile";
import { getClient, updateEntries, createFeed } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { formatRelative, htmlSnippet } from "@/lib/format";
import { cn } from "@workspace/ui/lib/utils";
import { buttonVariants } from "@workspace/ui/components/button";
import type { Entry, Feed } from "@/lib/types";

const EASE = [0.25, 1, 0.5, 1] as const;

/**
 * Patch an entry's status across all cached `["entries"]` queries in place,
 * without refetching. On filtered views (e.g. Unread) this keeps the row
 * visible — dimmed to "read" — after it's marked read, so the user can still
 * star it or open comments before it leaves on the next genuine refetch
 * (navigation, manual refresh, or another article opening).
 */
function patchEntryStatus(
  queryClient: QueryClient,
  entryId: number,
  status: Entry["status"],
) {
  queryClient.setQueriesData<InfiniteData<Entry[]>>({ queryKey: ["entries"] }, (data) => {
    if (!data?.pages) return data;
    return {
      ...data,
      pages: data.pages.map((page) =>
        page.map((e) => (e.id === entryId ? { ...e, status } : e)),
      ),
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
}: {
  entry: Entry;
  feed?: Feed;
  staggerIndex?: number;
  animateExit?: boolean;
  /** ID of the SharedArticle row if this entry is already shared; null otherwise. */
  shareId?: number | null;
}) {
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const [readOptimistic, setReadOptimistic] = useOptimistic(entry.status === "read");
  const [pending, setPending] = useState(false);
  // Tracks a subscribe action performed from this card; the feed prop may
  // arrive after mount (feeds load in parallel with entries), so derive
  // subscription from the prop and let a local override flip it on subscribe.
  const [subscribedOverride, setSubscribedOverride] = useState(false);

  const unread = !readOptimistic;
  const snippet = htmlSnippet(entry.description, 200);

  // The entry carries a nested global source feed (entry.feed) so shares from
  // feeds the user doesn't subscribe to can still render their source. The
  // passed `feed` prop (from the user's subscription list) carries the user's
  // title override, so prefer it for subscribed feeds; fall back to the nested
  // global feed for shares from unsubscribed feeds.
  const feedTitle = feed?.title || entry.feed?.title || "Unknown feed";
  const feedSiteUrl = feed?.site_url || entry.feed?.site_url;
  // Link target for the feed title: subscribed feeds go to their canonical
  // feed page; feeds the viewer doesn't subscribe to (shares from followed
  // users) go to the discovery view, which previews them and offers a
  // one-click subscribe.
  const feedHref = feed
    ? `/feeds/${feed.id}`
    : entry.feed?.feed_url
      ? `/feeds/preview?url=${encodeURIComponent(entry.feed.feed_url)}`
      : null;
  const sharerName = entry.shared_by_name?.trim()
    ? entry.shared_by_name
    : entry.shared_by
      ? `@${entry.shared_by}`
      : null;
  // The entry's feed is not subscribed when no `feed` prop was resolved by the
  // timeline (i.e. feed_id is absent from the user's subscriptions). A share
  // from a followed user surfaces such entries; offer a one-click subscribe.
  const canSubscribe = !subscribedOverride && !feed && Boolean(entry.feed?.feed_url);

  async function handleSubscribe(e: React.MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const feedURL = entry.feed?.feed_url;
    if (!feedURL) return;
    setPending(true);
    try {
      const { error } = await createFeed({
        client: await getClient(),
        body: { feed_url: feedURL },
      });
      if (error) throw error;
      setSubscribedOverride(true);
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      toast.success(`Subscribed to "${entry.feed?.title || "feed"}"`);
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not subscribe to feed"));
    } finally {
      setPending(false);
    }
  }

  function toggleRead(e: React.MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const next = readOptimistic ? "unread" : "read";
    startTransition(() => {
      setReadOptimistic(next === "read");
      setPending(true);
    });
    void (async () => {
      const { error } = await updateEntries({
        client: await getClient(),
        body: { entry_ids: [entry.id], status: next },
      });
      if (error) {
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
    if (unread) {
      startTransition(() => setReadOptimistic(true));
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
  }

  const inner = (
    <motion.a
      href={entry.url ?? "#"}
      target={entry.url ? "_blank" : undefined}
      rel={entry.url ? "noopener noreferrer" : undefined}
      onClick={handleClick}
      initial={{ opacity: 0, y: 5 }}
      animate={{ opacity: unread ? 1 : 0.6, y: 0 }}
      // "row-hover" variant propagates to child motion elements that declare it.
      whileHover="row-hover"
      transition={{
        opacity: { duration: 0.3, ease: EASE },
        y: { duration: 0.22, ease: EASE, delay: Math.min(staggerIndex ?? 0, 8) * 0.03 },
      }}
      className="group flex gap-3 px-4 py-3 hover:bg-muted/50"
    >
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
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <FeedIcon siteUrl={feedSiteUrl} className="size-3.5 rounded-sm" />
          {feedHref ? (
            <Link
              href={feedHref}
              onClick={(e) => e.stopPropagation()}
              className="truncate text-muted-foreground transition-colors hover:text-foreground hover:underline"
            >
              {feedTitle}
            </Link>
          ) : (
            <span className="truncate">{feedTitle}</span>
          )}
          {sharerName && entry.shared_by && (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">
                shared by{" "}
                <Link
                  href={`/users/${entry.shared_by}`}
                  onClick={(e) => e.stopPropagation()}
                  className="font-medium text-foreground hover:underline"
                >
                  {sharerName}
                </Link>
              </span>
            </>
          )}
          <span aria-hidden>·</span>
          <time className="shrink-0">{formatRelative(entry.published_at)}</time>
          {canSubscribe && (
            <button
              type="button"
              onClick={handleSubscribe}
              disabled={pending}
              aria-label={`Subscribe to ${entry.feed?.title || "this feed"}`}
              className={cn(
                buttonVariants({ variant: "outline", size: "xs" }),
                "ml-auto h-5 shrink-0 gap-0.5 px-1.5 text-[11px]",
              )}
            >
              <Plus className="size-3" />
              Subscribe
            </button>
          )}
        </div>
        <h3
          className={cn(
            "mt-1 line-clamp-2 text-sm",
            unread ? "font-semibold text-foreground" : "font-medium",
          )}
        >
          {entry.title || "Untitled"}
        </h3>
        <p className="mt-1 line-clamp-4 min-h-[5rem] text-sm text-muted-foreground sm:line-clamp-2 sm:min-h-[2.5rem]">
          {snippet}
        </p>
      </div>
      <div className="flex shrink-0 items-start gap-0.5" onClick={(e) => e.stopPropagation()}>
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
        <ShareToggle entry={entry} feed={feed} shareId={shareId} size="icon-sm" />
        <StarToggle entryId={entry.id} starred={entry.starred} size="icon-sm" />
      </div>
    </motion.a>
  );

  if (!animateExit) return inner;

  // On filtered views (unread, starred) wrap in a container that animates
  // both height and opacity simultaneously so the card collapses while fading
  // and siblings translate up in the same motion. The fixed height matches
  // the card's natural row height: 4 snippet lines on mobile, 2 on >= sm.
  const rowHeight = isMobile ? 168 : 108;
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
