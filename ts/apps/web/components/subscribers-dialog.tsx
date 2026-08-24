"use client";

import { useState, useDeferredValue, useMemo } from "react";
import Link from "next/link";
import { Search, X, ChevronLeft, ChevronRight, Users } from "lucide-react";
import { Avatar } from "@base-ui/react/avatar";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@workspace/ui/components/dialog";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import type { UserProfile } from "@/lib/types";

const PAGE_SIZE = 10;

function displayName(p: UserProfile): string {
  return p.display_name?.trim() || `@${p.handle}`;
}

function highlight(text: string, query: string) {
  const q = query.trim().toLowerCase();
  if (!q) return text;
  const idx = text.toLowerCase().indexOf(q);
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-transparent font-semibold text-foreground">{text.slice(idx, idx + q.length)}</mark>
      {text.slice(idx + q.length)}
    </>
  );
}

function SubscriberRow({ profile, query }: { profile: UserProfile; query: string }) {
  return (
    <Link
      href={`/users/${profile.handle}`}
      className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-muted/40"
    >
      <Avatar.Root className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary text-sm font-medium text-primary-foreground select-none">
        <Avatar.Fallback>{displayName(profile).charAt(0).toUpperCase()}</Avatar.Fallback>
      </Avatar.Root>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium leading-tight">{highlight(displayName(profile), query)}</p>
        <p className="truncate text-xs leading-tight text-muted-foreground">@{highlight(profile.handle, query)}</p>
      </div>
    </Link>
  );
}

/**
 * Dialog listing a feed's subscribers with a search filter and client-side
 * pagination. The feedSubscribers endpoint returns the full local list in one
 * response, so filtering and paging happen in the browser. The federated
 * (global) count is shown alongside the local count when a relay is configured.
 */
export function SubscribersDialog({
  count,
  globalCount,
  subscribers,
}: {
  count: number;
  globalCount: number;
  subscribers: UserProfile[];
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(0);
  const deferred = useDeferredValue(q);
  const trimmed = deferred.trim().toLowerCase();

  const total = globalCount > 0 ? globalCount : count;

  const filtered = useMemo(() => {
    if (!trimmed) return subscribers;
    return subscribers.filter(
      (p) =>
        p.handle.toLowerCase().includes(trimmed) ||
        (p.display_name ?? "").toLowerCase().includes(trimmed),
    );
  }, [subscribers, trimmed]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount - 1);
  const pageStart = safePage * PAGE_SIZE;
  const pageItems = filtered.slice(pageStart, pageStart + PAGE_SIZE);

  function handleQueryChange(next: string) {
    setQ(next);
    setPage(0);
  }

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      setQ("");
      setPage(0);
    }
  }

  const showEmpty = trimmed.length >= 1 && filtered.length === 0;
  const showPagination = pageCount > 1;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <button
            type="button"
            className="flex items-center gap-1.5 rounded-full px-2.5 py-1 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <Users className="size-4" />
            <span className="tabular-nums font-medium text-foreground">{total}</span>
            <span>{total === 1 ? "subscriber" : "subscribers"}</span>
            {globalCount > 0 && globalCount !== count && (
              <span className="text-muted-foreground/70">&middot; {count} here</span>
            )}
          </button>
        }
      />
      <DialogContent className="max-w-md gap-0 p-0">
        <DialogHeader className="px-4 pt-4">
          <DialogTitle>Subscribers</DialogTitle>
          <DialogDescription>
            {globalCount > 0 && globalCount !== count
              ? `${globalCount} across the fediverse · ${count} on this instance`
              : `People on this instance who subscribe to this feed.`}
          </DialogDescription>
        </DialogHeader>

        <div className="px-4 pt-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={q}
              onChange={(e) => handleQueryChange(e.target.value)}
              placeholder="handle or name…"
              aria-label="Filter subscribers"
              autoFocus
              className="pl-9 pr-9"
            />
            {q && (
              <button
                type="button"
                onClick={() => handleQueryChange("")}
                aria-label="Clear search"
                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
        </div>

        <div className="mt-2 min-h-20 pb-2">
          {subscribers.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-4 py-8 text-center text-sm text-muted-foreground">
              <Users className="size-6 text-muted-foreground/60" />
              <p>No local subscribers yet.</p>
            </div>
          ) : showEmpty ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              No subscribers match &ldquo;{trimmed}&rdquo;.
            </p>
          ) : (
            <div className="flex flex-col">
              {pageItems.map((p) => (
                <SubscriberRow key={p.user_id} profile={p} query={trimmed} />
              ))}
            </div>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-border px-4 py-3">
          <span className="text-xs text-muted-foreground">
            {trimmed.length >= 1
              ? filtered.length === 0
                ? "\u00A0"
                : `${filtered.length} ${filtered.length === 1 ? "match" : "matches"}`
              : `${subscribers.length} ${subscribers.length === 1 ? "subscriber" : "subscribers"}`}
          </span>
          {showPagination && (
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="icon-sm"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={safePage === 0}
                aria-label="Previous page"
              >
                <ChevronLeft className="size-4" />
              </Button>
              <span className="px-1 text-xs tabular-nums text-muted-foreground" aria-live="polite">
                {safePage + 1} / {pageCount}
              </span>
              <Button
                variant="outline"
                size="icon-sm"
                onClick={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
                disabled={safePage >= pageCount - 1}
                aria-label="Next page"
              >
                <ChevronRight className="size-4" />
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
