import { Menu, ExternalLink, Rss } from "lucide-react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { buttonVariants } from "@workspace/ui/components/button";
import { EntryCardSkeleton } from "@/components/entry-card-skeleton";
import { cn } from "@workspace/ui/lib/utils";

export default function DiscoverFeedLoading() {
  return (
    <div className="mx-auto w-full max-w-3xl">
      {/* Header matching FeedHeader's PageHeader */}
      <div className="sticky top-0 z-10 border-b border-border bg-background px-4 py-3">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            {/* Menu button — mobile only, matches PageHeader */}
            <button
              aria-hidden="true"
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon" }),
                "-ml-2 shrink-0 pointer-events-none transition-none lg:hidden",
              )}
            >
              <Menu className="size-4" />
            </button>
            {/* Feed favicon placeholder */}
            <Skeleton className="size-5 shrink-0 rounded-md" />
            {/* Feed title */}
            <Skeleton className="h-5 w-40" />
          </div>
          {/* Actions: external link, rss (preview mode hides rename/trash) */}
          <div className="flex shrink-0 items-center gap-1">
            <button
              aria-hidden="true"
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon-sm" }),
                "pointer-events-none text-muted-foreground",
              )}
            >
              <ExternalLink className="size-3.5" />
            </button>
            <button
              aria-hidden="true"
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon-sm" }),
                "pointer-events-none text-muted-foreground",
              )}
            >
              <Rss className="size-3.5" />
            </button>
          </div>
        </div>
        {/* Description line — matches PageHeader's min-h-[1.25rem] metadata div */}
        <Skeleton className="mt-1.5 h-[1.25rem] w-56 ml-[52px] lg:ml-0" />
      </div>
      {/* Toolbar: Subscribe button (left) + subscriber count (right) */}
      <div className="flex items-center gap-1.5 border-b border-border px-4 py-2 pl-[52px] sm:gap-2 lg:pl-[48px]">
        <Skeleton className="h-8 w-24 rounded-md" />
        <div className="ml-auto">
          <Skeleton className="h-8 w-20 rounded-md" />
        </div>
      </div>
      {/* Entry skeletons */}
      <div className="flex flex-col divide-y divide-border">
        {Array.from({ length: 6 }).map((_, i) => (
          <EntryCardSkeleton key={i} />
        ))}
      </div>
    </div>
  );
}
