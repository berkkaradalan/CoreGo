import type { Metadata, Viewport } from "next";
import { Geist_Mono } from "next/font/google";

import { CopyButtons } from "@/components/CopyButtons";
import { themeScript } from "@/components/ThemeToggle";
import { SITE_NAME, TAGLINE } from "@/lib/site";

import "./globals.css";

const mono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-geist-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: {
    default: `${SITE_NAME} · Backend SDK for Go`,
    template: `%s · ${SITE_NAME}`,
  },
  description: TAGLINE,
  openGraph: {
    title: `${SITE_NAME} · Backend SDK for Go`,
    description: TAGLINE,
    type: "website",
  },
};

export const viewport: Viewport = {
  themeColor: "#0c0e12",
  colorScheme: "dark light",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="dark" className={mono.variable} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        {children}
        <CopyButtons />
      </body>
    </html>
  );
}
