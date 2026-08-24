import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";

export const metadata = { title: "Feeds" };

/**
 * The feeds timeline: the latest articles from all of the viewer's
 * subscriptions. Discovery of a feed by URL lives at `/feeds/discover`.
 */
export default function FeedsPage() {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="Feeds" />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ source: "feeds" }}
            emptyTitle="No articles from your feeds"
            emptyDescription="Subscribe to RSS feeds and the latest articles from your subscriptions will appear here."
          />
        </div>
      </ScrollArea>
    </div>
  );
}
