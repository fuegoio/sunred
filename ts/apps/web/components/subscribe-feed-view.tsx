"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Rss, ExternalLink, Plus } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Logo } from "@/components/logo";
import { getClient, previewFeed, createFeed } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { subscribeFeedSchema, normalizeFeedURL } from "@/lib/schemas";
import type { PreviewFeedBody } from "@/lib/types";

/**
 * Client view for the subscribe page. Takes an optional `initialUrl` (from
 * the `?url=` query param) so other views can deep-link here with the URL
 * prefilled — the debounced value starts at the same URL so the preview
 * fetch kicks off immediately on mount.
 */
export function SubscribeFeedView({ initialUrl = "" }: { initialUrl?: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [url, setUrl] = useState(initialUrl);
  const [debouncedUrl, setDebouncedUrl] = useState(initialUrl);
  const [subscribing, setSubscribing] = useState(false);

  // Debounce the URL so we only fetch after the user pauses typing.
  useEffect(() => {
    const t = setTimeout(() => setDebouncedUrl(url), 400);
    return () => clearTimeout(t);
  }, [url]);

  const urlValidation = subscribeFeedSchema.safeParse({
    feed_url: debouncedUrl,
  });
  const isUrlValid = urlValidation.success;
  const normalizedUrl = isUrlValid ? normalizeFeedURL(debouncedUrl) : "";

  const {
    data: preview,
    error: previewError,
    isFetching: isPreviewFetching,
  } = useQuery<PreviewFeedBody>({
    queryKey: ["previewFeed", normalizedUrl],
    queryFn: async () => {
      const result = await previewFeed({
        client: await getClient(),
        body: { feed_url: normalizedUrl },
      });
      if (result.error) throw result.error;
      return result.data as PreviewFeedBody;
    },
    enabled: isUrlValid,
    retry: false,
  });

  async function handleSubscribe() {
    if (!preview) return;
    setSubscribing(true);
    try {
      const { error } = await createFeed({
        client: await getClient(),
        body: {
          feed_url: preview.feed_url,
        },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success(`Subscribed to "${preview.title}"`);
      router.push("/");
      router.refresh();
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not subscribe to feed"));
      setSubscribing(false);
    }
  }

  const showLoading = isUrlValid && isPreviewFetching && !preview;
  const showError = isUrlValid && !isPreviewFetching && previewError && !preview;
  const showPreview = !!preview;

  return (
    <div className="flex h-full w-full flex-col">
      {/* Scrollable body */}
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-2xl px-4 py-8 sm:px-6 sm:py-12">
          <div className="flex flex-col gap-8">
            {/* Header + URL input — always visible */}
            <div className="flex flex-col gap-3">
              <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10">
                <Logo className="size-7" />
              </div>
              <h1 className="font-serif text-2xl font-bold tracking-normal">Subscribe to a feed</h1>
              <p className="max-w-md text-sm text-muted-foreground">
                Paste any website URL or feed link. We&apos;ll discover the RSS feed automatically and
                show you what you&apos;ll get before you subscribe.
              </p>
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="feed_url">Website or feed URL</Label>
              <Input
                id="feed_url"
                type="url"
                placeholder="https://example.com"
                autoComplete="url"
                spellCheck={false}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                aria-invalid={!isUrlValid && debouncedUrl.length > 0}
              />
              {!isUrlValid && debouncedUrl.length > 0 && (
                <p className="text-sm text-destructive">Enter a valid URL</p>
              )}
            </div>

            {/* Live states below the input */}
            {showLoading && (
              <div className="flex flex-col items-center gap-4 py-8">
                <Loader2 className="size-6 animate-spin text-muted-foreground" />
                <p className="text-sm text-muted-foreground">Fetching and parsing feed...</p>
              </div>
            )}

            {showError && (
              <div
                role="alert"
                className="rounded-md border border-destructive/50 bg-destructive/10 p-4 text-foreground"
              >
                <div className="flex items-center gap-2 font-medium">
                  <span className="text-destructive">Could not fetch feed</span>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">
                  {getApiErrorMessage(previewError, "Check the URL and try again")}
                </p>
              </div>
            )}

            {showPreview && !subscribing && (
              <PreviewContent preview={preview} />
            )}

            {showPreview && subscribing && (
              <div className="flex flex-col items-center gap-4 py-8">
                <Loader2 className="size-6 animate-spin text-primary" />
                <p className="text-sm text-muted-foreground">
                  Subscribing to &ldquo;{preview.title || preview.feed_url}&rdquo;...
                </p>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Subscribe button — pinned to the bottom */}
      {showPreview && !subscribing && (
        <div className="shrink-0 border-t border-border bg-background px-4 py-4 sm:px-6">
          <div className="mx-auto w-full max-w-2xl">
            <Button onClick={handleSubscribe} size="lg" className="w-full">
              <Plus className="size-4" />
              Subscribe to this feed
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function PreviewContent({ preview }: { preview: PreviewFeedBody }) {
  const items = preview.items ?? [];

  return (
    <div className="flex flex-col gap-6">
      {/* Feed header */}
      <div className="flex items-start gap-4">
        {preview.favicon_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={preview.favicon_url}
            alt=""
            className="size-10 shrink-0 rounded-lg"
            width={40}
            height={40}
          />
        ) : (
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted">
            <Rss className="size-5 text-muted-foreground" />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-serif text-xl font-bold tracking-normal">
            {preview.title || "Untitled feed"}
          </h2>
          {preview.site_url && (
            <a
              href={preview.site_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <span className="truncate">{preview.site_url}</span>
              <ExternalLink className="size-3 shrink-0" />
            </a>
          )}
          {preview.description && (
            <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{preview.description}</p>
          )}
        </div>
      </div>

      {/* Articles preview */}
      <div className="rounded-lg border border-border">
        <div className="border-b border-border px-4 py-2.5">
          <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Recent articles
            {items.length > 0 && (
              <span className="ml-1.5 text-muted-foreground/70">({items.length})</span>
            )}
          </h3>
        </div>
        {items.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted-foreground">No articles found in this feed.</p>
        ) : (
          <ul className="divide-y divide-border">
            {items.map((item, idx) => (
              <li key={idx} className="px-4 py-3">
                <div className="flex flex-col gap-1">
                  <div className="flex items-baseline justify-between gap-3">
                    <a
                      href={item.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="line-clamp-1 text-sm font-medium hover:text-primary transition-colors"
                    >
                      {item.title || "Untitled"}
                    </a>
                    <time className="shrink-0 text-xs text-muted-foreground">
                      {formatDate(item.published_at)}
                    </time>
                  </div>
                  {item.author && <p className="text-xs text-muted-foreground">{item.author}</p>}
                  {item.description && (
                    <p className="line-clamp-2 text-sm text-muted-foreground">{item.description}</p>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

    </div>
  );
}

// --- Helpers ---

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return "";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}
