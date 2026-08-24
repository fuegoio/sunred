"use client";

import { useState, useDeferredValue } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Search, Loader2, UserPlus, UserCheck, X } from "lucide-react";
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
import { Skeleton } from "@workspace/ui/components/skeleton";
import { UserAvatar } from "@/components/user-avatar";
import { getClient, searchUsers, followUser, unfollowUser, unwrap } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import type { UserProfile } from "@/lib/types";

function displayName(p: UserProfile): string {
  return p.display_name?.trim() || `@${p.handle}`;
}

function SearchRow({
  profile,
  query,
  onToggle,
}: {
  profile: UserProfile;
  query: string;
  onToggle: (handle: string, next: boolean) => void;
}) {
  const [pending, setPending] = useState(false);
  const isFollowing = profile.is_following ?? false;

  async function handleClick() {
    setPending(true);
    try {
      if (isFollowing) {
        const { error } = await unfollowUser({
          client: await getClient(),
          path: { handle: profile.handle },
        });
        if (error) throw error;
      } else {
        const { error } = await followUser({
          client: await getClient(),
          path: { handle: profile.handle },
        });
        if (error) throw error;
      }
      onToggle(profile.handle, !isFollowing);
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not update follow"));
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="group flex items-center gap-3 px-4 py-2.5">
      <Link
        href={`/users/${profile.handle}`}
        className="flex min-w-0 flex-1 items-center gap-3"
      >
        <UserAvatar
          displayName={profile.display_name}
          handle={profile.handle}
          className="size-9 text-sm font-medium"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-tight">
            {highlight(displayName(profile), query)}
          </p>
          <p className="truncate text-xs text-muted-foreground leading-tight">
            @{highlight(profile.handle, query)}
          </p>
        </div>
      </Link>
      <Button
        variant={isFollowing ? "outline" : "default"}
        size="sm"
        onClick={handleClick}
        disabled={pending}
        className="h-7 shrink-0 gap-1 px-2 text-xs"
      >
        {pending ? (
          <Loader2 className="size-3 animate-spin" />
        ) : isFollowing ? (
          <>
            <UserCheck className="size-3" />
            Following
          </>
        ) : (
          <>
            <UserPlus className="size-3" />
            Follow
          </>
        )}
      </Button>
    </div>
  );
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

/** Bluesky-style find-people dialog: type a handle or name, follow from the results. */
export function FollowSearchDialog() {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const deferred = useDeferredValue(q);
  const trimmed = deferred.trim();

  const { data: results, isLoading } = useQuery<UserProfile[]>({
    queryKey: ["user-search", trimmed],
    queryFn: async () =>
      unwrap(searchUsers({ client: await getClient(), query: { q: trimmed, limit: 20 } })),
    enabled: open && trimmed.length >= 1,
    placeholderData: (prev) => prev,
    staleTime: 30_000,
  });

  function handleToggle(handle: string, next: boolean) {
    // Optimistically flip is_following in the visible search results.
    queryClient.setQueryData<UserProfile[]>(["user-search", trimmed], (old) =>
      (old ?? []).map((p) =>
        p.handle === handle
          ? {
              ...p,
              is_following: next,
              follower_count: p.follower_count + (next ? 1 : -1),
            }
          : p,
      ),
    );
    // The followed-users sidebar list and the social page read this.
    void queryClient.invalidateQueries({ queryKey: ["following"] });
  }

  const showEmpty = trimmed.length >= 1 && !isLoading && (results ?? []).length === 0;

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) setQ(""); }}>
      <DialogTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Find people to follow"
            className="text-muted-foreground hover:text-foreground"
          >
            <UserPlus className="size-3.5" />
          </Button>
        }
      />
      <DialogContent className="max-w-md gap-0 p-0">
        <DialogHeader className="px-4 pt-4">
          <DialogTitle>Find people</DialogTitle>
          <DialogDescription>Search by handle or name to follow them.</DialogDescription>
        </DialogHeader>

        <div className="px-4 pt-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="handle or name…"
              aria-label="Search users"
              autoFocus
              className="pl-9 pr-9"
            />
            {q && (
              <button
                type="button"
                onClick={() => setQ("")}
                aria-label="Clear search"
                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
        </div>

        <div className="mt-2 max-h-80 overflow-y-auto pb-2">
          {trimmed.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">
              Start typing to find people you might know.
            </p>
          ) : isLoading ? (
            <div className="flex flex-col gap-1 px-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 px-2 py-2.5">
                  <Skeleton className="size-9 shrink-0 rounded-full" />
                  <div className="flex-1">
                    <Skeleton className="h-3.5 w-32" />
                    <Skeleton className="mt-1.5 h-3 w-24" />
                  </div>
                  <Skeleton className="h-7 w-16 rounded-md" />
                </div>
              ))}
            </div>
          ) : showEmpty ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">
              No users match &ldquo;{trimmed}&rdquo;.
            </p>
          ) : (
            <div className="flex flex-col">
              {(results ?? []).map((p) => (
                <div key={p.user_id} className="hover:bg-muted/40 transition-colors">
                  <SearchRow profile={p} query={trimmed} onToggle={handleToggle} />
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-border px-4 py-3">
          <span className="text-xs text-muted-foreground">
            {trimmed.length >= 1 && (results ?? []).length > 0
              ? `${(results ?? []).length} ${(results ?? []).length === 1 ? "result" : "results"}`
              : "\u00A0"}
          </span>
        </div>
      </DialogContent>
    </Dialog>
  );
}
