import { Avatar } from "@base-ui/react/avatar";
import { cn } from "@workspace/ui/lib/utils";

/**
 * Initials avatar used across the web UI. Matches the profile page header:
 * muted background, foreground text, first letter of the display name (or
 * handle when no display name is set). Pass sizing + typography via
 * `className` (e.g. "size-9 text-sm font-medium").
 */
export function UserAvatar({
  displayName,
  handle,
  className,
}: {
  displayName?: string;
  handle: string;
  className?: string;
}) {
  const initial = (displayName?.trim() || handle || "?").charAt(0).toUpperCase();
  return (
    <Avatar.Root
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full",
        "bg-muted text-foreground select-none",
        className,
      )}
    >
      <Avatar.Fallback>{initial}</Avatar.Fallback>
    </Avatar.Root>
  );
}
