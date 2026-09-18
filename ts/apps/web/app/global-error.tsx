"use client";

import { useEffect } from "react";
import { AlertCircle } from "lucide-react";
import { Button } from "@workspace/ui/components/button";

import "@workspace/ui/globals.css";

export default function GlobalError({
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
    <html lang="en">
      <body className="bg-background font-sans text-foreground antialiased">
        <div
          role="alert"
          className="flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center"
        >
          <div className="flex size-12 items-center justify-center rounded-xl bg-destructive/10">
            <AlertCircle className="size-6 text-destructive" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-normal text-balance">
              Something went wrong
            </h1>
            <p className="mt-1 max-w-sm text-sm text-muted-foreground">
              The app failed to load. Trying again usually fixes it.
            </p>
            {error.digest ? (
              <p className="mt-3 font-mono text-xs text-muted-foreground">
                Error ID: {error.digest}
              </p>
            ) : null}
          </div>
          <Button onClick={() => reset()} size="sm">
            Try again
          </Button>
        </div>
      </body>
    </html>
  );
}
