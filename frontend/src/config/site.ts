export const siteConfig = {
  name: "Gradex",
  wordmark: { lead: "Grade", accent: "x" },
  url: "https://gradex.com",
  ogImage: "/og.png",
  description:
    "Published university Course details and authorized learning access for GCC students. Fully bilingual, with KWD prices when configured.",
  keywords: [
    "Gradex",
    "university courses",
    "Kuwait",
    "GCC",
    "computer science",
    "online learning",
    "Arabic courses",
    "programming labs",
  ],
  locale: "en_US",
} as const;

export type SiteConfig = typeof siteConfig;
