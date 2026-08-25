import { Skeleton } from "@workspace/ui/components/skeleton";

/**
 * Loading placeholder that mirrors the EntryCard layout: read/unread dot,
 * metadata row (favicon + feed title + time), two-line title, two-line
 * snippet, and a right-side action button. Element heights match the real
 * card's line-heights (text-xs = 16px, text-sm = 20px) so borders don't
 * jump when data replaces the skeleton.
 */
export function EntryCardSkeleton() {
  return (
    <div className="flex gap-3 px-4 py-3">
      {/* read/unread dot */}
      <div className="flex size-5 shrink-0 items-start justify-center pt-1">
        <Skeleton className="size-2 rounded-full" />
      </div>
      {/* content */}
      <div className="min-w-0 flex-1">
        {/* metadata row (text-xs, line-height 1rem) */}
        <div className="flex items-center gap-2">
          <Skeleton className="size-3.5 shrink-0 rounded-sm" />
          <Skeleton className="h-4 w-24" />
          <span aria-hidden className="text-xs text-muted-foreground">
            ·
          </span>
          <Skeleton className="h-4 w-12" />
        </div>
        {/* title (text-sm line-clamp-1) */}
        <Skeleton className="mt-1 h-5 w-3/4" />
        {/* snippet (text-sm line-clamp-2 on >= sm, line-clamp-4 on mobile) */}
        <Skeleton className="mt-1 h-4 w-full" />
        <Skeleton className="mt-1 h-4 w-2/3 mb-1" />
        <Skeleton className="mt-1 h-4 w-full sm:hidden" />
        <Skeleton className="mt-1 h-4 w-1/2 mb-1 sm:hidden" />
      </div>
      {/* right action */}
      <div className="flex shrink-0 items-start gap-0.5">
        <Skeleton className="size-8 rounded-md" />
      </div>
    </div>
  );
}
