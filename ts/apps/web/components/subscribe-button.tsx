"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Loader2, Check } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { getClient, createFeed } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";

/**
 * The Subscribe action used in the feed discovery toolbar. On click it
 * subscribes to the feed, invalidates feeds/entries, and navigates to the
 * new feed's page. After a successful subscribe the button briefly shows a
 * "Subscribed" state before navigating, so the click is acknowledged even if
 * the redirect is slow.
 */
export function SubscribeButton({
  feedUrl,
  feedTitle,
  size = "sm",
}: {
  feedUrl: string;
  feedTitle?: string;
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
