import { LegalPolicyPage } from "@/components/legal/legal-policy-page";
import { legalMetadata } from "@/lib/legal/metadata";

export const dynamic = "force-dynamic";
export const generateMetadata = () => legalMetadata("ar", "privacy");

export default function ArabicPrivacyPage() {
  return <LegalPolicyPage locale="ar" kind="privacy" />;
}
