import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";

export const metadata = { title: "Follows" };

export default function FollowsPage() {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="Follows" />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ source: "follows" }}
            emptyTitle="Nothing reposted yet"
            emptyDescription="Articles reposted by people you follow will appear here. Use the + next to Follows to find people."
          />
        </div>
      </ScrollArea>
    </div>
  );
}
