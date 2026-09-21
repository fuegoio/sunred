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

// The sidebar tree shared by every docs layout and the section tabs.
export function getSidebarTree(): Root {
  cached ??= {
    ...source.pageTree,
    children: source.pageTree.children.map((child) =>
      child.type === "folder" ? { ...child, children: foldSeparators(child.children) } : child,
    ),
  };
  return cached;
}
