"use client";

import { use } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Users, ExternalLink, UserCheck, UserPlus } from "lucide-react";
import { Avatar } from "@base-ui/react/avatar";
import { toast } from "sonner";
import { SharedArticleCard } from "@/components/shared-article-card";
import { FeedIcon } from "@/components/feed-icon";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import {
  getClient,
  getUserProfile,
  followUser,
  unfollowUser,
  unwrap,
} from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import { cn } from "@workspace/ui/lib/utils";
import type { PublicProfileResponse } from "@/lib/types";

function FollowButton({
  handle,
  isFollowing,
  onToggle,
}: {
  handle: string;
  isFollowing: boolean;
  onToggle: (next: boolean) => void;
}) {
  async function handleClick() {
    const next = !isFollowing;
    try {
      if (isFollowing) {
        const { error } = await unfollowUser({
          client: await getClient(),
          path: { handle },
        });
        if (error) throw error;
      } else {
        const { error } = await followUser({
          client: await getClient(),
          path: { handle },
        });
        if (error) throw error;
      }
      onToggle(next);
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not update follow"));
    }
  }

  return (
    <Button
      variant={isFollowing ? "outline" : "default"}
      size="sm"
      onClick={handleClick}
      className="gap-1.5"
    >
      {isFollowing ? (
        <>
          <UserCheck className="size-3.5" />
          Following
        </>
      ) : (
        <>
          <UserPlus className="size-3.5" />
          Follow
        </>
      )}
    </Button>
  );
}

export default function UserProfilePage({
  params,
}: {
  params: Promise<{ handle: string }>;
}) {
  const { handle } = use(params);
  const queryClient = useQueryClient();

  const {
    data: profile,
    isLoading,
    error,
  } = useQuery<PublicProfileResponse>({
    queryKey: ["user-profile", handle],
    queryFn: async () => unwrap(getUserProfile({ client: await getClient(), path: { handle } })),
  });

  function handleFollowToggle(next: boolean) {
    queryClient.setQueryData<PublicProfileResponse>(
      ["user-profile", handle],
      (old) => {
        if (!old) return old;
        return {
          ...old,
          profile: {
            ...old.profile,
            is_following: next,
            follower_count: old.profile.follower_count + (next ? 1 : -1),
          },
          global_follower_count: Math.max(0, old.global_follower_count + (next ? 1 : -1)),
        };
      },
    );
    // Also invalidate the following list
    void queryClient.invalidateQueries({ queryKey: ["following"] });
  }

  if (isLoading) {
    return (
      <div className="flex flex-col">
        <PageHeader title={<Skeleton className="h-5 w-32" />} />
        <div className="px-4 py-6 flex flex-col gap-6">
          <div className="flex items-center gap-4">
            <Skeleton className="size-16 rounded-full shrink-0" />
            <div className="flex flex-col gap-2">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3 w-48" />
            </div>
          </div>
          <div className="flex flex-col gap-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error || !profile) {
    return (
      <div className="flex flex-col">
        <PageHeader title="User not found" />
        <div className="p-4">
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Users className="size-6 text-primary" />
              </EmptyMedia>
              <EmptyTitle>User not found</EmptyTitle>
              <EmptyDescription>
                No user with the handle @{handle} exists.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </div>
    );
  }

  const { profile: user, shared_articles, feeds, global_follower_count } = profile;
  const displayName = user.display_name?.trim() || `@${user.handle}`;
  // Federated total from the relay aggregates; fall back to the local count
  // when no relay is configured (global_follower_count == 0).
  const followerTotal = global_follower_count > 0 ? global_follower_count : user.follower_count;

  return (
    <div className="flex flex-col">
      <PageHeader title={`@${user.handle}`} />

      <div className="px-4 py-6 flex flex-col gap-8">
        {/* Profile header */}
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-center gap-4">
            <Avatar.Root
              className={cn(
                "flex size-16 shrink-0 items-center justify-center rounded-full",
                "bg-primary text-xl font-semibold text-primary-foreground select-none",
              )}
            >
              <Avatar.Fallback>
                {displayName.charAt(0).toUpperCase()}
              </Avatar.Fallback>
            </Avatar.Root>
            <div>
              <h1 className="text-lg font-semibold">{displayName}</h1>
              <p className="text-sm text-muted-foreground">@{user.handle}</p>
              {user.bio && (
                <p className="mt-1 text-sm text-muted-foreground max-w-sm">{user.bio}</p>
              )}
              <div className="mt-2 flex items-center gap-4 text-xs text-muted-foreground">
                <span>
                  <span className="font-medium text-foreground">{followerTotal}</span>{" "}
                  {followerTotal === 1 ? "follower" : "followers"}
                  {global_follower_count > 0 && global_follower_count !== user.follower_count && (
                    <span className="ml-1 text-muted-foreground/70">
                      ({user.follower_count} here)
                    </span>
                  )}
                </span>
                <span>
                  <span className="font-medium text-foreground">{user.following_count}</span>{" "}
                  following
                </span>
              </div>
            </div>
          </div>

          <FollowButton
            handle={user.handle}
            isFollowing={user.is_following ?? false}
            onToggle={handleFollowToggle}
          />
        </div>

        {/* Shared articles */}
        <section aria-labelledby="shared-heading">
          <h2 id="shared-heading" className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            Shared articles
          </h2>
          {shared_articles && shared_articles.length > 0 ? (
            <div className="divide-y divide-border rounded-lg border border-border">
              {shared_articles.map((article, i) => (
                <SharedArticleCard
                  key={article.id}
                  article={article}
                  staggerIndex={i}
                  showSharer={false}
                />
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">No shared articles yet.</p>
          )}
        </section>

        {/* Subscribed feeds */}
        <section aria-labelledby="feeds-heading">
          <h2 id="feeds-heading" className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
            Subscribed feeds
          </h2>
          {feeds && feeds.length > 0 ? (
            <div className="flex flex-col gap-1">
              {feeds.map((feed) => (
                <a
                  key={feed.id}
                  href={feed.site_url || feed.feed_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={cn(
                    "flex items-center gap-3 rounded-md px-3 py-2",
                    "hover:bg-muted/50 transition-colors",
                  )}
                >
                  <FeedIcon siteUrl={feed.site_url} className="size-4 shrink-0 rounded-sm" />
                  <span className="flex-1 truncate text-sm">{feed.title || feed.feed_url}</span>
                  <ExternalLink className="size-3.5 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100" />
                </a>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">No public feeds.</p>
          )}
        </section>
      </div>
    </div>
  );
}
