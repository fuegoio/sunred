"use client";

import { useState, useCallback } from "react";
import { usePathname } from "next/navigation";
import { AppSidebar } from "@/components/app-sidebar";
import { OnboardingOverlay } from "@/components/onboarding-overlay";
import { SettingsSidebar } from "@/components/settings-sidebar";
import { ShellContext } from "@/components/shell-context";

export function AppShell({
  children,
  userHandle,
  userDisplayName,
  userHasAvatar,
  pdsSyncStatus,
  userOnboarded,
}: {
  children: React.ReactNode;
  userHandle: string;
  userDisplayName?: string;
  userHasAvatar?: boolean;
  pdsSyncStatus: string;
  userOnboarded: boolean;
}) {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const pathname = usePathname();
  const isSettings = pathname.startsWith("/settings");

  const Sidebar = isSettings ? SettingsSidebar : AppSidebar;
  const openSidebar = useCallback(() => setSidebarOpen(true), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  return (
    <ShellContext value={{ openSidebar, closeSidebar }}>
      <div className="flex h-svh overflow-hidden">
        {/* Left filler — extends sidebar bg to the viewport edge on wide screens */}
        <div className="hidden flex-1 bg-sidebar lg:block" />

        <div className="flex w-full min-w-0 max-w-5xl shrink-0 overflow-hidden">
          <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} userHandle={userHandle} userDisplayName={userDisplayName} userHasAvatar={userHasAvatar} pdsSyncStatus={pdsSyncStatus} />
          <main className="flex-1 overflow-hidden bg-background">{children}</main>
        </div>

        {/* Right filler — matches main content bg on wide screens */}
        <div className="hidden flex-1 bg-background lg:block" />

        <OnboardingOverlay initialOnboarded={userOnboarded} userDisplayName={userDisplayName} />
      </div>
    </ShellContext>
  );
}
