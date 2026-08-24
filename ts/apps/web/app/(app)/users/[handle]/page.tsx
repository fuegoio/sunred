"use client";

import { use } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Users, UserCheck, UserPlus, Menu as MenuIcon, ExternalLink, Rss } from "lucide-react";
import { Avatar } from "@base-ui/react/avatar";
import { toast } from "sonner";
import { SharedArticleCard } from "@/components/shared-article-card";
import { FeedIcon } from "@/components/feed-icon";
import { useShell } from "@/components/shell-context";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Button } from "@workspace/ui/components/button";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs";
import {
  Empty,
  EmptyContent,
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

/**
 * Profile masthead — the feed-style header for a user's profile. Mirrors the
 * app's `PageHeader` treatment (border-b, bg-background, desktop indent) so it
 * reads as the same chrome as the unread/starred/follows timelines, and
 * inlines the mobile sidebar toggle that `PageHeader` would otherwise render.
 */
function ProfileMasthead({
  displayName,
  handle,
  bio,
  hasDisplayName,
  followerTotal,
  localFollowerCount,
  followingCount,
  showFederatedSplit,
  isFollowing,
  onFollowToggle,
}: {
  displayName: string;
  handle: string;
  bio?: string;
  hasDisplayName: boolean;
  followerTotal: number;
  localFollowerCount: number;
  followingCount: number;
  showFederatedSplit: boolean;
  isFollowing: boolean;
  onFollowToggle: (next: boolean) => void;
}) {
  const shell = useShell();

  return (
    <div className="border-b border-border bg-background px-4 py-3 lg:pl-[48px]">
      <div className="flex items-start gap-3">
        {/* Mobile sidebar toggle — replaces the separate top bar, same as PageHeader */}
        {shell && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Toggle sidebar"
            onClick={shell.openSidebar}
            className="-ml-2 mt-0.5 shrink-0 lg:hidden"
          >
            <MenuIcon className="size-4" />
          </Button>
        )}

        <Avatar.Root
          className={cn(
            "flex size-12 shrink-0 items-center justify-center rounded-full",
            "bg-primary text-lg font-semibold text-primary-foreground select-none",
          )}
        >
          <Avatar.Fallback>{displayName.charAt(0).toUpperCase()}</Avatar.Fallback>
        </Avatar.Root>

        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h1 className="truncate font-serif text-lg font-bold tracking-wider">
                {displayName}
              </h1>
              {hasDisplayName && (
                <p className="truncate text-sm text-muted-foreground">@{handle}</p>
              )}
            </div>
            <FollowButton
              handle={handle}
              isFollowing={isFollowing}
              onToggle={onFollowToggle}
            />
          </div>

          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>
              <span className="font-medium text-foreground">{followerTotal}</span>{" "}
              {followerTotal === 1 ? "follower" : "followers"}
              {showFederatedSplit && (
                <span className="ml-1 text-muted-foreground/70">
                  ({localFollowerCount} here)
                </span>
              )}
            </span>
            <span>
              <span className="font-medium text-foreground">{followingCount}</span>{" "}
              following
            </span>
          </div>

          {bio && <p className="mt-2 text-sm text-muted-foreground">{bio}</p>}
        </div>
      </div>
    </div>
  );
}

function MastheadSkeleton() {
  const shell = useShell();
  return (
    <div className="border-b border-border bg-background px-4 py-3 lg:pl-[48px]">
      <div className="flex items-start gap-3">
        {shell && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Toggle sidebar"
            onClick={shell.openSidebar}
            className="-ml-2 mt-0.5 shrink-0 lg:hidden"
          >
            <MenuIcon className="size-4" />
          </Button>
        )}
        <Skeleton className="size-12 shrink-0 rounded-full" />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div className="flex flex-col gap-1.5">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3.5 w-24" />
            </div>
            <Skeleton className="h-8 w-20" />
          </div>
          <div className="mt-1 flex items-center gap-4">
            <Skeleton className="h-3.5 w-20" />
            <Skeleton className="h-3.5 w-20" />
          </div>
        </div>
      </div>
    </div>
  );
}

