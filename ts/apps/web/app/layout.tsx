import type { Metadata, Viewport } from "next";
import { Geist_Mono, Merriweather, IBM_Plex_Sans } from "next/font/google";
import Script from "next/script";
import { Toaster } from "@workspace/ui/components/sonner";
import { SerwistProvider } from "@serwist/turbopack/react";

import "@workspace/ui/globals.css";
import { ThemeProvider } from "@/components/theme-provider";
import { QueryProvider } from "@/components/query-provider";
import { env } from "@/lib/env";
import { PublicEnv } from "@/lib/public-env";
import { cn } from "@workspace/ui/lib/utils";

export const metadata: Metadata = {
  applicationName: "Sunred",
  title: {
    default: "Sunred",
    template: "%s — Sunred",
  },
  description: "A modern, self-hostable RSS reader with a clean REST API.",
  appleWebApp: {
    capable: true,
    statusBarStyle: "default",
    title: "Sunred",
  },
  formatDetection: {
    telephone: false,
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#fffffd" },
    { media: "(prefers-color-scheme: dark)", color: "#1a1815" },
  ],
};

const ibmPlexSans = IBM_Plex_Sans({
  subsets: ["latin"],
  variable: "--font-sans",
});

const merriweather = Merriweather({
  subsets: ["latin"],
  variable: "--font-serif",
});

const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
});

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={cn("antialiased", ibmPlexSans.variable, geistMono.variable, merriweather.variable)}
    >
      <body>
        <PublicEnv />
        <SerwistProvider swUrl="/serwist/sw.js">
          <ThemeProvider>
            <QueryProvider>
              {children}
              <Toaster />
            </QueryProvider>
          </ThemeProvider>
        </SerwistProvider>
        {env.TELEMETRY && (
          <Script
            defer
            src="https://umami.alexistac.net/script.js"
            data-website-id="c31ff124-7710-4ee7-bf24-d1cad767d72e"
          />
        )}
      </body>
    </html>
  );
}
