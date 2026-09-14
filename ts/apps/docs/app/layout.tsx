import type { Metadata } from "next";
import Script from "next/script";
import { RootProvider } from "fumadocs-ui/provider/next";
import { Geist_Mono, Merriweather, IBM_Plex_Sans } from "next/font/google";
import "./globals.css";

const ibmPlexSans = IBM_Plex_Sans({ subsets: ["latin"], variable: "--font-sans" });
const merriweather = Merriweather({ subsets: ["latin"], variable: "--font-serif" });
const geistMono = Geist_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const metadata: Metadata = {
  title: "Sunred Docs",
  description: "Documentation for Sunred, a self-hosted RSS reader",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${ibmPlexSans.variable} ${geistMono.variable} ${merriweather.variable}`}
    >
      <body className="flex flex-col min-h-screen">
        <RootProvider>{children}</RootProvider>
        {process.env.NODE_ENV === "production" && (
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
