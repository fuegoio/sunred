import { PageHeader } from "@/components/page-header";
import { EntryTimeline } from "@/components/entry-timeline";
import { ScrollArea } from "@workspace/ui/components/scroll-area";

export const metadata = { title: "Starred" };

export default function StarredPage() {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="mx-auto w-full max-w-3xl shrink-0">
        <PageHeader title="Starred" />
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="mx-auto w-full max-w-3xl">
          <EntryTimeline
            filter={{ starred: true }}
            emptyTitle="No starred articles"
            emptyDescription="Star articles you want to keep — they'll show up here for quick access."
            emptyAction={null}
            animateExit
          />
        </div>
      </ScrollArea>
    </div>
  );
}
