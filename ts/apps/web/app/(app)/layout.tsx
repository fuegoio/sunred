import { redirect } from "next/navigation";
import { getClient, getMe } from "@/lib/sunred";
import { apiErrorStatus, getApiErrorMessage, isClientError } from "@/lib/errors";
import { ApiError } from "@/components/api-error";
import { AppShell } from "@/components/app-shell";

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const client = await getClient();

  let user: Awaited<ReturnType<typeof getMe>>["data"];
  let err: unknown;
  try {
    const result = await getMe({ client });
    if (result.error) {
      err = result.error;
    } else {
      user = result.data;
    }
  } catch (e) {
    // Network failure — the API is unreachable (down, DNS, etc.).
    err = e;
  }

  // 4xx (expired/invalid session) → send to login.
  if (err !== undefined && isClientError(err)) {
    redirect("/login");
  }
  // No user and no error response → treat as unauthenticated.
  if (user === undefined && err === undefined) {
    redirect("/login");
  }
  // 5xx / network failure → show an error instead of logging the user out,
  // so an API outage doesn't kick them to /login (which previously looped
  // with the proxy's cookie-presence redirect).
  if (user === undefined) {
    const status = apiErrorStatus(err) ?? 503;
    return (
      <div className="flex h-svh items-center justify-center p-8">
        <ApiError
          status={status}
          message={getApiErrorMessage(err, "The API is unreachable. Please try again later.")}
          className="max-w-md"
        />
      </div>
    );
  }

  return <AppShell userHandle={user.handle ?? ""} userDisplayName={user.display_name} userHasAvatar={user.has_avatar} pdsSyncStatus={user.pds_sync_status}>{children}</AppShell>;
}
