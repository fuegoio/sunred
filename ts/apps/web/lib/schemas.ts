import { z } from "zod";

// ATProto handle syntax (https://atproto.com/specs/handle#handle-identifier-syntax).
// Dot-separated labels (at least two segments), each 1-63 chars of ASCII
// letters/digits/hyphens (no leading/trailing hyphen); the final segment (TLD)
// must start with a letter. Case-insensitive; normalize to lowercase on submit.
const HANDLE_RE =
  /^([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/;

export const handleSchema = z.object({
  handle: z
    .string()
    .trim()
    .min(1, "Enter your AT Proto handle")
    .max(253, "Handle is too long")
    .refine((v) => !v.includes(" "), "Handle cannot contain spaces")
    .refine((v) => HANDLE_RE.test(v), "Enter a valid handle (e.g. alice.bsky.social)"),
});

export type HandleValues = z.infer<typeof handleSchema>;

export const subscribeFeedSchema = z.object({
  feed_url: z
    .string()
    .min(1, "Enter a URL")
    .max(2048, "URL is too long")
    .refine((val) => {
      try {
        // Accept URLs with or without a scheme.
        const withScheme = /^https?:\/\//i.test(val) ? val : `https://${val}`;
        new URL(withScheme);
        return true;
      } catch {
        return false;
      }
    }, "Enter a valid URL"),
});

export function normalizeFeedURL(input: string): string {
  const trimmed = input.trim();
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  return `https://${trimmed}`;
}

export type SubscribeFeedValues = z.infer<typeof subscribeFeedSchema>;
