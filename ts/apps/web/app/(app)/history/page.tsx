import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";

export const metadata = { title: "History" };

export default function HistoryPage() {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="History" />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{}}
            history
            emptyTitle="Nothing in your history"
            emptyDescription="Articles you read show up here in the order you read them. Read something to get started."
            emptyAction={null}
            animateExit
          />
        </div>
      </ScrollArea>
    </div>
  );
}
