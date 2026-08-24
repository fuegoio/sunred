"use client";

import { ExternalLink, Rss, ChevronRight } from "lucide-react";
import { FeedIcon } from "@/components/feed-icon";
import { SubscribeButton } from "@/components/subscribe-button";
import { PageHeader } from "@/components/page-header";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { buttonVariants } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { formatRelative, htmlSnippet } from "@/lib/format";
import type { PreviewFeedBody } from "@/lib/types";

/**
 * Discovery view for a feed the viewer is not subscribed to. Mirrors the
 * feed detail page's layout (max-w-3xl, PageHeader, ScrollArea) so the two
 * surfaces read as the same chrome — mutualized via the shared
 * `SubscribeButton` in the header actions slot. The body lists the feed's
 * recent articles as external links; subscribing (header button) creates the
 * subscription and navigates to the canonical feed page.
 */
export function FeedDiscovery({ preview }: { preview: PreviewFeedBody }) {
  const items = preview.items ?? [];
  const siteUrl = preview.site_url || undefined;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="shrink-0">
        <div className="mx-auto w-full max-w-3xl">
          <PageHeader
            className="static"
            title={preview.title || "Untitled feed"}
            icon={<FeedIcon siteUrl={siteUrl} className="size-5 shrink-0 rounded-md" />}
            actions={
              <div className="flex items-center gap-1">
                <SubscribeButton
                  feedUrl={preview.feed_url}
                  feedTitle={preview.title}
                  subscribed={false}
                />
                {siteUrl && (
                  <a
                    href={siteUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label="Open website"
                    className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                  >
                    <ExternalLink className="size-3.5" />
                  </a>
                )}
                <a
                  href={preview.feed_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="Open feed XML"
                  className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }))}
                >
                  <Rss className="size-3.5" />
                </a>
              </div>
            }
            metadata={
              <>
                {siteUrl && (
                  <a
                    href={siteUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    <span className="truncate">{siteUrl}</span>
                    <ExternalLink className="size-3 shrink-0" />
                  </a>
                )}
                {preview.description && (
                  <p className={cn(siteUrl && "mt-1", "line-clamp-2 text-sm text-muted-foreground")}>
                    {preview.description}
                  </p>
                )}
              </>
            }
          />
        </div>
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <div className="flex items-center justify-between border-b border-border px-4 py-2.5 pl-[52px] lg:pl-[48px]">
            <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Recent articles
              {items.length > 0 && (
                <span className="ml-1.5 text-muted-foreground/70">({items.length})</span>
              )}
            </h2>
          </div>
          {items.length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted-foreground">
              No articles found in this feed.
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {items.map((item, idx) => (
                <li key={idx}>
                  <a
                    href={item.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="group flex gap-3 px-4 py-3 hover:bg-muted/50 transition-colors"
                  >
                    <span className="mt-1 flex size-5 shrink-0 items-start justify-center pt-1 text-muted-foreground">
                      <ChevronRight className="size-3.5" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="line-clamp-1 text-sm font-medium text-foreground group-hover:text-primary transition-colors">
                          {item.title || "Untitled"}
                        </span>
                        <time className="shrink-0 text-xs text-muted-foreground">
                          {formatRelative(item.published_at)}
                        </time>
                      </div>
                      {item.author && (
                        <p className="text-xs text-muted-foreground">{item.author}</p>
                      )}
                      {item.description && (
                        <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                          {htmlSnippet(item.description, 200)}
                        </p>
                      )}
                    </div>
                  </a>
                </li>
              ))}
            </ul>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
