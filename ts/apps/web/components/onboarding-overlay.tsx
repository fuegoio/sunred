"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, ChevronRight, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Logo } from "@/components/logo";
import { getClient, getMe, listFeeds, completeMeOnboarding, unwrap } from "@/lib/sunred";
import { getApiErrorMessage } from "@/lib/errors";
import type { Feed, User } from "@/lib/types";

/**
 * First-run onboarding for new accounts.
 *
 * Triggered once, after the post-login PDS sync settles, for any user whose
 * `onboarded` flag is false. The flow has two beats:
 *
 *   1. Welcome card — asks the user to add their first RSS feed (only when they
 *      have no feeds yet; users whose PDS sync already imported feeds skip
 *      straight to the tour).
 *   2. Spotlight tour — a full-viewport overlay that dims the UI and highlights
 *      the sidebar's Feeds section and the reading nav, with coachmark tooltips.
 *
 * Completing or dismissing the tour POSTs /v1/me/onboarding, which flips the
 * server-side `onboarded` flag so the overlay never re-appears for the account.
 *
 * The transient step (welcome vs. tour) is held in localStorage so the flow
 * survives the navigation to /feeds/new and back; the server flag is the source
 * of truth for whether onboarding should run at all.
 */

const STEP_KEY = "sunred.onboarding.step";
type Step = "welcome" | "tour";

function readStep(): Step | null {
  if (typeof window === "undefined") return null;
  try {
    const v = window.localStorage.getItem(STEP_KEY);
    return v === "welcome" || v === "tour" ? v : null;
  } catch {
    return null;
  }
}

function writeStep(step: Step | null) {
  if (typeof window === "undefined") return;
  try {
    if (step) window.localStorage.setItem(STEP_KEY, step);
    else window.localStorage.removeItem(STEP_KEY);
  } catch {
    // localStorage unavailable (private mode) — non-fatal; the server flag still
    // gates re-display across sessions.
  }
}

const TOUR_STEPS = [
  {
    selector: '[data-onboarding="feeds"]',
    title: "Your feeds live here",
    body: "Every site you subscribe to appears in this list. Use the + button to add another feed at any time.",
  },
  {
    selector: '[data-onboarding="unread"]',
    title: "New articles land in Unread",
    body: "Fresh posts from your feeds collect in Unread. All shows everything, and Starred keeps your favourites.",
  },
] as const;

