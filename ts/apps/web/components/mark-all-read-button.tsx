"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CheckCheck } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { getClient, listEntries, updateEntries } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import type { Entry } from "@/lib/types";

export function MarkAllReadButton() {
  const queryClient = useQueryClient();
  const [pending, setPending] = useState(false);

  const { data: unreadCheck } = useQuery<Entry[]>({
    queryKey: ["entries:unread-probe"],
    queryFn: async () => {
      const result = await listEntries({
        client: await getClient(),
        query: { status: "unread", limit: 1 },
      });
      if (result.error) throw result.error;
      return (result.data ?? []) as Entry[];
    },
  });

  const hasUnread = unreadCheck !== undefined && unreadCheck.length > 0;

  async function handleClick() {
    setPending(true);
    const { error } = await updateEntries({
      client: await getClient(),
      body: { entry_ids: null, status: "read" },
    });
    setPending(false);
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not mark all as read"));
      return;
    }
    await queryClient.invalidateQueries({ queryKey: ["entries"] });
    await queryClient.invalidateQueries({ queryKey: ["entries:unread-probe"] });
  }

  return (
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label="Mark all as read"
      disabled={pending || !hasUnread}
      onClick={handleClick}
    >
      <CheckCheck className="size-3.5" />
    </Button>
  );
}
