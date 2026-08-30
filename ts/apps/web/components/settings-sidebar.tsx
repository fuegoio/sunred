"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, Key, User, FileText, CircleHelp } from "lucide-react";
import { Logo } from "@/components/logo";
import { OfflineBadge } from "@/components/offline-badge";
import { Separator } from "@workspace/ui/components/separator";
import { buttonVariants } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import {
  AccountButton,
  isActive,
  navLinkActiveClass,
  navLinkClass,
} from "@/components/app-sidebar";

const settingsNavItems = [
  { href: "/settings/tokens", label: "API tokens", icon: Key },
  { href: "/settings/profile", label: "Profile", icon: User },
  { href: "/settings/opml", label: "OPML", icon: FileText },
];

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

function SettingsSidebarContent({ userHandle, userDisplayName, userHasAvatar }: { userHandle: string; userDisplayName?: string; userHasAvatar?: boolean }) {
  const pathname = usePathname();

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 shrink-0 items-center gap-2 px-4 w-full">
        <Link href="/" className="flex items-center gap-2 font-serif text-lg font-bold px-1">
          <Logo className="size-5" />
          Sunred
        </Link>
        <div className="flex-1" />
        <OfflineBadge />
        <AccountButton userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
      </div>

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-3">
        <Link
          href="/"
          className={cn(navLinkClass, "text-muted-foreground hover:text-sidebar-foreground")}
        >
          <ArrowLeft className="size-4" />
          Back to feeds
        </Link>

        <Separator />

        <nav className="flex flex-col gap-0.5">
          <h3 className="px-3 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Settings
          </h3>
          {settingsNavItems.map((item) => {
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
      </div>

      <div className="shrink-0 p-3">
        <HelpButton />
      </div>
    </div>
  );
}

export function SettingsSidebar({
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
        <SettingsSidebarContent userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
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
            <SettingsSidebarContent userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
          </aside>
        </div>
      )}
    </>
  );
}
