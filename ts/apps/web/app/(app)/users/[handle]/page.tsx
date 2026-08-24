"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Users, UserCheck, UserPlus, Menu as MenuIcon, ChevronRight, Rss } from "lucide-react";
import { toast } from "sonner";
import { EntryCard } from "@/components/entry-card";
import { EntryCardSkeleton } from "@/components/entry-card-skeleton";
import { FeedIcon } from "@/components/feed-icon";
import { useShell } from "@/components/shell-context";
import { UserAvatar } from "@/components/user-avatar";
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
import { siteDomain, htmlSnippet } from "@/lib/format";
import { cn } from "@workspace/ui/lib/utils";
import type { Entry, PublicProfileResponse, SharedArticle } from "@/lib/types";

/**
 * Map a SharedArticle to the Entry shape EntryCard expects. The shared_at
 * timestamp is used as the publish date so the card shows when the article
 * was shared. Status and starred are populated from the viewer's state
 * (joined by article URL on the server).
 */
function toEntry(article: SharedArticle): Entry {
  return {
    id: 0,
    feed_id: 0,
    hash: "",
    changed_at: article.shared_at,
    published_at: article.published_at ?? article.shared_at,
    status: article.status || "unread",
    starred: article.starred || false,
    title: article.title,
    url: article.article_url,
    description: article.description,
    author: article.author,
    feed: {
      id: 0,
      title: article.feed_title ?? "",
      site_url: article.feed_site_url ?? "",
      feed_url: article.feed_url ?? "",
    } as Entry["feed"],
  } as unknown as Entry;
}

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

        <UserAvatar
          displayName={hasDisplayName ? displayName : undefined}
          handle={handle}
          className="size-12 text-lg font-semibold"
        />

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

export default function UserProfilePage({
  params,
}: {
  params: Promise<{ handle: string }>;
}) {
  const { handle } = use(params);
  const queryClient = useQueryClient();

  // Reflect the active tab in the URL hash (#articles / #feeds) so it can be
  // shared and restored. Syncs with the browser location — an external system.
  const [tab, setTab] = useState("articles");
  useEffect(() => {
    const fromHash = () => {
      const h = window.location.hash.replace(/^#/, "");
      setTab(h === "feeds" ? "feeds" : "articles");
    };
    fromHash();
    window.addEventListener("hashchange", fromHash);
    return () => window.removeEventListener("hashchange", fromHash);
  }, []);
  function handleTabChange(value: string) {
    setTab(value);
    if (typeof window !== "undefined") {
      window.location.hash = value;
    }
  }

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
                <EntryCardSkeleton key={i} />
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
      <Tabs value={tab} onValueChange={handleTabChange} className="flex min-h-0 flex-1 flex-col gap-0">
        <div className="mx-auto w-full max-w-3xl shrink-0 border-b border-border px-4 pt-1.5 lg:pl-[48px]">
          <TabsList variant="line" className="h-8! px-0">
            <TabsTrigger value="articles">
              Articles
              {articles.length > 0 && (
                <span className="rounded-full bg-muted px-1.5 text-xs font-medium tabular-nums text-foreground/70">
                  {articles.length}
                </span>
              )}
            </TabsTrigger>
            <TabsTrigger value="feeds">
              Feeds
              {userFeeds.length > 0 && (
                <span className="rounded-full bg-muted px-1.5 text-xs font-medium tabular-nums text-foreground/70">
                  {userFeeds.length}
                </span>
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
                    <EntryCard
                      key={article.id}
                      entry={toEntry(article)}
                      staggerIndex={i}
                      preview
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
                  {userFeeds.map((feed) => {
                    const domain = siteDomain(feed.site_url);
                    const summary = htmlSnippet(feed.description, 240);
                    return (
                      <li key={feed.id}>
                        <Link
                          href={`/feeds?url=${encodeURIComponent(feed.feed_url)}`}
                          className={cn(
                            "group flex items-start gap-3 px-4 py-3",
                            "hover:bg-muted/50 transition-colors",
                          )}
                        >
                          <FeedIcon
                            siteUrl={feed.site_url}
                            className="size-5 shrink-0 rounded-md"
                          />
                          <div className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium text-foreground">
                              {feed.title || feed.feed_url}
                            </span>
                            {domain && (
                              <span className="block truncate text-xs text-muted-foreground">
                                {domain}
                              </span>
                            )}
                            {summary && (
                              <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
                                {summary}
                              </p>
                            )}
                          </div>
                          <ChevronRight className="size-4 shrink-0 self-center text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                        </Link>
                      </li>
                    );
                  })}
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
                        {displayName}{" "}isn&apos;t sharing any public feeds.
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
