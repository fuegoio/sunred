"use client";

import { useState } from "react";
import { Popover } from "@base-ui/react/popover";
import { ChevronDown, ChevronRight, Check, FolderOpen, Folder as FolderIcon } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { buttonVariants } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import type { Folder } from "@/lib/types";

const EASE = [0.25, 1, 0.5, 1] as const;

/** Recursively build the folder tree (folders only, no feeds). */
type FolderNode = { folder: Folder; children: FolderNode[] };

function buildFolderTree(folders: Folder[]): FolderNode[] {
  const childrenMap = new Map<number | null, FolderNode[]>();
  for (const f of folders) {
    const key = f.parent_id ?? null;
    if (!childrenMap.has(key)) childrenMap.set(key, []);
    childrenMap.get(key)!.push({ folder: f, children: [] });
  }
  function populate(nodes: FolderNode[]) {
    for (const node of nodes) {
      node.children = childrenMap.get(node.folder.id) ?? [];
      populate(node.children);
    }
  }
  const roots = childrenMap.get(null) ?? [];
  populate(roots);
  return roots;
}

function FolderRow({
  node,
  currentFolderId,
  onSelect,
  depth,
}: {
  node: FolderNode;
  currentFolderId: number | undefined | null;
  onSelect: (folderId: number | undefined) => void;
  depth: number;
}) {
  const [open, setOpen] = useState(true);
  const isSelected = currentFolderId === node.folder.id;
  const hasChildren = node.children.length > 0;

  return (
    <div>
      <button
        type="button"
        onClick={() => onSelect(node.folder.id)}
        className={cn(
          "group flex w-full items-center gap-2 rounded-md py-1.5 pr-2 text-sm transition-colors",
          "hover:bg-accent hover:text-accent-foreground",
          isSelected && "bg-accent/60 text-accent-foreground font-medium",
        )}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {/* Expand/collapse toggle — only rendered if has children */}
        {hasChildren ? (
          <span
            role="button"
            tabIndex={-1}
            onClick={(e) => {
              e.stopPropagation();
              setOpen((o) => !o);
            }}
            className="flex size-4 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground"
          >
            <motion.span
              animate={{ rotate: open ? 90 : 0 }}
              transition={{ duration: 0.18, ease: EASE }}
              className="flex"
            >
              <ChevronRight className="size-3.5" />
            </motion.span>
          </span>
        ) : (
          <span className="size-4 shrink-0" />
        )}

        {/* Folder icon — crossfade open/closed */}
        <span className="relative size-3.5 shrink-0">
          <motion.span
            animate={{ opacity: open && hasChildren ? 1 : 0 }}
            transition={{ duration: 0.15 }}
            className="absolute inset-0 flex items-center"
          >
            <FolderOpen className="size-3.5 text-muted-foreground" />
          </motion.span>
          <motion.span
            animate={{ opacity: open && hasChildren ? 0 : 1 }}
            transition={{ duration: 0.15 }}
            className="absolute inset-0 flex items-center"
          >
            <FolderIcon className="size-3.5 text-muted-foreground" />
          </motion.span>
        </span>

        <span className="min-w-0 flex-1 truncate text-left">{node.folder.title}</span>

        {isSelected && <Check className="size-3.5 shrink-0 text-primary" />}
      </button>

      <AnimatePresence initial={false}>
        {open && hasChildren && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.18, ease: EASE }}
            className="overflow-hidden"
          >
            {node.children.map((child) => (
              <FolderRow
                key={child.folder.id}
                node={child}
                currentFolderId={currentFolderId}
                onSelect={onSelect}
                depth={depth + 1}
              />
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export function FolderPickerPopover({
  folders,
  currentFolderId,
  disabled,
  onSelect,
}: {
  folders: Folder[] | undefined;
  currentFolderId: number | undefined | null;
  disabled?: boolean;
  onSelect: (folderId: number | undefined) => void;
}) {
  const currentFolder = folders?.find((f) => f.id === currentFolderId);
  const tree = buildFolderTree(folders ?? []);

  function handleSelect(folderId: number | undefined) {
    onSelect(folderId);
  }

  return (
    <Popover.Root>
      <Popover.Trigger
        disabled={disabled}
        className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
        aria-label="Move to folder"
      >
        <FolderOpen className="size-3.5" />
        <span className="max-w-[4rem] truncate sm:max-w-[8rem]">
          {currentFolder ? currentFolder.title : "No folder"}
        </span>
        <ChevronDown className="size-3 text-muted-foreground" />
      </Popover.Trigger>

      <Popover.Portal>
        <Popover.Positioner align="start" sideOffset={2}>
          <Popover.Popup className="z-50 min-w-52 max-w-72 overflow-hidden rounded-md border border-border bg-popover p-1 shadow-md animate-in fade-in-0 zoom-in-95 duration-100">
            {/* "No folder" root option */}
            <button
              type="button"
              onClick={() => handleSelect(undefined)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
                "hover:bg-accent hover:text-accent-foreground",
                !currentFolderId
                  ? "bg-accent/60 font-medium text-accent-foreground"
                  : "text-muted-foreground",
              )}
            >
              <span className="size-4 shrink-0" />
              <span className="size-3.5 shrink-0" />
              <span className="flex-1 text-left">No folder</span>
              {!currentFolderId && <Check className="size-3.5 shrink-0 text-primary" />}
            </button>

            {tree.length > 0 && <div className="my-1 h-px bg-border" />}

            {/* Folder tree */}
            <div className="flex flex-col">
              {tree.map((node) => (
                <FolderRow
                  key={node.folder.id}
                  node={node}
                  currentFolderId={currentFolderId}
                  onSelect={handleSelect}
                  depth={0}
                />
              ))}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
