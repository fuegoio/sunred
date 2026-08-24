"use client";

import { useRouter } from "next/navigation";
import { motion } from "motion/react";
import { FeedIcon } from "@/components/feed-icon";
import { formatRelative, htmlSnippet } from "@/lib/format";
import { cn } from "@workspace/ui/lib/utils";
import type { SharedArticle } from "@/lib/types";

const EASE = [0.25, 1, 0.5, 1] as const;

/**
 * A card for a shared article on the social timeline or profile view.
 * Unlike EntryCard, this renders sharer attribution and uses the shared_at
 * timestamp as the primary date rather than the article publish date.
 */
export function SharedArticleCard({
  article,
  staggerIndex,
  showSharer = true,
}: {
  article: SharedArticle;
  staggerIndex?: number;
  showSharer?: boolean;
}) {
  const router = useRouter();
  const snippet = htmlSnippet(article.description ?? "", 200);

  const sharerLabel = article.sharer_display_name?.trim()
    ? article.sharer_display_name
    : article.sharer_handle
      ? `@${article.sharer_handle}`
      : null;

  return (
    <motion.a
      href={article.article_url}
      target="_blank"
      rel="noopener noreferrer"
      initial={{ opacity: 0, y: 5 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        opacity: { duration: 0.3, ease: EASE },
        y: {
          duration: 0.22,
          ease: EASE,
          delay: Math.min(staggerIndex ?? 0, 8) * 0.03,
        },
      }}
      className="group flex gap-3 px-4 py-3 hover:bg-muted/50"
    >
      {/* Shared-by avatar glyph — replaces the read-dot */}
      <div className="flex size-5 shrink-0 items-start justify-center pt-1">
        <span
          className={cn(
            "flex size-5 items-center justify-center rounded-full text-[9px] font-bold leading-none",
            "bg-primary/10 text-primary",
          )}
          aria-hidden
        >
          {(article.sharer_handle ?? "?").charAt(0).toUpperCase()}
        </span>
      </div>

      <div className="min-w-0 flex-1">
        {/* Feed attribution */}
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <FeedIcon siteUrl={article.feed_site_url} className="size-3.5 rounded-sm" />
          <span className="truncate">{article.feed_title || "Unknown feed"}</span>
          <span aria-hidden>·</span>
          <time className="shrink-0">{formatRelative(article.shared_at)}</time>
        </div>

        {/* Title */}
        <h3 className="mt-1 line-clamp-2 text-sm font-semibold text-foreground">
          {article.title || "Untitled"}
        </h3>

        {/* Snippet */}
        {snippet && (
          <p className="mt-1 line-clamp-2 min-h-[2.5rem] text-sm text-muted-foreground">
            {snippet}
          </p>
        )}

        {/* Sharer attribution */}
        {showSharer && article.sharer_handle && (
          <p className="mt-2 text-xs text-muted-foreground">
            Shared by{" "}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                e.preventDefault();
                router.push(`/users/${article.sharer_handle}`);
              }}
              className="font-medium text-foreground hover:underline"
            >
              {sharerLabel ?? `@${article.sharer_handle}`}
            </button>
          </p>
        )}
      </div>
    </motion.a>
  );
}
