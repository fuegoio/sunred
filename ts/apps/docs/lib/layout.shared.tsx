import type { BaseLayoutProps, LayoutTab } from "fumadocs-ui/layouts/shared";
import type { Folder, Node } from "fumadocs-core/page-tree";
import { Logo } from "@/components/logo";
import { source } from "@/lib/source";

// Fumadocs only renders the section selector for the tab it considers active,
// which it detects by URL. Prefix matching works for /self-hosting and
// /api-reference, but the product docs live at the root ("/", "/entries", ...)
// with no shared prefix, so a single tab URL can never match every page.
// Bind each tab to the full set of page URLs in its section so the selector
// stays active on every page via exact-URL matching.
function collectPageUrls(folder: Folder): Set<string> {
  const urls = new Set<string>();
  const visit = (node: Node) => {
    if (node.type === "page") {
      if (node.url) urls.add(node.url);
    } else if (node.type === "folder") {
      if (node.index?.url) urls.add(node.index.url);
      for (const child of node.children) visit(child);
    }
  };
  if (folder.index?.url) urls.add(folder.index.url);
  for (const child of folder.children) visit(child);
  return urls;
}

function sectionUrls(tabUrl: string): Set<string> | undefined {
  const folder = source.pageTree.children.find(
    (c): c is Folder =>
      c.type === "folder" &&
      (c.index?.url ??
        c.children.find((n): n is Extract<Node, { type: "page" }> => n.type === "page")?.url) ===
        tabUrl,
  );
  return folder ? collectPageUrls(folder) : undefined;
}

const DocsIcon = (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
    <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
  </svg>
);
const SelfHostIcon = (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <rect width="20" height="8" x="2" y="2" rx="2" ry="2" />
    <rect width="20" height="8" x="2" y="14" rx="2" ry="2" />
    <line x1="6" x2="6.01" y1="6" y2="6" />
    <line x1="6" x2="6.01" y1="18" y2="18" />
  </svg>
);
const ApiRefIcon = (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <polyline points="16 18 22 12 16 6" />
    <polyline points="8 6 2 12 8 18" />
  </svg>
);

export const tabs: LayoutTab[] = [
  {
    title: "Docs",
    description: "Guides and usage documentation",
    icon: DocsIcon,
    url: "/",
    urls: sectionUrls("/"),
  },
  {
    title: "Self-Hosting",
    description: "Deploy and operate your own instance",
    icon: SelfHostIcon,
    url: "/self-hosting",
    urls: sectionUrls("/self-hosting"),
  },
  {
    title: "API Reference",
    description: "REST API endpoints and schemas",
    icon: ApiRefIcon,
    url: "/api-reference",
    urls: sectionUrls("/api-reference"),
  },
];

export function baseOptions(props: BaseLayoutProps = {}): BaseLayoutProps {
  return {
    nav: {
      title: (
        <span className="flex items-center gap-2 font-semibold font-serif px-1.5">
          <Logo className="size-5" />
          Sunred
        </span>
      ),
    },
    githubUrl: "https://github.com/fuegoio/sunred",
    ...props,
  };
}
