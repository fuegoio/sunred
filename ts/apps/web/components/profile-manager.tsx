"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { Loader2, User } from "lucide-react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { UserAvatar } from "@/components/user-avatar";
import { getClient, getMe, updateMe, deleteMe, updateHandle, unwrap, avatarUrl } from "@/lib/sunred";
import { signout } from "@/lib/auth";
import { getApiErrorMessage } from "@/lib/errors";
import { cn } from "@workspace/ui/lib/utils";
import { ExternalLink } from "lucide-react";
import type { User as UserType } from "@/lib/types";

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

const profileSchema = z.object({
  display_name: z.string().max(255, "Name must be 255 characters or fewer"),
});

type ProfileValues = z.infer<typeof profileSchema>;

// ---------------------------------------------------------------------------
// Avatar
// ---------------------------------------------------------------------------

function AvatarDisplay({ displayName, handle, hasAvatar }: { displayName?: string; handle?: string; hasAvatar?: boolean }) {
  return (
    <UserAvatar
      displayName={displayName}
      handle={handle ?? "?"}
      src={avatarUrl(handle ?? "", hasAvatar)}
      className="size-16 text-xl font-semibold"
    />
  );
}

// ---------------------------------------------------------------------------
// Profile form
// ---------------------------------------------------------------------------

