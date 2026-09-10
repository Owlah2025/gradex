"use client";

import { useEffect, useState } from "react";
import { Layers3 } from "lucide-react";
import { Section } from "@/components/layout/section";
import { SectionHeading } from "@/components/ui/typography";
import { Alert } from "@/components/ui/alert";
import { EmptyState } from "@/components/common/empty-state";
import { BundleCard } from "@/components/catalog/bundle-card";
import { getPublicBundles, type PublicBundle } from "@/lib/api/public-catalog";
import { useLocale } from "@/lib/i18n/locale-provider";

type BundleState =
  | { kind: "loading" }
  | { kind: "ready"; bundles: PublicBundle[] }
  | { kind: "failed" };

export function FeaturedBundles() {
  const { locale, t } = useLocale();
  const [state, setState] = useState<BundleState>({ kind: "loading" });

  useEffect(() => {
    let active = true;
    setState({ kind: "loading" });
    getPublicBundles(locale)
      .then((result) => {
        if (active) setState({ kind: "ready", bundles: result.items.slice(0, 6) });
      })
      .catch(() => {
        if (active) setState({ kind: "failed" });
      });
    return () => {
      active = false;
    };
  }, [locale]);

  return (
    <Section aria-labelledby="bundles-title" tone="muted">
      <div className="max-w-2xl">
        <SectionHeading id="bundles-title">{t.bundles.title}</SectionHeading>
        <p className="mt-2 text-pretty text-muted-foreground">{t.bundles.subtitle}</p>
      </div>
      {state.kind === "loading" ? (
        <p className="mt-6" aria-live="polite" data-testid="featured-bundles-loading">
          {t.bundles.loading}
        </p>
      ) : null}
      {state.kind === "failed" ? (
        <div className="mt-6" data-testid="featured-bundles-error">
          <Alert tone="error" title={t.bundles.failed} />
        </div>
      ) : null}
      {state.kind === "ready" && state.bundles.length === 0 ? (
        <div className="mt-6" data-testid="featured-bundles-empty">
          <EmptyState icon={<Layers3 aria-hidden />} title={t.bundles.emptyTitle} description={t.bundles.emptyBody} />
        </div>
      ) : null}
      {state.kind === "ready" && state.bundles.length > 0 ? (
        <ul className="mt-7 grid gap-5 md:grid-cols-2 xl:grid-cols-3" data-testid="featured-bundles-list">
          {state.bundles.map((bundle) => (
            <li key={bundle.id}><BundleCard bundle={bundle} locale={locale} /></li>
          ))}
        </ul>
      ) : null}
    </Section>
  );
}