/** Lightweight placeholder mirroring SharedArticleCard's layout (avatar glyph + metadata + title + snippet). */
function SharedArticleCardSkeleton() {
  return (
    <div className="flex gap-3 px-4 py-3">
      <div className="flex size-5 shrink-0 items-start justify-center pt-1">
        <Skeleton className="size-5 rounded-full" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Skeleton className="size-3.5 shrink-0 rounded-sm" />
          <Skeleton className="h-4 w-28" />
          <span aria-hidden className="text-xs text-muted-foreground">
            ·
          </span>
          <Skeleton className="h-4 w-10" />
        </div>
        <Skeleton className="mt-1.5 h-4 w-3/4" />
        <Skeleton className="mt-1 h-4 w-full" />
        <Skeleton className="mt-1 h-4 w-2/3" />
      </div>
    </div>
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
    void queryClient.invalidateQueries({ queryKey: ["following"] });
  }

  if (isLoading) {
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <div className="mx-auto w-full max-w-3xl shrink-0">
          <MastheadSkeleton />
        </div>
        <ScrollArea className="flex-1 min-h-0">
          <div className="mx-auto w-full max-w-3xl">
            <div className="divide-y divide-border">
              {Array.from({ length: 6 }).map((_, i) => (
                <SharedArticleCardSkeleton key={i} />
              ))}
            </div>
          </div>
        </ScrollArea>
      </div>
    );
  }

  if (error || !profile) {
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <div className="mx-auto w-full max-w-3xl shrink-0">
          <MastheadSkeleton />
        </div>
        <ScrollArea className="flex-1 min-h-0">
          <div className="mx-auto w-full max-w-3xl">
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
        </ScrollArea>
      </div>
    );
  }

  const { profile: user, shared_articles, feeds, global_follower_count } = profile;
  const hasDisplayName = !!(user.display_name?.trim());
  const displayName = hasDisplayName ? user.display_name!.trim() : `@${user.handle}`;
  // Federated total from the relay aggregates; fall back to the local count
  // when no relay is configured (global_follower_count == 0).
  const followerTotal = global_follower_count > 0 ? global_follower_count : user.follower_count;
  const showFederatedSplit =
    global_follower_count > 0 && global_follower_count !== user.follower_count;
  const articles = shared_articles ?? [];
  const userFeeds = feeds ?? [];

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <ProfileMasthead
          displayName={displayName}
          handle={user.handle}
          bio={user.bio}
          hasDisplayName={hasDisplayName}
          followerTotal={followerTotal}
          localFollowerCount={user.follower_count}
          followingCount={user.following_count}
          showFederatedSplit={showFederatedSplit}
          isFollowing={user.is_following ?? false}
          onFollowToggle={handleFollowToggle}
        />
      </div>
      <Tabs defaultValue="articles" className="flex min-h-0 flex-1 flex-col gap-0">
        <div className="mx-auto w-full max-w-3xl shrink-0 px-4 pt-3 lg:pl-[48px]">
          <TabsList>
            <TabsTrigger value="articles">
              Articles
              {articles.length > 0 && (
                <span className="text-xs text-muted-foreground">{articles.length}</span>
              )}
            </TabsTrigger>
            <TabsTrigger value="feeds">
              Feeds
              {userFeeds.length > 0 && (
                <span className="text-xs text-muted-foreground">{userFeeds.length}</span>
              )}
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="articles" className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="mx-auto w-full max-w-3xl">
              {articles.length > 0 ? (
                <div className="divide-y divide-border">
                  {articles.map((article, i) => (
                    <SharedArticleCard
                      key={article.id}
                      article={article}
                      staggerIndex={i}
                      showSharer={false}
                    />
                  ))}
                </div>
              ) : (
                <div className="p-4">
                  <Empty className="border">
                    <EmptyHeader>
                      <EmptyMedia variant="icon">
                        <Users className="size-6 text-primary" />
                      </EmptyMedia>
                      <EmptyTitle>Nothing shared yet</EmptyTitle>
                      <EmptyDescription>
                        {displayName}{" "}hasn&apos;t shared any articles. When they do,
                        they&apos;ll land here in reverse-chronological order.
                      </EmptyDescription>
                    </EmptyHeader>
                    <EmptyContent>
                      <Button
                        variant="link"
                        size="sm"
                        onClick={() => handleFollowToggle(!(user.is_following ?? false))}
                        className="h-auto p-0"
                      >
                        {user.is_following ? "Unfollow" : `Follow @${user.handle}`}
                      </Button>
                    </EmptyContent>
                  </Empty>
                </div>
              )}
            </div>
          </ScrollArea>
        </TabsContent>
        <TabsContent value="feeds" className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="mx-auto w-full max-w-3xl">
              {userFeeds.length > 0 ? (
                <ul className="divide-y divide-border">
                  {userFeeds.map((feed) => (
                    <li key={feed.id}>
                      <a
                        href={feed.site_url || feed.feed_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className={cn(
                          "group flex items-center gap-3 px-4 py-3",
                          "hover:bg-muted/50 transition-colors",
                        )}
                      >
                        <FeedIcon
                          siteUrl={feed.site_url}
                          className="size-4 shrink-0 rounded-sm"
                        />
                        <span className="flex-1 truncate text-sm">
                          {feed.title || feed.feed_url}
                        </span>
                        <ExternalLink className="size-3.5 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100" />
                      </a>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="p-4">
                  <Empty className="border">
                    <EmptyHeader>
                      <EmptyMedia variant="icon">
                        <Rss className="size-6 text-primary" />
                      </EmptyMedia>
                      <EmptyTitle>No public feeds</EmptyTitle>
                      <EmptyDescription>
                        {displayName} isn&apos;t sharing any public feeds.
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                </div>
              )}
            </div>
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
  );
}
