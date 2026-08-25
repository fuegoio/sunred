"use client";

import { Repeat2, Star } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";

/**
 * Per-entry repost (share) and star counts. These are embedded in the
 * /entries response (derived locally on the API from shared_articles and
 * entry_stars), so no secondary fetch is needed.
 *
 * Renders nothing when both counts are zero — the affordance only appears
 * once an article has been reshared or starred, keeping the row quiet
 * otherwise.
 *
 * Pass a `className` to scope the badge group per breakpoint (e.g. hide it
 * on mobile where the metadata row is too dense, and render a separate
 * footer instance there instead).
 */
export function EntryCounts({
  repostCount,
  starCount,
  className,
}: {
  repostCount?: number;
  starCount?: number;
  className?: string;
}) {
  const reposts = repostCount ?? 0;
  const stars = starCount ?? 0;
  if (reposts <= 0 && stars <= 0) return null;

  return (
    <span className={cn("inline-flex items-center gap-2 text-xs text-muted-foreground", className)}>
      {reposts > 0 && (
        <span className="inline-flex items-center gap-0.5" title={`${reposts} repost${reposts === 1 ? "" : "s"}`}>
          <Repeat2 className="size-3.5" />
          {reposts}
        </span>
      )}
      {stars > 0 && (
        <span className="inline-flex items-center gap-0.5" title={`${stars} star${stars === 1 ? "" : "s"}`}>
          <Star className="size-3.5" />
          {stars}
        </span>
      )}
    </span>
  );
}
