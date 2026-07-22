export const siteConfig = {
  name: "Gradex",
  wordmark: { lead: "Grade", accent: "x" },
  url: "https://gradex.com",
  ogImage: "/og.png",
  description:
    "University courses for GCC students. Real lectures, notes, and labs — with instructors who stay with you after you enroll. Fully bilingual, fair KWD pricing.",
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
  links: {
    discord: "#",
    x: "#",
    instagram: "#",
  },
} as const;

export type SiteConfig = typeof siteConfig;
