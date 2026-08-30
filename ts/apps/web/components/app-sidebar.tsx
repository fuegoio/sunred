"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { LayoutList, Circle, Star, Plus, Settings, LogOut, Sun, Moon, User, CircleHelp } from "lucide-react";
import { useTheme } from "next-themes";
import { Menu } from "@base-ui/react/menu";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { getClient, listFeeds, listFolders, listFollowing, unwrap, avatarUrl } from "@/lib/sunred";
import { signout } from "@/lib/auth";
import { Logo } from "@/components/logo";
import { OfflineBadge } from "@/components/offline-badge";
import { FeedTree } from "@/components/feed-tree";
import { SearchBox } from "@/components/search-box";
import { FollowSearchDialog } from "@/components/follow-search-dialog";
import { UserAvatar } from "@/components/user-avatar";
import { buttonVariants } from "@workspace/ui/components/button";
import { FolderCreateDialog } from "@/components/folder-create-dialog";
import { cn } from "@workspace/ui/lib/utils";
import { Separator } from "@workspace/ui/components/separator";
import type { Feed, Folder, UserProfile } from "@/lib/types";

export const navLinkClass = cn(
  "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium",
  "text-sidebar-foreground/70 transition-colors",
  "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
);

export const navLinkActiveClass = cn("bg-sidebar-accent text-sidebar-accent-foreground");

export function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  return pathname === href || pathname.startsWith(href + "/");
}

