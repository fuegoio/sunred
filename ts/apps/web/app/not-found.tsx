import Link from "next/link";
import { Logo } from "@/components/logo";
import { buttonVariants } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";

export default function NotFound() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-8 p-6">
      <Link href="/" className="flex flex-col items-center gap-3">
        <Logo className="size-14" />
        <span className="font-serif text-2xl font-bold">Sunred</span>
      </Link>
      <div className="flex max-w-sm flex-col items-center gap-2 text-center">
        <h1 className="font-serif text-2xl font-bold tracking-normal text-balance">
          Page not found
        </h1>
        <p className="text-sm text-muted-foreground">
          The link may be mistyped, or the page may have moved.
        </p>
        <Link href="/" className={cn(buttonVariants({ size: "sm" }), "mt-4")}>
          Go to your timeline
        </Link>
      </div>
    </div>
  );
}
