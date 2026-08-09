import { LegalPolicyPage } from "@/components/legal/legal-policy-page";
import { legalMetadata } from "@/lib/legal/metadata";

export const dynamic = "force-dynamic";
export const generateMetadata = () => legalMetadata("en", "terms");

export default function EnglishTermsPage() {
  return <LegalPolicyPage locale="en" kind="terms" />;
}