function SidebarNav() {
  const pathname = usePathname();
  const navItems = [
    { href: "/", label: "Unread", icon: Circle },
    { href: "/all", label: "All", icon: LayoutList },
    { href: "/starred", label: "Starred", icon: Star },
  ];

  return (
    <nav data-onboarding="unread" className="flex flex-col gap-0.5">
      {navItems.map((item) => {
        const active = isActive(pathname, item.href);
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={active ? "page" : undefined}
            className={cn(navLinkClass, active && navLinkActiveClass)}
          >
            <item.icon className={cn("size-4", active && "text-primary")} />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}

export function AccountButton({ userHandle, userDisplayName, userHasAvatar }: { userHandle: string; userDisplayName?: string; userHasAvatar?: boolean }) {
  const router = useRouter();
  const { resolvedTheme, setTheme } = useTheme();

  async function handleSignout() {
    await signout();
    router.push("/login");
    router.refresh();
  }

  const menuItemClass = cn(
    "flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm",
    "hover:bg-accent hover:text-accent-foreground transition-colors",
    "focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/30",
  );

  return (
    <Menu.Root>
      <Menu.Trigger
        className={cn(
          "flex size-8 items-center justify-center rounded-full transition-colors",
          "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
          "focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/30",
        )}
        aria-label="Account menu"
      >
        <UserAvatar displayName={userDisplayName} handle={userHandle} src={avatarUrl(userHandle, userHasAvatar)} className="size-7 text-xs font-medium" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner
          className={cn(
            "z-50 min-w-56 overflow-hidden rounded-md border border-border bg-popover p-1",
            "shadow-md",
          )}
          align="end"
          side="bottom"
          sideOffset={6}
        >
          <Menu.Popup>
            <div className="flex items-center gap-2.5 px-2 py-2">
              <UserAvatar displayName={userDisplayName} handle={userHandle} src={avatarUrl(userHandle, userHasAvatar)} className="size-9 text-sm font-medium" />
              <div className="flex min-w-0 flex-col">
                {userDisplayName ? (
                  <span className="truncate text-sm font-medium">{userDisplayName}</span>
                ) : null}
                <span className="truncate text-xs text-muted-foreground">@{userHandle}</span>
              </div>
            </div>
            <hr className="-mx-1 my-1 border-border" />
            <Menu.Item className={menuItemClass} render={<Link href={`/users/${userHandle}`} />}>
              <User className="size-4" />
              Profile
            </Menu.Item>
            <Menu.Item className={menuItemClass} render={<Link href="/settings" />}>
              <Settings className="size-4" />
              Settings
            </Menu.Item>
            <hr className="-mx-1 my-1 border-border" />
            <Menu.Item
              className={menuItemClass}
              onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
            >
              {resolvedTheme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
              {resolvedTheme === "dark" ? "Light mode" : "Dark mode"}
            </Menu.Item>
            <hr className="-mx-1 my-1 border-border" />
            <Menu.Item className={menuItemClass} onClick={handleSignout}>
              <LogOut className="size-4" />
              Sign out
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}

function HelpButton() {
  return (
    <div className="flex shrink-0 items-center gap-1">
      <Link
        href="https://sunred.app/docs"
        target="_blank"
        rel="noreferrer"
        aria-label="Help & docs"
        className={cn(
          buttonVariants({ variant: "ghost", size: "icon-xs" }),
          "rounded-full text-muted-foreground hover:text-foreground",
        )}
      >
        <CircleHelp className="size-3.5" />
      </Link>
    </div>
  );
}

function SidebarContent({ userHandle, userDisplayName, userHasAvatar }: { userHandle: string; userDisplayName?: string; userHasAvatar?: boolean }) {
  const pathname = usePathname();
  const { data: feeds, isLoading: feedsLoading } = useQuery<Feed[]>({
    queryKey: ["feeds"],
    queryFn: async () => unwrap(listFeeds({ client: await getClient() })),
  });

  const { data: folders, isLoading: foldersLoading } = useQuery<Folder[]>({
    queryKey: ["folders"],
    queryFn: async () => unwrap(listFolders({ client: await getClient() })),
  });

  const { data: following, isLoading: followingLoading } = useQuery<UserProfile[]>({
    queryKey: ["following"],
    queryFn: async () => unwrap(listFollowing({ client: await getClient() })),
  });

  const isLoading = feedsLoading || foldersLoading;

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 shrink-0 items-center gap-2 px-4 w-full mt-1">
        <Link href="/" className="flex items-center gap-2 font-serif text-lg font-bold px-1">
          <Logo className="size-5" />
          Sunred
        </Link>
        <div className="flex-1" />
        <OfflineBadge />
        <AccountButton userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
      </div>

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-3">
        <SearchBox className="max-w-none" />

        <SidebarNav />

        <Separator />

        <div data-onboarding="feeds" className="flex flex-col gap-1">
          <div className="flex items-center justify-between pl-3 pr-2.5 pb-1">
            <Link
              href="/feeds"
              aria-current={isActive(pathname, "/feeds") ? "page" : undefined}
              className="text-xs font-medium uppercase tracking-wide text-muted-foreground transition-colors hover:text-foreground"
            >
              Feeds
            </Link>
            <div className="flex items-center gap-0.5">
              <FolderCreateDialog />
              <Link
                href="/feeds/new"
                aria-label="Subscribe to a feed"
                className={cn(
                  buttonVariants({ variant: "ghost", size: "icon-xs" }),
                  "text-muted-foreground hover:text-foreground",
                )}
              >
                <Plus className="size-3.5" />
              </Link>
            </div>
          </div>
          {isLoading ? (
            <div className="flex flex-col gap-0.5">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-2.5 px-3 py-2">
                  <Skeleton className="size-3.5 shrink-0 rounded-sm" />
                  <Skeleton className="h-3 flex-1" />
                </div>
              ))}
            </div>
          ) : (feeds ?? []).length === 0 && (folders ?? []).length === 0 ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">
              No feeds yet. Add one to get started.
            </p>
          ) : (
            <FeedTree feeds={feeds ?? []} folders={folders ?? []} />
          )}
        </div>

        <Separator />

        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between pl-3 pr-2.5 pb-1">
            <Link
              href="/follows"
              aria-current={isActive(pathname, "/follows") ? "page" : undefined}
              className="text-xs font-medium uppercase tracking-wide text-muted-foreground transition-colors hover:text-foreground"
            >
              Follows
            </Link>
            <FollowSearchDialog />
          </div>
          {followingLoading ? (
            <div className="flex flex-col gap-0.5">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="flex items-center gap-2.5 px-3 py-2">
                  <Skeleton className="size-5 shrink-0 rounded-full" />
                  <Skeleton className="h-3 flex-1" />
                </div>
              ))}
            </div>
          ) : (following ?? []).length === 0 ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">
              Not following anyone yet. Use + to find people.
            </p>
          ) : (
            <div className="flex flex-col gap-0.5">
              {(following ?? []).map((p) => {
                const href = `/users/${p.handle}`;
                const active = isActive(pathname, href);
                return (
                  <Link
                    key={p.user_id}
                    href={href}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm",
                      "text-sidebar-foreground/70 transition-colors",
                      active
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                    )}
                  >
                    <UserAvatar
                      displayName={p.display_name}
                      handle={p.handle}
                      src={avatarUrl(p.handle, p.has_avatar)}
                      className="size-5 text-[10px] font-medium"
                    />
                    <span className="truncate">@{p.handle}</span>
                  </Link>
                );
              })}
            </div>
          )}
        </div>
      </div>

      <div className="shrink-0 p-3">
        <HelpButton />
      </div>
    </div>
  );
}

export function AppSidebar({
  open,
  onClose,
  userHandle,
  userDisplayName,
  userHasAvatar,
}: {
  open: boolean;
  onClose: () => void;
  userHandle: string;
  userDisplayName?: string;
  userHasAvatar?: boolean;
}) {
  return (
    <>
      <aside className="hidden w-64 shrink-0 border-r border-sidebar-border bg-sidebar lg:flex lg:flex-col">
        <SidebarContent userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
      </aside>

      {open && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div
            className="absolute inset-0 bg-black/50 animate-in fade-in duration-200"
            onClick={onClose}
            aria-hidden="true"
          />
          <aside
            className="absolute left-0 top-0 flex h-full w-64 flex-col border-r border-sidebar-border bg-sidebar animate-in slide-in-from-left duration-200"
            onClick={(e) => {
              if ((e.target as HTMLElement).closest("a")) onClose();
            }}
          >
            <SidebarContent userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
          </aside>
        </div>
      )}
    </>
  );
}
