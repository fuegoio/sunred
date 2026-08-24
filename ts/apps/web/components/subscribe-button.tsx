"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Trash2, Loader2, Check } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { getClient, createFeed, deleteFeed } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";

/**
 * The primary subscription action shared by the feed detail page (subscribed)
 * and the feed discovery view (not subscribed). Sits in the same header slot
 * on both surfaces so the affordance is consistent.
 *
 * - `subscribed` → outline "Unsubscribe" button backed by a confirm dialog;
 *   on confirm it deletes the subscription, invalidates feeds/entries, and
 *   navigates home.
 * - not subscribed → primary "Subscribe" button; on click it subscribes,
 *   invalidates feeds/entries, and navigates to the new feed's page.
 *
 * After a successful subscribe the button briefly shows a "Subscribed" state
 * before navigating, so the click is acknowledged even if the redirect is slow.
 */
export function SubscribeButton({
  feedUrl,
  feedId,
  feedTitle,
  subscribed,
  size = "sm",
}: {
  feedUrl: string;
  feedId?: number;
  feedTitle?: string;
  subscribed: boolean;
  size?: "sm" | "default";
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [subscribing, setSubscribing] = useState(false);
  const [justSubscribed, setJustSubscribed] = useState(false);

  async function handleSubscribe() {
    setSubscribing(true);
    try {
      const { data, error } = await createFeed({
        client: await getClient(),
        body: { feed_url: feedUrl },
      });
      if (error) throw error;
      setJustSubscribed(true);
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success(`Subscribed to "${feedTitle || "feed"}"`);
      if (data && typeof data === "object" && "id" in data) {
        router.push(`/feeds/${(data as { id: number }).id}`);
        router.refresh();
      } else {
        router.push("/");
        router.refresh();
      }
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not subscribe to feed"));
      setSubscribing(false);
    }
  }

  async function handleUnsubscribe() {
    if (feedId === undefined) return;
    try {
      const { error } = await deleteFeed({
        client: await getClient(),
        path: { feedId },
      });
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["feeds"] });
      await queryClient.invalidateQueries({ queryKey: ["entries"] });
      toast.success(`Unsubscribed from "${feedTitle || "feed"}"`);
      router.push("/");
      router.refresh();
    } catch (err) {
      toast.error(getApiErrorMessage(err, "Could not unsubscribe from feed"));
    }
  }

  if (subscribed) {
    return (
      <ConfirmDialog
        trigger={
          <Button variant="outline" size={size} className="gap-1.5">
            <Trash2 className="size-3.5" />
            Unsubscribe
          </Button>
        }
        title="Unsubscribe from feed?"
        description={`This removes "${feedTitle || "this feed"}" and all its entries. This cannot be undone.`}
        confirmLabel="Unsubscribe"
        onConfirm={handleUnsubscribe}
      />
    );
  }

  return (
    <Button
      variant="default"
      size={size}
      onClick={handleSubscribe}
      disabled={subscribing || justSubscribed}
      className="gap-1.5"
    >
      {subscribing ? (
        <Loader2 className="size-3.5 animate-spin" />
      ) : justSubscribed ? (
        <Check className="size-3.5" />
      ) : (
        <Plus className="size-3.5" />
      )}
      {justSubscribed ? "Subscribed" : "Subscribe"}
    </Button>
  );
}