export function OnboardingOverlay({
  initialSyncStatus,
  initialOnboarded,
  userDisplayName,
}: {
  initialSyncStatus: string;
  initialOnboarded: boolean;
  userDisplayName?: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const queryClient = useQueryClient();

  // Poll /v1/me while a sync is in flight (shares the ["me"] cache with
  // SyncStatusBar, so this adds no extra requests). Once sync settles, the
  // query goes idle and we read the final status + onboarded flag.
  const { data: me } = useQuery<User>({
    queryKey: ["me"],
    queryFn: async () => unwrap(getMe({ client: await getClient() })),
    enabled: initialSyncStatus === "syncing",
    refetchInterval: (query) =>
      query.state.data?.pds_sync_status === "syncing" ? 2000 : false,
  });

  const feedsQuery = useQuery<Feed[]>({
    queryKey: ["feeds"],
    queryFn: async () => unwrap(listFeeds({ client: await getClient() })),
  });

  const syncStatus = me?.pds_sync_status ?? initialSyncStatus;
  const onboarded = me?.onboarded ?? initialOnboarded;
  const hasFeeds = (feedsQuery.data?.length ?? 0) > 0;

  // The tour highlights sidebar elements, which only exist on non-settings
  // routes (the settings section uses a different sidebar). Onboarding is
  // about the reading surface, so we keep it to the app routes.
  const onReadingSurface = !pathname.startsWith("/settings");

  const eligible =
    syncStatus !== "syncing" && !onboarded && feedsQuery.isSuccess && onReadingSurface;

  const [step, setStep] = useState<Step | null>(null);
  const [dismissed, setDismissed] = useState(false);

  // Decide which beat to show once eligible. A user with feeds (e.g. imported
  // from their PDS) skips the welcome and goes straight to the tour.
  useEffect(() => {
    if (!eligible || dismissed) return;
    const stored = readStep();
    if (stored === "tour" || hasFeeds) {
      setStep("tour");
    } else if (stored === "welcome" || stored === null) {
      setStep((prev) => prev ?? "welcome");
    }
  }, [eligible, dismissed, hasFeeds]);

  const complete = useCallback(async () => {
    const { error } = await completeMeOnboarding({ client: await getClient() });
    if (error) {
      toast.error(getApiErrorMessage(error, "Could not finish onboarding"));
      return false;
    }
    await queryClient.invalidateQueries({ queryKey: ["me"] });
    writeStep(null);
    setStep(null);
    setDismissed(true);
    return true;
  }, [queryClient]);

  const handleAddFeed = useCallback(() => {
    writeStep("tour");
    setStep(null);
    router.push("/feeds/new");
  }, [router]);

  const handleMaybeLater = useCallback(() => {
    // Skip adding a feed but still walk through the tour so they learn the
    // layout before we send them off.
    writeStep("tour");
    setStep("tour");
  }, []);

  if (dismissed || !eligible || !step) return null;

  if (step === "welcome") {
    return (
      <WelcomeCard
        userDisplayName={userDisplayName}
        onAddFeed={handleAddFeed}
        onMaybeLater={handleMaybeLater}
      />
    );
  }

  return (
    <SpotlightTour steps={TOUR_STEPS} onComplete={complete} onSkip={complete} />
  );
}

function WelcomeCard({
  userDisplayName,
  onAddFeed,
  onMaybeLater,
}: {
  userDisplayName?: string;
  onAddFeed: () => void;
  onMaybeLater: () => void;
}) {
  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/50 backdrop-blur-[2px] animate-in fade-in duration-200"
      role="dialog"
      aria-modal="true"
      aria-label="Welcome to Sunred"
    >
      <div className="w-full max-w-md rounded-2xl border border-border bg-card p-8 text-center shadow-xl animate-in fade-in zoom-in-95 duration-200">
        <div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-primary/10">
          <Logo className="size-8" />
        </div>
        <h1 className="mt-5 font-serif text-2xl font-bold tracking-normal text-balance">
          {userDisplayName ? `Welcome, ${userDisplayName}` : "Welcome to Sunred"}
        </h1>
        <p className="mx-auto mt-2 max-w-sm text-sm text-muted-foreground text-pretty">
          Add your first RSS feed and Sunred will fetch new articles for you
          automatically. It only takes a URL.
        </p>
        <div className="mt-6 flex flex-col gap-2">
          <Button
            size="lg"
            className="bg-primary text-primary-foreground hover:bg-primary/90"
            onClick={onAddFeed}
            autoFocus
          >
            <Plus className="size-4" />
            Add your first feed
          </Button>
          <Button variant="ghost" size="lg" onClick={onMaybeLater}>
            Maybe later
          </Button>
        </div>
      </div>
    </div>
  );
}

type TourStep = (typeof TOUR_STEPS)[number];

