import type { Metadata } from "next";
import { getClient, getUserProfile } from "@/lib/sunred";
import type { PublicProfileResponse } from "@/lib/types";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ handle: string }>;
}): Promise<Metadata> {
  const { handle } = await params;
  try {
    const { data } = await getUserProfile({ client: await getClient(), path: { handle } });
    if (data) {
      const profile = data as PublicProfileResponse;
      const name = profile.profile.display_name?.trim();
      return { title: name || `@${profile.profile.handle}` };
    }
  } catch {
    // metadata is best-effort; fall through to default
  }
  return { title: `@${handle}` };
}

export default function UserLayout({ children }: { children: React.ReactNode }) {
  return children;
}
