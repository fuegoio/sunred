import { Suspense } from "react";
import { AuthForm } from "@/components/auth-form";
import { redirectIfAuthenticated } from "@/lib/auth-guard";
import { env } from "@/lib/env";

export const metadata = { title: "Login" };

const ERROR_MESSAGES: Record<string, string> = {
  bad_handle: "We couldn't find that handle. Check it and try again.",
  oauth_failed: "Login was cancelled or failed. Please try again.",
  login_failed: "Could not start login. Please try again.",
  internal: "Something went wrong on our side. Please try again.",
  signup_failed: "Could not start signup. Please try again.",
};

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ redirect?: string; error?: string }>;
}) {
  const { redirect: redirectTo, error } = await searchParams;
  // If the visitor already has a valid session, skip the form and go to the
  // redirect target (or home). 4xx/5xx from the API → render the form.
  await redirectIfAuthenticated(redirectTo);

  const errorMessage = error ? ERROR_MESSAGES[error] ?? "Login failed. Please try again." : null;

  return (
    <Suspense>
      <div className="flex flex-col gap-4">
        {errorMessage && (
          <div
            role="alert"
            className="rounded-lg border border-destructive/40 bg-destructive/8 px-4 py-3 text-sm text-destructive"
          >
            {errorMessage}
          </div>
        )}
        <AuthForm defaultPds={env.NEXT_PUBLIC_SUNRED_DEFAULT_PDS} />
      </div>
    </Suspense>
  );
}