function SpotlightTour({
  steps,
  onComplete,
  onSkip,
}: {
  steps: readonly TourStep[];
  onComplete: () => Promise<boolean>;
  onSkip: () => Promise<boolean>;
}) {
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<DOMRect | null>(null);
  const [placement, setPlacement] = useState<{ top: number; left: number } | null>(null);
  const [finishing, setFinishing] = useState(false);
  const step = steps[index];
  const isLast = index === steps.length - 1;
  const selector = step?.selector;

  // Measure the highlighted target and recompute on resize/scroll. The sidebar
  // is fixed within the viewport, so scroll/resize are the only movements.
  const measure = useCallback(() => {
    if (!selector) {
      setRect(null);
      setPlacement(null);
      return;
    }
    const el = document.querySelector(selector) as HTMLElement | null;
    if (!el) {
      setRect(null);
      setPlacement(null);
      return;
    }
    const r = el.getBoundingClientRect();
    // A display:none element (e.g. the sidebar below the lg breakpoint) is
    // present in the DOM but has a zero-size rect — there's nothing to
    // highlight, so treat it as a missing target.
    if (r.width === 0 || r.height === 0) {
      setRect(null);
      setPlacement(null);
      return;
    }
    setRect(r);
    setPlacement(placeCoachmark(r));
  }, [selector]);

  useEffect(() => {
    measure();
    window.addEventListener("resize", measure);
    window.addEventListener("scroll", measure, true);
    return () => {
      window.removeEventListener("resize", measure);
      window.removeEventListener("scroll", measure, true);
    };
  }, [measure]);

  const next = useCallback(() => {
    if (isLast) {
      setFinishing(true);
      void onComplete().finally(() => setFinishing(false));
    } else {
      setIndex((i) => i + 1);
    }
  }, [isLast, onComplete]);

  const skip = useCallback(() => {
    setFinishing(true);
    void onSkip().finally(() => setFinishing(false));
  }, [onSkip]);

  // No target found — the sidebar isn't rendered (below the lg breakpoint the
  // sidebar is a closed drawer, or we're on a route without the app sidebar).
  // Rather than stepping the user through invisible coachmarks, complete the
  // tour so onboarding ends cleanly. A short grace period lets a transient
  // layout settle before we give up.
  useEffect(() => {
    if (rect !== null) return;
    const t = setTimeout(() => {
      setFinishing(true);
      void onComplete().finally(() => setFinishing(false));
    }, 200);
    return () => clearTimeout(t);
  }, [rect, onComplete]);

  return (
    <>
      {/* Click-capture backdrop: blocks interaction with the UI beneath the
          tour. Transparent itself; the darkening comes from the spotlight's
          box-shadow so the cutout stays crisp. */}
      <div className="fixed inset-0 z-[60]" aria-hidden="true" />

      {/* Spotlight cutout around the target. */}
      {rect && step && (
        <div
          className="fixed z-[61] rounded-lg border-2 border-primary pointer-events-none transition-all duration-150 ease-out"
          style={{
            top: rect.top - 4,
            left: rect.left - 4,
            width: rect.width + 8,
            height: rect.height + 8,
            boxShadow: "0 0 0 9999px rgba(0, 0, 0, 0.55)",
          }}
        />
      )}

      {/* Coachmark. */}
      {rect && placement && step && (
        <div
          className="fixed z-[62] w-72 rounded-lg border border-border bg-popover p-4 shadow-xl"
          style={{ top: placement.top, left: placement.left }}
          role="dialog"
          aria-modal="true"
          aria-label={step.title}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">
              {index + 1} of {steps.length}
            </span>
            <button
              type="button"
              onClick={skip}
              disabled={finishing}
              className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/30 disabled:opacity-50"
              aria-label="Skip tour"
            >
              <X className="size-4" />
            </button>
          </div>
          <h2 className="mt-2 font-medium text-foreground">{step.title}</h2>
          <p className="mt-1 text-sm text-muted-foreground text-pretty">{step.body}</p>
          <div className="mt-4 flex items-center justify-end gap-2">
            {!isLast && (
              <button
                type="button"
                onClick={skip}
                disabled={finishing}
                className="text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/30 rounded-sm disabled:opacity-50"
              >
                Skip
              </button>
            )}
            <Button size="sm" onClick={next} disabled={finishing} autoFocus>
              {isLast ? "Got it" : "Next"}
              {!isLast && <ChevronRight className="size-4" />}
            </Button>
          </div>
        </div>
      )}
    </>
  );
}

/**
 * Position the coachmark to the right of the target when there's room (the
 * targets live in the left sidebar, so the main content area is open to the
 * right), otherwise below it. Falls back to the viewport centre if the target
 * rect is missing.
 */
function placeCoachmark(r: DOMRect): { top: number; left: number } {
  const CARD_W = 288; // w-72
  const GAP = 16;

  // Prefer to the right of the target.
  const rightLeft = r.right + GAP;
  if (rightLeft + CARD_W <= window.innerWidth - 8) {
    const top = Math.min(
      Math.max(8, r.top + r.height / 2 - 60),
      window.innerHeight - 200,
    );
    return { top, left: rightLeft };
  }

  // Otherwise below the target.
  const belowTop = r.bottom + GAP;
  if (belowTop + 180 <= window.innerHeight - 8) {
    const left = Math.min(
      Math.max(8, r.left),
      window.innerWidth - CARD_W - 8,
    );
    return { top: belowTop, left };
  }

  // Last resort: above the target.
  const top = Math.max(8, r.top - GAP - 180);
  const left = Math.min(Math.max(8, r.left), window.innerWidth - CARD_W - 8);
  return { top, left };
}
