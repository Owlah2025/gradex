import type { Metadata } from "next";
import {
  canonicalPolicyURL,
  type LegalLocale,
  type LegalPolicyKind,
} from "./policy";
import { legalRuntime } from "./runtime";

export function legalMetadata(locale: LegalLocale, kind: LegalPolicyKind): Metadata {
  const { publicOrigin } = legalRuntime();
  const title = locale === "ar"
    ? kind === "privacy" ? "سياسة خصوصية Gradex" : "شروط استخدام Gradex"
    : kind === "privacy" ? "Gradex Privacy Policy" : "Gradex Terms of Use";
  return {
    title,
    alternates: {
      canonical: canonicalPolicyURL(publicOrigin, locale, kind),
      languages: {
        ar: canonicalPolicyURL(publicOrigin, "ar", kind),
        en: canonicalPolicyURL(publicOrigin, "en", kind),
      },
    },
  };
}
