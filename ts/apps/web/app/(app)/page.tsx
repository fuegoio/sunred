import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { MarkAllReadButton } from "@/components/mark-all-read-button";
import { ScrollArea } from "@workspace/ui/components/scroll-area";
import { getClient, refreshAllFeeds } from "@/lib/sunred";

export const metadata = { title: "Unread" };

export default async function UnreadPage() {
  // Refresh all due feeds server-side so the latest articles are fetched
  // before the timeline renders. Errors are non-fatal — the page still
  // renders with whatever entries were already stored.
  try {
    await refreshAllFeeds({ client: await getClient() });
  } catch {
    // refresh is best-effort
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="Unread" actions={<MarkAllReadButton />} />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ status: "unread" }}
            emptyTitle="You're all caught up"
            emptyDescription="Nothing left to read. Enjoy the quiet — new articles will land here when your feeds update."
            emptyVariant="celebration"
            animateExit
          />
        </div>
      </ScrollArea>
    </div>
  );
}
