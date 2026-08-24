import { Menu } from "lucide-react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { buttonVariants } from "@workspace/ui/components/button";
import { EntryCardSkeleton } from "@/components/entry-card-skeleton";
import { cn } from "@workspace/ui/lib/utils";

export default function FeedPreviewLoading() {
  return (
    <div className="mx-auto w-full max-w-3xl">
      {/* Header matching FeedDiscovery's static PageHeader */}
      <div className="border-b border-border bg-background px-4 py-3 lg:pl-[48px]">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <button
              aria-hidden="true"
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon" }),
                "-ml-2 shrink-0 pointer-events-none transition-none lg:hidden",
              )}
            >
              <Menu className="size-4" />
            </button>
            <Skeleton className="size-5 shrink-0 rounded-md" />
            <Skeleton className="h-5 w-48" />
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {/* Subscribe button */}
            <Skeleton className="h-8 w-24 rounded-4xl" />
            <Skeleton className="size-8 rounded-4xl" />
            <Skeleton className="size-8 rounded-4xl" />
          </div>
        </div>
        {/* Metadata: site url + description */}
        <Skeleton className="mt-1.5 ml-[52px] h-[1.25rem] w-56 lg:ml-0" />
      </div>
      {/* Recent articles header */}
      <div className="border-b border-border px-4 py-2.5 pl-[52px] lg:pl-[48px]">
        <Skeleton className="h-3.5 w-28" />
      </div>
      {/* Article skeletons */}
      <div className="divide-y divide-border">
        {Array.from({ length: 6 }).map((_, i) => (
          <EntryCardSkeleton key={i} />
        ))}
      </div>
    </div>
  );
}
