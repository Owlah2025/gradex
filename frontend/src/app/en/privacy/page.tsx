import { LegalPolicyPage } from "@/components/legal/legal-policy-page";
import { legalMetadata } from "@/lib/legal/metadata";

export const dynamic = "force-dynamic";
export const generateMetadata = () => legalMetadata("en", "privacy");

export default function EnglishPrivacyPage() {
  return <LegalPolicyPage locale="en" kind="privacy" />;
}
