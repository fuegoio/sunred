"use client";

import { useEffect } from "react";
import Link from "next/link";
import { AlertCircle } from "lucide-react";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { Button, buttonVariants } from "@workspace/ui/components/button";

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <div role="alert" className="flex h-full items-center justify-center p-4">
      <Empty className="border w-full max-w-md">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <AlertCircle className="size-6 text-destructive" />
          </EmptyMedia>
          <EmptyTitle>Something went wrong</EmptyTitle>
          <EmptyDescription>
            An unexpected error interrupted this page. Trying again usually
            fixes it.
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent className="flex-row">
          <Button onClick={() => reset()} size="sm">
            Try again
          </Button>
          <Link href="/" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Back to timeline
          </Link>
        </EmptyContent>
        {error.digest ? (
          <p className="font-mono text-xs text-muted-foreground">
            Error ID: {error.digest}
          </p>
        ) : null}
      </Empty>
    </div>
  );
}
