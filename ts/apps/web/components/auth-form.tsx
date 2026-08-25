"use client";

import { useState } from "react";
import { useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Field, FieldDescription, FieldError, FieldLabel } from "@workspace/ui/components/field";
import { loginWithDefaultPDS, loginWithHandle, safeRedirect } from "@/lib/auth";
import { handleSchema, type HandleValues } from "@/lib/schemas";
import { AnimatePresence, motion } from "motion/react";

function pdsHostname(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

export function AuthForm({ defaultPds }: { defaultPds: string }) {
  const searchParams = useSearchParams();
  const redirect = safeRedirect(searchParams.get("redirect"));
  const [isSubmitting, setIsSubmitting] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    formState: { errors },
  } = useForm<HandleValues>({
    resolver: zodResolver(handleSchema),
  });

  const handle = watch("handle")?.trim() ?? "";
  const hasHandle = handle.length > 0;

  function handlePDSLogin() {
    setIsSubmitting(true);
    loginWithDefaultPDS(redirect);
  }

  function onSubmit(values: HandleValues) {
    setIsSubmitting(true);
    // Full-page navigation to the API's OAuth login endpoint. The API
    // resolves the handle, sends a PAR request to the user's PDS, and
    // redirects the browser to the PDS to approve.
    loginWithHandle(values.handle.trim().toLowerCase(), redirect);
  }

  return (
    <div className="flex flex-col gap-6">
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4" noValidate>
        <Field data-invalid={!!errors.handle}>
          <FieldLabel htmlFor="handle">Handle</FieldLabel>
          <div className="relative">
            <span
              aria-hidden="true"
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-base text-muted-foreground select-none"
            >
              @
            </span>
            <Input
              id="handle"
              type="text"
              placeholder="alice.bsky.social"
              autoComplete="username"
              autoFocus
              className="h-11 pl-7 text-base"
              aria-invalid={!!errors.handle}
              {...register("handle")}
            />
          </div>
          {errors.handle ? (
            <FieldError>{errors.handle.message}</FieldError>
          ) : (
            <FieldDescription>
              Use your{" "}
              <a
                href="https://atproto.com"
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium text-foreground hover:underline"
              >
                AT Protocol
              </a>{" "}
              handle to log in. If you&apos;re unsure, this is likely your Bluesky (
              <code className="text-xs">.bsky.social</code>) account.
            </FieldDescription>
          )}
        </Field>

        <AnimatePresence mode="wait" initial={false}>
          {hasHandle ? (
            <motion.div
              key="login"
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.2, ease: "easeInOut" }}
              className="overflow-hidden"
            >
              <Button type="submit" size="lg" disabled={isSubmitting} className="w-full">
                {isSubmitting && <Loader2 className="size-4 animate-spin" />}
                Login
              </Button>
            </motion.div>
          ) : null}
        </AnimatePresence>
      </form>

      <AnimatePresence mode="wait" initial={false}>
        {defaultPds && !hasHandle ? (
          <motion.div
            key="pds"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            transition={{
              height: { duration: 0.3, ease: "easeInOut" },
              opacity: { duration: 0.12, ease: "easeInOut" },
            }}
            className="overflow-hidden"
          >
            <div className="flex flex-col gap-6">
              <div className="relative py-1">
                <div className="absolute inset-0 flex items-center">
                  <span className="w-full border-t" />
                </div>
                <div className="relative flex justify-center text-xs uppercase">
                  <span className="bg-background px-2 text-muted-foreground">
                    or use our hosted PDS
                  </span>
                </div>
              </div>
              <Button
                type="button"
                size="lg"
                disabled={isSubmitting}
                onClick={handlePDSLogin}
                className="w-full"
              >
                {isSubmitting && <Loader2 className="size-4 animate-spin" />}
                Continue with {pdsHostname(defaultPds)}
              </Button>
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
