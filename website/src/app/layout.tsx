import { RootProvider } from "fumadocs-ui/provider/next";
import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";

import { SITE_NAME, SITE_URL, TAGLINE } from "@/lib/site";

import "./globals.css";

const sans = Geist({
  subsets: ["latin"],
  variable: "--font-geist-sans",
  display: "swap",
});

const mono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-geist-mono",
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: `${SITE_NAME} · Backend SDK for Go`,
    template: `%s · ${SITE_NAME}`,
  },
  description: TAGLINE,
  openGraph: {
    title: `${SITE_NAME} · Backend SDK for Go`,
    description: TAGLINE,
    type: "website",
    siteName: SITE_NAME,
  },
  twitter: {
    card: "summary_large_image",
    title: `${SITE_NAME} · Backend SDK for Go`,
    description: TAGLINE,
  },
};

export const viewport: Viewport = {
  // The backgrounds of the light and dark variants (src/styles/colors.css).
  themeColor: [
    { media: "(prefers-color-scheme: dark)", color: "#000000" },
    { media: "(prefers-color-scheme: light)", color: "#f4f2eb" },
  ],
  colorScheme: "dark light",
};

/** localStorage key next-themes uses for the color mode. */
const THEME_STORAGE_KEY = "corego-theme";

/**
 * Runs in <head> before first paint: `?theme=dark` or `?theme=light`
 * stores that color mode where next-themes reads it (handy for shared
 * links and screenshots). next-themes then applies it without a flash.
 */
const themeQueryScript = `(function(){try{var t=new URLSearchParams(location.search).get("theme");if(t==="dark"||t==="light")localStorage.setItem(${JSON.stringify(THEME_STORAGE_KEY)},t);}catch(e){}})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${sans.variable} ${mono.variable}`} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeQueryScript }} />
      </head>
      <body>
        <RootProvider
          theme={{
            attribute: "class",
            defaultTheme: "system",
            enableSystem: true,
            storageKey: THEME_STORAGE_KEY,
            disableTransitionOnChange: true,
          }}
          search={{
            options: { type: "static", api: "/api/search" },
            links: [
              ["Getting started", "/docs/getting-started"],
              ["Authentication", "/docs/authentication"],
              ["Web framework", "/docs/web-framework"],
              ["API reference", "/docs/api-reference"],
            ],
          }}
        >
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
