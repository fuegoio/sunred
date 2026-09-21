import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { DocsPage, type DocsPageProps } from "fumadocs-ui/layouts/docs/page";
import type { Root } from "fumadocs-core/page-tree";
import type { OpenAPIPageProps } from "fumadocs-openapi/ui";
import type { ReactNode } from "react";
import { navigate } from "astro:transitions/client";
import { RootProvider } from "fumadocs-ui/provider/astro";
import type { AstroProviderProps } from "fumadocs-core/framework/astro";
import { OpenAPIPage } from "../api-page";
import { baseOptions, createTabs } from "./layout-shared";
import SearchDialog from "./search";

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
  return (
    <RootProvider
      pathname={pathname}
      params={params}
      navigate={navigate}
      theme={{ enabled: false }}
      search={{ SearchDialog }}
    >
      <DocsLayout
        tree={tree}
        tabs={createTabs(tree)}
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
