"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { Camera, Loader2, Trash2, User } from "lucide-react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { UserAvatar } from "@/components/user-avatar";
import {
  avatarUrl,
  deleteMe,
  deleteMeAvatar,
  getClient,
  getMe,
  unwrap,
  updateMe,
  uploadMeAvatar,
} from "@/lib/sunred";
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
  bio: z.string().max(500, "Bio must be 500 characters or fewer"),
});

type ProfileValues = z.infer<typeof profileSchema>;

const BIO_MAX = 500;

// ---------------------------------------------------------------------------
// Avatar uploader
// ---------------------------------------------------------------------------

const AVATAR_TYPES = ["image/jpeg", "image/png", "image/webp"];
const AVATAR_MAX_BYTES = 2 * 1024 * 1024;

function AvatarUploader({
  user,
  version,
  onUploaded,
  onRemoved,
}: {
  user: UserType;
  version: number;
  onUploaded: () => void;
  onRemoved: () => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);

  const src = avatarUrl(user.handle, user.has_avatar);
  const versionedSrc = src ? `${src}?v=${version}` : undefined;

  async function handleFile(file: File) {
    if (!AVATAR_TYPES.includes(file.type)) {
      toast.error("Avatar must be a JPEG, PNG, or WebP image");
      return;
    }
    if (file.size > AVATAR_MAX_BYTES) {
      toast.error("Avatar must be 2 MiB or smaller");
      return;
    }
    setUploading(true);
    const { error } = await uploadMeAvatar({
      client: await getClient(),
      body: { avatar: file },
    });
    setUploading(false);
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not upload avatar"));
      return;
    }
    onUploaded();
    toast.success("Avatar updated");
  }

  async function handleRemove() {
    setUploading(true);
    const { error } = await deleteMeAvatar({ client: await getClient() });
    setUploading(false);
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not remove avatar"));
      return;
    }
    onRemoved();
    toast.success("Avatar removed");
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-4">
        <div className="relative shrink-0">
          <button
            type="button"
            onClick={() => inputRef.current?.click()}
            disabled={uploading}
            aria-label="Upload avatar"
            className="group relative size-20 overflow-hidden rounded-full focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/30 disabled:cursor-not-allowed"
          >
            <UserAvatar
              displayName={user.display_name}
              handle={user.handle}
              src={versionedSrc}
              className="size-20 text-2xl font-semibold"
            />
            <span
              aria-hidden="true"
              className="absolute inset-0 flex flex-col items-center justify-center gap-0.5 bg-black/50 text-white opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100"
            >
              <Camera className="size-4" />
              <span className="text-[10px] font-medium">Upload</span>
            </span>
            {uploading && (
              <span className="absolute inset-0 flex items-center justify-center bg-black/50 text-white">
                <Loader2 className="size-5 animate-spin" />
              </span>
            )}
          </button>
          <input
            ref={inputRef}
            type="file"
            accept={AVATAR_TYPES.join(",")}
            className="sr-only"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) handleFile(file);
              // Reset so picking the same file twice still fires change.
              e.target.value = "";
            }}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={uploading}
            onClick={() => inputRef.current?.click()}
          >
            {user.has_avatar ? "Change" : "Upload"}
          </Button>
          {user.has_avatar && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={uploading}
              onClick={handleRemove}
              className="text-muted-foreground"
            >
              <Trash2 className="size-3.5" />
              Remove
            </Button>
          )}
        </div>
      </div>
      <p className="text-xs text-muted-foreground">
        JPEG, PNG, or WebP. Uploaded to your Bluesky profile.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Profile form
// ---------------------------------------------------------------------------

function ProfileForm({
  user,
  avatarVersion,
  onAvatarChanged,
}: {
  user: UserType;
  avatarVersion: number;
  onAvatarChanged: () => void;
}) {
  const queryClient = useQueryClient();

  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<ProfileValues>({
    resolver: zodResolver(profileSchema),
    defaultValues: {
      display_name: user.display_name ?? "",
      bio: user.bio ?? "",
    },
  });

  const bioValue = watch("bio") ?? "";

  async function onSubmit(values: ProfileValues) {
    const { error } = await updateMe({
      client: await getClient(),
      body: { display_name: values.display_name, bio: values.bio },
    });
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not update profile"));
      return;
    }
    await queryClient.invalidateQueries({ queryKey: ["me"] });
    // Re-sync the form to the saved values so isDirty clears.
    reset({ display_name: values.display_name, bio: values.bio });
    toast.success("Profile updated");
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-5">
      <AvatarUploader
        user={user}
        version={avatarVersion}
        onUploaded={onAvatarChanged}
        onRemoved={onAvatarChanged}
      />

      <div className="h-px bg-border" />

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

        <div className="flex flex-col gap-1.5">
          <div className="flex items-baseline justify-between gap-2">
            <Label htmlFor="profile-bio">Bio</Label>
            <span
              className={cn(
                "text-xs tabular-nums",
                bioValue.length > BIO_MAX - 20 ? "text-destructive" : "text-muted-foreground",
              )}
              aria-live="polite"
            >
              {bioValue.length}/{BIO_MAX}
            </span>
          </div>
          <textarea
            id="profile-bio"
            placeholder="Tell people about yourself…"
            rows={4}
            maxLength={BIO_MAX}
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

        {/* Read-only handle: not an input. Managed via the user's Bluesky account. */}
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="profile-handle-display">Handle</Label>
          <p
            id="profile-handle-display"
            className="min-h-9 rounded-md border border-input bg-muted/40 px-3 py-2 text-sm text-muted-foreground"
          >
            @{user.handle}
          </p>
          <p className="text-xs text-muted-foreground">
            Your handle is your AT Protocol identity. Change it from a Bluesky
            client — Sunred mirrors it from your account.
          </p>
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
// Skeleton
// ---------------------------------------------------------------------------

function ProfileSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-4">
        <Skeleton className="size-20 rounded-full shrink-0" />
        <div className="flex flex-col gap-2">
          <Skeleton className="h-8 w-20 rounded-md" />
          <Skeleton className="h-4 w-32" />
        </div>
      </div>
      <div className="h-px bg-border" />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-9 w-full" />
        </div>
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-24 w-full" />
        </div>
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-3 w-16" />
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
  const queryClient = useQueryClient();
  const { data: user, isLoading } = useQuery<UserType>({
    queryKey: ["me"],
    queryFn: async () => unwrap(getMe({ client: await getClient() })),
  });
  // Bumps the avatar <img> cache-buster after an upload/remove so the new
  // image is fetched despite the proxy's long cache headers.
  const [avatarVersion, setAvatarVersion] = useState(0);

  function handleAvatarChanged() {
    setAvatarVersion((v) => v + 1);
    void queryClient.invalidateQueries({ queryKey: ["me"] });
  }

  return (
    <div className="mx-auto w-full max-w-2xl px-4 py-6 sm:px-6">
      <header>
        <h1 className="flex items-center gap-2 font-serif text-2xl font-bold tracking-normal">
          <User className="size-5" />
          Profile
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {user?.did
            ? "Your profile is published to your Bluesky account."
            : "Manage your display name, bio, and avatar."}
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
            <ProfileForm
              key={user.id}
              user={user}
              avatarVersion={avatarVersion}
              onAvatarChanged={handleAvatarChanged}
            />
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
