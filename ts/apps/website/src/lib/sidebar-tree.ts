import type { Folder, Node, Root } from "fumadocs-core/page-tree";
import { source } from "./source";

let cached: Root | undefined;

// Turn each "--- Section ---" separator plus the pages that follow it into a
// collapsible folder. Real directories would add a URL segment, so sections
// stay as meta.json separators and the tree is reshaped here instead.
function foldSeparators(children: Node[]): Node[] {
  const out: Node[] = [];
  let group: Folder | null = null;

  for (const node of children) {
    if (node.type === "separator") {
      if (group) out.push(group);
      group = {
        type: "folder",
        name: node.name,
        icon: node.icon,
        collapsible: true,
        defaultOpen: true,
        children: [],
      };
    } else if (group) {
      group.children.push(node);
    } else {
      out.push(node);
    }
  }

  if (group) out.push(group);
  return out;
}

// Method and deprecation state of each spec page, keyed by its URL. The tree
// crosses the island serialization boundary as a prop, so React elements
// can't travel with it — these are plain strings that the island turns into
// the method badge next to the operation name.
function specPageMeta(): Map<string, { method: string; deprecated?: boolean }> {
  const out = new Map<string, { method: string; deprecated?: boolean }>();
  for (const page of source.getPages()) {
    const meta = (page.data as { _openapi?: { method?: string; deprecated?: boolean } })._openapi;
    if (meta?.method) out.set(page.url, { method: meta.method, deprecated: meta.deprecated });
  }
  return out;
}

function attachSpecMeta(
  children: Node[],
  meta: Map<string, { method: string; deprecated?: boolean }>,
): Node[] {
  return children.map((node) => {
    if (node.type === "page" && node.url) {
      const page = meta.get(node.url);
      if (page) {
        return { ...node, method: page.method, deprecated: page.deprecated } as Node & {
          method: string;
          deprecated?: boolean;
        };
      }
    } else if (node.type === "folder") {
      return { ...node, children: attachSpecMeta(node.children, meta) };
    }
    return node;
  });
}

// The sidebar tree shared by every docs layout and the section tabs.
export function getSidebarTree(): Root {
  cached ??= {
    ...source.pageTree,
    children: attachSpecMeta(
      source.pageTree.children.map((child) =>
        child.type === "folder" ? { ...child, children: foldSeparators(child.children) } : child,
      ),
      specPageMeta(),
    ),
  };
  return cached;
}
