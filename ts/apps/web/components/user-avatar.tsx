import { Avatar } from "@base-ui/react/avatar";
import { cn } from "@workspace/ui/lib/utils";

/**
 * Initials avatar used across the web UI. When `src` is set (a cached PDS
 * getBlob URL), renders the user's Bluesky profile image; otherwise falls
 * back to the first letter of the display name (or handle). Matches the
 * profile page header styling: muted background, foreground text. Pass
 * sizing + typography via `className` (e.g. "size-9 text-sm font-medium").
 */
export function UserAvatar({
  displayName,
  handle,
  src,
  className,
}: {
  displayName?: string;
  handle: string;
  src?: string;
  className?: string;
}) {
  const initial = (displayName?.trim() || handle || "?").charAt(0).toUpperCase();
  return (
    <Avatar.Root
      className={cn(
        "flex shrink-0 items-center justify-center overflow-hidden rounded-full",
        "bg-muted text-foreground select-none",
        className,
      )}
    >
      {src ? (
        <Avatar.Image src={src} alt="" className="h-full w-full object-cover" />
      ) : null}
      <Avatar.Fallback>{initial}</Avatar.Fallback>
    </Avatar.Root>
  );
}