function ProfileForm({ user }: { user: UserType }) {
  const queryClient = useQueryClient();

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<ProfileValues>({
    resolver: zodResolver(profileSchema),
    defaultValues: {
      display_name: user.display_name ?? "",
    },
  });

  // Sync form when user data changes (e.g. after a successful save)
  useEffect(() => {
    reset({ display_name: user.display_name ?? "" });
  }, [user, reset]);

  async function onSubmit(values: ProfileValues) {
    const { error } = await updateMe({
      client: await getClient(),
      body: { display_name: values.display_name },
    });
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not update profile"));
      return;
    }
    await queryClient.invalidateQueries({ queryKey: ["me"] });
    toast.success("Profile updated");
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-5">
      {/* Avatar + identity */}
      <div className="flex items-center gap-4">
        <AvatarDisplay displayName={user.display_name} handle={user.handle} hasAvatar={user.has_avatar} />
        <div className="min-w-0">
          <p className="truncate font-medium">
            {user.display_name?.trim() || `@${user.handle}`}
          </p>
          <p className="text-sm text-muted-foreground">@{user.handle}</p>
        </div>
      </div>

      <div className="h-px bg-border" />

      {/* Fields */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="profile-name">Display name</Label>
          <Input
            id="profile-name"
            placeholder="Your name"
            autoComplete="given-name"
            aria-invalid={!!errors.display_name}
            {...register("display_name")}
          />
          {errors.display_name ? (
            <p className="text-xs text-destructive">{errors.display_name.message}</p>
          ) : (
            <p className="text-xs text-muted-foreground">
              Shown on your profile and published to Bluesky.
            </p>
          )}
        </div>
      </div>

      <div className="flex justify-end">
        <Button type="submit" disabled={isSubmitting || !isDirty}>
          {isSubmitting && <Loader2 className="size-4 animate-spin" />}
          Save changes
        </Button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------------------
// Danger zone
// ---------------------------------------------------------------------------

function DangerZone({ userHandle }: { userHandle: string }) {
  const router = useRouter();

  async function handleDelete() {
    const { error } = await deleteMe({ client: await getClient() });
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not delete account"));
      throw error;
    }
    // Best-effort sign out before redirecting
    await signout();
    router.push("/login");
    router.refresh();
  }

  return (
    <section
      aria-labelledby="danger-zone-heading"
      className="rounded-lg border border-border p-4"
    >
      <h2
        id="danger-zone-heading"
        className="text-sm font-medium text-muted-foreground"
      >
        Danger zone
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Permanently delete your account and all of your data, including feeds,
        folders, entries, and API tokens. This cannot be undone.
      </p>
      <div className="mt-4">
        <ConfirmDialog
          trigger={
            <Button variant="ghost" size="sm" className="text-muted-foreground">
              Delete account
            </Button>
          }
          title="Delete your account?"
          description={`All data for @${userHandle} will be permanently deleted. This cannot be undone.`}
          confirmLabel="Yes, delete my account"
          onConfirm={handleDelete}
        />
      </div>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Handle form
// ---------------------------------------------------------------------------

const handleSchema = z.object({
  handle: z
    .string()
    .min(3, "Handle must be at least 3 characters")
    .max(64, "Handle must be 64 characters or fewer")
    .regex(/^[a-zA-Z0-9_-]+$/, "Only letters, digits, hyphens, and underscores"),
  bio: z.string().max(500, "Bio must be 500 characters or fewer").optional(),
});

type HandleValues = z.infer<typeof handleSchema>;

function HandleForm({ currentHandle, currentBio, hasDID }: { currentHandle?: string; currentBio?: string; hasDID: boolean }) {
  const queryClient = useQueryClient();

  const {
    register,
    handleSubmit,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<HandleValues>({
    resolver: zodResolver(handleSchema),
    defaultValues: {
      handle: currentHandle ?? "",
      bio: currentBio ?? "",
    },
  });

  async function onSubmit(values: HandleValues) {
    const { error } = await updateHandle({
      client: await getClient(),
      body: { handle: values.handle, bio: values.bio ?? "" },
    });
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not update handle"));
      return;
    }
    await queryClient.invalidateQueries({ queryKey: ["me"] });
    toast.success("Profile updated");
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="profile-handle">Handle</Label>
        <div className="relative">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground text-sm">
            @
          </span>
          <Input
            id="profile-handle"
            placeholder="yourhandle"
            autoComplete="off"
            className="pl-7"
            readOnly={hasDID}
            aria-invalid={!!errors.handle}
            aria-describedby={hasDID ? "profile-handle-help" : undefined}
            {...register("handle")}
          />
        </div>
        {errors.handle ? (
          <p className="text-xs text-destructive">{errors.handle.message}</p>
        ) : hasDID ? (
          <p id="profile-handle-help" className="text-xs text-muted-foreground">
            Your handle is part of your AT Protocol identity and is managed via
            your PDS. Update it from a Bluesky client.
          </p>
        ) : (
          <p className="text-xs text-muted-foreground">
            Your public handle on this Sunred instance.
          </p>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="profile-bio">Bio</Label>
        <textarea
          id="profile-bio"
          placeholder="Tell people about yourself…"
          rows={3}
          className={cn(
            "flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm",
            "placeholder:text-muted-foreground focus-visible:outline-none",
            "focus-visible:ring-3 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50",
            "resize-none",
          )}
          aria-invalid={!!errors.bio}
          {...register("bio")}
        />
        {errors.bio ? (
          <p className="text-xs text-destructive">{errors.bio.message}</p>
        ) : (
          <p className="text-xs text-muted-foreground">
            Published as your Bluesky profile description.
          </p>
        )}
      </div>

      <div className="flex justify-end">
        <Button type="submit" disabled={isSubmitting || !isDirty}>
          {isSubmitting && <Loader2 className="size-4 animate-spin" />}
          Save
        </Button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------------------
// Skeleton
// ---------------------------------------------------------------------------

function ProfileSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-4">
        <Skeleton className="size-16 rounded-full shrink-0" />
        <div className="flex flex-col gap-2">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-48" />
        </div>
      </div>
      <div className="h-px bg-border" />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-9 w-full" />
        </div>
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-9 w-full" />
        </div>
      </div>
      <div className="flex justify-end">
        <Skeleton className="h-9 w-28 rounded-md" />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main export
// ---------------------------------------------------------------------------

export function ProfileManager() {
  const { data: user, isLoading } = useQuery<UserType>({
    queryKey: ["me"],
    queryFn: async () => unwrap(getMe({ client: await getClient() })),
  });

  return (
    <div className="mx-auto w-full max-w-2xl px-4 py-6 sm:px-6">
      <header>
        <h1 className="flex items-center gap-2 font-serif text-2xl font-bold tracking-normal">
          <User className="size-5" />
          Profile
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {user?.did
            ? "Your display name and bio are published to your Bluesky profile."
            : "Manage your display name and account."}
        </p>
        {user?.did && (
          <a
            href={`https://bsky.app/profile/${user.did}`}
            target="_blank"
            rel="noreferrer"
            className="mt-1.5 inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:underline"
          >
            View on Bluesky
            <ExternalLink className="size-3" />
          </a>
        )}
      </header>

      <div className="mt-6 flex flex-col gap-8">
        <section aria-label="Profile information">
          {isLoading || !user ? (
            <ProfileSkeleton />
          ) : (
            <ProfileForm user={user} />
          )}
        </section>

        <div className="h-px bg-border" />

        <section aria-labelledby="handle-section-heading">
          <h2 id="handle-section-heading" className="mb-4 text-base font-semibold">
            Handle and bio
          </h2>
          {isLoading || !user ? (
            <div className="flex flex-col gap-4">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : (
            <HandleForm currentHandle={user.handle ?? ""} currentBio={user.bio ?? ""} hasDID={!!user.did} />
          )}
        </section>

        {user && (
          <>
            <div className="h-px bg-border" />
            <DangerZone userHandle={user.handle ?? ""} />
          </>
        )}
      </div>
    </div>
  );
}
