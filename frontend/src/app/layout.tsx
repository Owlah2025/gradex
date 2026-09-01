import type { Metadata, Viewport } from "next";
import { Alexandria, IBM_Plex_Sans_Arabic, IBM_Plex_Mono } from "next/font/google";
import { Providers } from "@/components/providers";
import { SkipLink } from "@/components/common/skip-link";
import { siteConfig } from "@/config/site";
import { defaultLocale, localeDir } from "@/lib/i18n/config";
import "./globals.css";

const display = Alexandria({
  subsets: ["latin", "arabic"],
  weight: ["400", "500", "600", "700", "800"],
  variable: "--font-display",
  display: "swap",
});

const body = IBM_Plex_Sans_Arabic({
  subsets: ["latin", "arabic"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-body",
  display: "swap",
});

const mono = IBM_Plex_Mono({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  variable: "--font-mono",
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL(siteConfig.url),
  title: {
    default: `${siteConfig.name} — Graduate with excellence`,
    template: `%s · ${siteConfig.name}`,
  },
  description: siteConfig.description,
  applicationName: siteConfig.name,
  keywords: [...siteConfig.keywords],
  authors: [{ name: siteConfig.name }],
  creator: siteConfig.name,
  alternates: {
    canonical: "/",
    languages: { en: "/", ar: "/" },
  },
  openGraph: {
    type: "website",
    locale: siteConfig.locale,
    url: siteConfig.url,
    siteName: siteConfig.name,
    title: `${siteConfig.name} — Graduate with excellence`,
    description: siteConfig.description,
  },
  twitter: {
    card: "summary_large_image",
    title: `${siteConfig.name} — Graduate with excellence`,
    description: siteConfig.description,
  },
  robots: {
    index: true,
    follow: true,
    googleBot: { index: true, follow: true, "max-image-preview": "large" },
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f8fafc" },
    { media: "(prefers-color-scheme: dark)", color: "#0d1b2a" },
  ],
};

/**
 * The root document.
 *
 * # WHY THIS DOES NOT READ THE LOCALE COOKIE
 *
 * It did, briefly, so that `<html lang>` and the first-rendered dictionary
 * could be correct on the very first byte for the routes that carry no locale
 * segment. Calling `cookies()` here makes the root layout dynamic, and a
 * dynamic *root* layout is not a local cost: every client navigation in the
 * product then has to re-render and re-serialize this layout on the server,
 * because the router can no longer treat the shell as unchanged and start from
 * the segment that actually differs.
 *
 * Measured on the Admin review workspace, one soft navigation went from ~2.0s
 * to ~4.3s. That is a regression for every reader on every navigation, paid to
 * remove one frame of wrong language on four unprefixed screens.
 *
 * So the shell is static again, and the language is resolved where it is
 * genuinely known:
 *
 *   - `/[locale]/…` — the URL names it. `LocaleProvider` reads it with
 *     `usePathname`, which is correct during server rendering too, so the
 *     catalogue, the learning surfaces and the workspaces render the right
 *     dictionary on the first byte with no cookie and no dynamic rendering.
 *     These are the routes the reported defect was actually about.
 *
 *   - everywhere else — `/`, the admission screens, `/staff` — the stored
 *     preference is applied as the provider mounts, and `<html lang>`/`dir`
 *     follow it. One corrected frame on those four screens is the accepted
 *     cost; a slower product on every screen was not.
 */
export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html
      lang={defaultLocale}
      dir={localeDir[defaultLocale]}
      suppressHydrationWarning
      className={`${display.variable} ${body.variable} ${mono.variable}`}
    >
      <body>
        <Providers>
          <SkipLink />
          {children}
        </Providers>
      </body>
    </html>
  );
}
