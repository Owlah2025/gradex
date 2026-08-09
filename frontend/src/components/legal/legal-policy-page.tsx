import Link from "next/link";
import {
  approvedPolicyDocument,
  approvedPolicyMetadata,
  canonicalPolicyPath,
  interpolatePolicyDocument,
  type LegalIdentity,
  type LegalLocale,
  type LegalPolicyKind,
} from "@/lib/legal/policy";
import { legalRuntime } from "@/lib/legal/runtime";

type Props = { locale: LegalLocale; kind: LegalPolicyKind };
type Labels = ReturnType<typeof legalLabels>;

function inlineMarkdown(text: string) {
  return text.split(/(\*\*[^*]+\*\*)/g).filter(Boolean).map((part, index) =>
    part.startsWith("**") && part.endsWith("**") ? (
      <strong key={index}>{part.slice(2, -2)}</strong>
    ) : <span key={index}>{part}</span>,
  );
}

function PolicyBlock({ block }: { block: string }) {
  const value = block.trim();
  if (value === "---") return <hr className="my-8 border-border" />;
  if (value.startsWith("### ")) return <h3 className="mt-9 text-xl">{inlineMarkdown(value.slice(4))}</h3>;
  if (value.startsWith("## ")) return <h2 className="mt-10 text-2xl">{inlineMarkdown(value.slice(3))}</h2>;
  if (value.startsWith("# ")) return <h1 className="text-3xl sm:text-4xl">{inlineMarkdown(value.slice(2))}</h1>;
  const lines = value.split("\n").map((line) => line.trimEnd());
  return (
    <p className="leading-8 text-foreground/90">
      {lines.map((line, index) => <span key={index}>{inlineMarkdown(line)}{index < lines.length - 1 ? <br /> : null}</span>)}
    </p>
  );
}

function policyBlocks(markdown: string) {
  return markdown.split(/\n\s*\n/g).filter(Boolean).map((block, index) => (
    <PolicyBlock block={block} key={index} />
  ));
}

function legalLabels(locale: LegalLocale) {
  if (locale === "ar") return {
    language: "English", navigation: "التنقل بين اللغات",
    staging: "بيانات الهوية القانونية المعروضة مخصصة لبيئة اختبار غير عامة فقط.",
    operator: "بيانات المشغل والتواصل", registration: "رقم التسجيل/الترخيص",
    address: "العنوان المسجل", privacy: "الخصوصية", support: "الدعم", security: "الأمن",
  };
  return {
    language: "العربية", navigation: "Language navigation",
    staging: "The displayed legal identity is authorized for controlled non-public staging only.",
    operator: "Operator and contact details", registration: "Registration/license number",
    address: "Registered address", privacy: "Privacy", support: "Support", security: "Security",
  };
}

function LanguageNavigation({ locale, kind, labels }: Props & { labels: Labels }) {
  const otherLocale: LegalLocale = locale === "ar" ? "en" : "ar";
  return (
    <nav aria-label={labels.navigation} className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b pb-5">
      <Link href="/" className="font-display text-lg font-bold text-primary">Gradex</Link>
      <Link className="rounded-md border px-4 py-2 text-sm font-semibold hover:bg-muted" href={canonicalPolicyPath(otherLocale, kind)} hrefLang={otherLocale}>
        {labels.language}
      </Link>
    </nav>
  );
}

function ContactRow({ label, children }: { label: string; children: React.ReactNode }) {
  return <><dt className="font-semibold">{label}</dt><dd>{children}</dd></>;
}

function LegalContactSection({ identity, labels }: { identity: LegalIdentity; labels: Labels }) {
  return (
    <section aria-labelledby="legal-contact-heading" className="mt-8 rounded-xl border bg-card p-5 sm:p-7">
      <h2 id="legal-contact-heading" className="text-xl">{labels.operator}</h2>
      <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-[12rem_1fr]">
        <ContactRow label={labels.registration}>{identity.registrationNumber}</ContactRow>
        <ContactRow label={labels.address}>{identity.registeredAddress}</ContactRow>
        <ContactRow label={labels.privacy}><a className="text-primary underline" href={`mailto:${identity.privacyEmail}`}>{identity.privacyEmail}</a></ContactRow>
        <ContactRow label={labels.support}><a className="text-primary underline" href={`mailto:${identity.supportEmail}`}>{identity.supportEmail}</a></ContactRow>
        <ContactRow label={labels.security}><a className="text-primary underline" href={`mailto:${identity.securityEmail}`}>{identity.securityEmail}</a></ContactRow>
      </dl>
      <p className="mt-5 text-xs text-muted-foreground">
        {approvedPolicyMetadata.id} · {approvedPolicyMetadata.version} · {approvedPolicyMetadata.effectiveDate}
      </p>
    </section>
  );
}

export function LegalPolicyPage({ locale, kind }: Props) {
  const runtime = legalRuntime();
  const labels = legalLabels(locale);
  const document = interpolatePolicyDocument(
    approvedPolicyDocument(locale, kind), runtime.identity, kind,
  );
  return (
    <main id="main" tabIndex={-1} lang={locale} dir={locale === "ar" ? "rtl" : "ltr"} className="min-h-screen bg-background">
      <div className="mx-auto max-w-4xl px-5 py-8 sm:px-8 sm:py-12">
        <LanguageNavigation locale={locale} kind={kind} labels={labels} />
        {runtime.controlledStaging ? <p role="status" className="mb-7 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm font-semibold text-amber-950">{labels.staging}</p> : null}
        <article className="space-y-5 rounded-xl border bg-card p-5 shadow-sm sm:p-9">{policyBlocks(document)}</article>
        <LegalContactSection identity={runtime.identity} labels={labels} />
      </div>
    </main>
  );
}
