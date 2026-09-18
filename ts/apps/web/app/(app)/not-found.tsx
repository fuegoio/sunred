import Link from "next/link";
import { Compass } from "lucide-react";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { buttonVariants } from "@workspace/ui/components/button";

export default function NotFound() {
  return (
    <div className="flex h-full items-center justify-center p-4">
      <Empty className="border w-full max-w-md">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Compass className="size-6 text-primary" />
          </EmptyMedia>
          <EmptyTitle>Couldn&apos;t find this page</EmptyTitle>
          <EmptyDescription>
            It may have been deleted, or the link may be off by a character.
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Link href="/" className={buttonVariants({ size: "sm" })}>
            Back to timeline
          </Link>
        </EmptyContent>
      </Empty>
    </div>
  );
}
