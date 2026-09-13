import type { ReactNode } from "react";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { baseOptions, tabs } from "@/lib/layout.shared";
import { getSidebarTree } from "@/lib/sidebar-tree";

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout tree={getSidebarTree()} tabs={tabs} {...baseOptions()}>
      {children}
    </DocsLayout>
  );
}
