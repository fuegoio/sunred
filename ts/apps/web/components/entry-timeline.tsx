"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { useInfiniteQuery, useQuery, type InfiniteData } from "@tanstack/react-query";
import { Rss, Sparkles } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { EntryCard } from "@/components/entry-card";
import { EntryCardSkeleton } from "@/components/entry-card-skeleton";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { getClient, listEntries, listFeeds, unwrap } from "@/lib/sunred";
import type { Entry, Feed } from "@/lib/types";
import { buttonVariants } from "@workspace/ui/components/button";
import type { ReactNode } from "react";
import { cn } from "@workspace/ui/lib/utils";

const EASE = [0.25, 1, 0.5, 1] as const;

export type EntryFilter = {
  feed_id?: number;
  folder_id?: number;
  status?: "unread" | "read" | "removed";
  starred?: boolean;
  search?: string;
  source?: "feeds" | "follows";
};

const PAGE_SIZE = 50;

/**
 * Filtered, paginated entry list with infinite scroll. Fetches the user's feeds
 * in parallel so each card can show the owning feed's favicon + title.
 *
 * Pass `emptyVariant="celebration"` for the "all caught up" unread empty state —
 * it renders a lighter, warmer state without a subscribe CTA.
 *
 * Pass `animateExit` on filtered views (unread, starred) so entries that leave
 * the filter after a mutation animate out with a height-collapse + fade instead
 * of disappearing instantly.
 */
export function EntryTimeline({
  filter,
  emptyTitle = "Nothing here yet",
  emptyDescription = "Subscribe to feeds and your latest articles will appear here.",
  emptyVariant = "default",
  emptyAction = "default",
  animateExit = false,
}: {
  filter: EntryFilter;
  emptyTitle?: string;
  emptyDescription?: string;
  emptyVariant?: "default" | "celebration";
  /**
   * CTA rendered in the default empty state. Pass `null` to hide it, or a node
   * to render a custom action. Defaults to a "Subscribe to a feed" link.
   */
  emptyAction?: ReactNode | "default" | null;
  animateExit?: boolean;
}) {
  const { data: feeds } = useQuery<Feed[]>({
    queryKey: ["feeds"],
    queryFn: async () => unwrap(listFeeds({ client: await getClient() })),
  });
  const feedMap = new Map<number, Feed>();
  for (const f of feeds ?? []) feedMap.set(f.id, f);

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading, error, refetch } =
    useInfiniteQuery<Entry[], Error, InfiniteData<Entry[]>, ["entries", EntryFilter], number>({
      queryKey: ["entries", filter],
      queryFn: async ({ pageParam }) => {
        const result = await listEntries({
          client: await getClient(),
          query: { ...filter, limit: PAGE_SIZE, offset: pageParam },
        });
        if (result.error) throw result.error;
        return (result.data ?? []) as Entry[];
      },
      initialPageParam: 0,
      getNextPageParam: (lastPage, _all, lastParam) =>
        lastPage.length < PAGE_SIZE ? undefined : lastParam + PAGE_SIZE,
      refetchInterval: 30_000,
    });

  const sentinelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || !hasNextPage || isFetchingNextPage) return;
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) fetchNextPage();
      },
      { rootMargin: "600px" },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const entries = data?.pages.flat() ?? [];

  if (isLoading) {
    return (
      <div className="divide-y divide-border">
        {Array.from({ length: 8 }).map((_, i) => (
          <EntryCardSkeleton key={i} />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4">
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Rss className="size-6 text-primary" />
            </EmptyMedia>
            <EmptyTitle>Couldn&apos;t load entries</EmptyTitle>
            <EmptyDescription>
              Something went wrong fetching your timeline. Try again.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <button onClick={() => refetch()} className={cn(buttonVariants({ size: "sm" }))}>
              Retry
            </button>
          </EmptyContent>
        </Empty>
      </div>
    );
  }

  if (entries.length === 0) {
    if (emptyVariant === "celebration") {
      return (
        <motion.div
          className="flex flex-col items-center justify-center px-4 py-20 text-center"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: EASE }}
        >
          <motion.div
            className="mb-6 flex size-16 items-center justify-center rounded-full bg-primary/10"
            initial={{ scale: 0.7, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ duration: 0.5, ease: EASE, delay: 0.08 }}
          >
            <motion.div
              animate={{ rotate: [0, -8, 8, -4, 4, 0] }}
              transition={{ duration: 0.7, ease: EASE, delay: 0.3 }}
            >
              <Sparkles className="size-7 text-primary" />
            </motion.div>
          </motion.div>
          <motion.h2
            className="font-serif text-xl font-bold tracking-wide text-foreground"
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, ease: EASE, delay: 0.15 }}
          >
            {emptyTitle}
          </motion.h2>
          <motion.p
            className="mt-2 max-w-xs text-sm/relaxed text-muted-foreground"
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, ease: EASE, delay: 0.22 }}
          >
            {emptyDescription}
          </motion.p>
        </motion.div>
      );
    }

    return (
      <div className="p-4">
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Rss className="size-6 text-primary" />
            </EmptyMedia>
            <EmptyTitle>{emptyTitle}</EmptyTitle>
            <EmptyDescription>{emptyDescription}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {emptyAction === null ? null : emptyAction === "default" ? (
              <Link href="/feeds/new" className={cn(buttonVariants({ size: "sm" }))}>
                Subscribe to a feed
              </Link>
            ) : (
              emptyAction
            )}
          </EmptyContent>
        </Empty>
      </div>
    );
  }

  return (
    <div className="divide-y divide-border">
      {/* No `initial={false}`: the first-page entrance is intentional — it
       * drives the staggered fade-in in EntryCard via `staggerIndex`. Disabling
       * it would suppress the entrance for the whole first page; only newly
       * appended pages (infinite scroll) would animate. */}
      <AnimatePresence>
        {entries.map((entry, i) => (
          <EntryCard
            key={entry.id}
            entry={entry}
            feed={feedMap.get(entry.feed_id)}
            staggerIndex={i}
            animateExit={animateExit}
            shareId={entry.share_id ?? null}
          />
        ))}
      </AnimatePresence>
      <div ref={sentinelRef} className="h-px" />
      {isFetchingNextPage && (
        <div className="divide-y divide-border">
          {Array.from({ length: 3 }).map((_, i) => (
            <EntryCardSkeleton key={i} />
          ))}
        </div>
      )}
    </div>
  );
}
