import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { DocsPage, type DocsPageProps } from "fumadocs-ui/layouts/docs/page";
import type { Node, Root } from "fumadocs-core/page-tree";
import type { OpenAPIPageProps } from "fumadocs-openapi/ui";
import type { ReactNode } from "react";
import { navigate } from "astro:transitions/client";
import { RootProvider } from "fumadocs-ui/provider/astro";
import type { AstroProviderProps } from "fumadocs-core/framework/astro";
import { resolveIcon } from "../../lib/icons";
import { OpenAPIPage } from "../api-page";
import { baseOptions, createTabs } from "./layout-shared";
import SearchDialog from "./search";

// The tree arrives as a serialized island prop, so its icons travel as name
// strings (see lib/source.ts) — React elements cannot cross the serialization
// boundary. Resolve them here to real components, on both the prerender and
// the client, before the tree reaches DocsLayout.
function resolveTreeIcons<T extends Node>(node: T): T {
  const icon = ("icon" in node ? node.icon : undefined) as string | undefined;
  const spec = node as T & { method?: string; deprecated?: boolean };
  const next = { ...node } as T & { icon: ReactNode };
  next.icon = resolveIcon(icon);
  if (spec.method !== undefined || spec.deprecated !== undefined) {
    next.name = specNodeName(spec);
  }
  if (next.type === "folder") {
    next.children = next.children.map(resolveTreeIcons);
    if (next.index) next.index = resolveTreeIcons(next.index);
  }
  return next;
}

// Spec operation pages carry their HTTP method (and deprecation state) as
// plain strings on the tree nodes (see lib/sidebar-tree.ts); render the same
// method badge the OpenAPI integration ships for its own page tree.
const METHOD_COLORS: Record<string, string> = {
  GET: "text-green-600 dark:text-green-400",
  POST: "text-blue-600 dark:text-blue-400",
  PUT: "text-yellow-600 dark:text-yellow-400",
  PATCH: "text-orange-600 dark:text-orange-400",
  DELETE: "text-red-600 dark:text-red-400",
};

function specNodeName(node: Node & { method?: string; deprecated?: boolean }): ReactNode {
  let name: ReactNode = node.name;
  if (node.deprecated) {
    name = <span className="fd-page-tree-item-name line-through">{name}</span>;
  }
  if (node.method) {
    const color = METHOD_COLORS[node.method.toUpperCase()] ?? METHOD_COLORS.GET;
    name = (
      <>
        {name}{" "}
        <span className={`ms-auto text-xs text-nowrap font-mono font-medium ${color}`}>
          {node.method.toUpperCase()}
        </span>
      </>
    );
  }
  return name;
}

// The docs shell rendered as a React island (see the .astro pages under
// src/pages/docs). The page tree and openapi props arrive as serializable
// props; MDX content arrives as server-rendered children.
export function Docs({
  tree,
  children,
  pathname,
  params,
  page,
  prose = false,
  api,
  apiTitle,
  apiDescription,
}: {
  tree: Root;
  children?: ReactNode;
  pathname: string;
  params: AstroProviderProps["params"];
  page: DocsPageProps;
  /** Wrap the reading column at ~70ch like the marketing pages. */
  prose?: boolean;
  /** Spec pages: rendered inside the island so the playground stays one tree. */
  api?: OpenAPIPageProps;
  apiTitle?: string;
  apiDescription?: string;
}) {
  const resolvedTree: Root = {
    ...tree,
    children: tree.children.map(resolveTreeIcons),
  };
  return (
    <RootProvider
      pathname={pathname}
      params={params}
      navigate={navigate}
      theme={{ enabled: false }}
      search={{ SearchDialog }}
    >
      <DocsLayout
        tree={resolvedTree}
        tabs={createTabs(resolvedTree)}
        containerProps={prose ? { className: "sunred-prose-layout" } : undefined}
        {...baseOptions()}
      >
        <DocsPage {...page}>
          {api ? (
            <>
              <h1 className="text-3xl font-semibold mb-2">{apiTitle}</h1>
              {apiDescription ? (
                <p className="text-lg text-fd-muted-foreground mb-8">{apiDescription}</p>
              ) : null}
              <OpenAPIPage {...api} />
            </>
          ) : (
            children
          )}
        </DocsPage>
      </DocsLayout>
    </RootProvider>
  );
}
