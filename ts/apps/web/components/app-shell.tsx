"use client";

import { useState, useCallback, useEffect } from "react";
import { usePathname } from "next/navigation";
import { AppSidebar } from "@/components/app-sidebar";
import { SettingsSidebar } from "@/components/settings-sidebar";
import { ShellContext } from "@/components/shell-context";
import { SyncStatusBar } from "@/components/sync-status-bar";
import { clearReadSession } from "@/lib/entry-read-session";

export function AppShell({
  children,
  userHandle,
  userDisplayName,
  userHasAvatar,
  pdsSyncStatus,
}: {
  children: React.ReactNode;
  userHandle: string;
  userDisplayName?: string;
  userHasAvatar?: boolean;
  pdsSyncStatus: string;
}) {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const pathname = usePathname();
  const isSettings = pathname.startsWith("/settings");

  // Wipe the read-temp store on navigation so rows marked read by opening an
  // article don't follow the user across views — they should leave once the
  // user leaves the page.
  useEffect(() => {
    clearReadSession();
  }, [pathname]);

  const Sidebar = isSettings ? SettingsSidebar : AppSidebar;
  const openSidebar = useCallback(() => setSidebarOpen(true), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  return (
    <ShellContext value={{ openSidebar, closeSidebar }}>
      <div className="flex h-svh overflow-hidden">
        {/* Left filler — extends sidebar bg to the viewport edge on wide screens */}
        <div className="hidden flex-1 bg-sidebar lg:block" />

        <div className="flex w-full min-w-0 max-w-5xl shrink-0 overflow-hidden">
          <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} />
          <div className="flex min-w-0 flex-1 flex-col">
            <SyncStatusBar initialStatus={pdsSyncStatus} />
            <main className="flex-1 overflow-hidden bg-background">{children}</main>
          </div>
        </div>

        {/* Right filler — matches main content bg on wide screens */}
        <div className="hidden flex-1 bg-background lg:block" />
      </div>
    </ShellContext>
  );
}
