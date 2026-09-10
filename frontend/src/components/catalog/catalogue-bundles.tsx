"use client";

import { useEffect, useState } from "react";
import { BundleCard } from "./bundle-card";
import { getPublicBundles, type PublicBundle } from "@/lib/api/public-catalog";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";

export function CatalogueBundles() {
  const { locale, t } = useLocale();
  const [bundles, setBundles] = useState<PublicBundle[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    setBundles(null);
    setFailed(false);
    getPublicBundles(locale)
      .then((result) => { if (active) setBundles(result.items); })
      .catch(() => { if (active) setFailed(true); });
    return () => { active = false; };
  }, [locale]);
  return (
    <section className="mt-16 border-t border-border pt-10" aria-labelledby="catalogue-bundles-title">
      <h2 id="catalogue-bundles-title" className="font-display text-2xl font-bold text-foreground">
        {t.bundles.title}
      </h2>
      <p className="mt-2 max-w-2xl text-muted-foreground">{t.bundles.subtitle}</p>
      {bundles === null && !failed ? <p className="mt-6" aria-live="polite">{t.bundles.loading}</p> : null}
      {failed ? <div className="mt-6"><Alert tone="error" title={t.bundles.failed} /></div> : null}
      {bundles?.length === 0 ? <p className="mt-6 text-muted-foreground">{t.bundles.emptyBody}</p> : null}
      {bundles && bundles.length > 0 ? (
        <ul className="mt-7 grid gap-5 md:grid-cols-2 xl:grid-cols-3" data-testid="catalogue-bundles-list">
          {bundles.map((bundle) => <li key={bundle.id}><BundleCard bundle={bundle} locale={locale} /></li>)}
        </ul>
      ) : null}
    </section>
  );
}

