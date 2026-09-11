"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { Layers3 } from "lucide-react";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Container } from "@/components/layout/container";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { LoadingState } from "@/components/common/loading-state";
import { EmptyState } from "@/components/common/empty-state";
import { PriceDisplay } from "./price-display";
import { ThumbnailImage } from "./thumbnail-image";
import { getPublicBundle, type PublicBundle } from "@/lib/api/public-catalog";
import {
  createStudentBundlePurchaseRequest,
  listStudentPurchaseRequests,
  type PurchaseRequest,
} from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";
import { useLocale } from "@/lib/i18n/locale-provider";

type State = { kind: "loading" } | { kind: "ready"; bundle: PublicBundle } | { kind: "missing" } | { kind: "failed" };
type PurchaseHistoryState =
  | { kind: "loading" }
  | { kind: "ready"; requests: PurchaseRequest[] }
  | { kind: "anonymous" }
  | { kind: "failed" };

export function BundleDetail({ idOrSlug, routeLocale }: { idOrSlug: string; routeLocale: "ar" | "en" }) {
  const { locale, t } = useLocale();
  const pathname = usePathname();
  const [state, setState] = useState<State>({ kind: "loading" });
  const [history, setHistory] = useState<PurchaseHistoryState>({ kind: "loading" });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inFlight = useRef(false);

  const loadHistory = useCallback(async () => {
    setHistory({ kind: "loading" });
    try {
      const result = await listStudentPurchaseRequests(routeLocale);
      setHistory({ kind: "ready", requests: result?.purchase_requests ?? [] });
    } catch (cause: unknown) {
      setHistory(
        cause instanceof ProblemError && cause.problem.status === 401
          ? { kind: "anonymous" }
          : { kind: "failed" },
      );
    }
  }, [routeLocale]);

  useEffect(() => {
    let active = true;
    getPublicBundle(idOrSlug, routeLocale)
      .then((bundle) => { if (active) setState({ kind: "ready", bundle }); })
      .catch((cause: unknown) => {
        if (!active) return;
        setState(cause instanceof ProblemError && cause.problem.status === 404 ? { kind: "missing" } : { kind: "failed" });
      });
    return () => { active = false; };
  }, [idOrSlug, routeLocale]);

  useEffect(() => { void loadHistory(); }, [loadHistory]);

  async function requestPurchase(bundle: PublicBundle) {
    if (inFlight.current || history.kind !== "ready") return;
    inFlight.current = true;
    setSubmitting(true);
    setError(null);
    try {
      const result = await createStudentBundlePurchaseRequest(bundle.id, routeLocale);
      window.location.assign(result.whatsapp_url);
    } catch {
      setError(t.bundles.failed);
      setSubmitting(false);
      inFlight.current = false;
    }
  }

  const bundle = state.kind === "ready" ? state.bundle : null;
  const existing = bundle
    ? history.kind === "ready"
      ? history.requests.find((request) => request.target_kind === "BUNDLE" && request.bundle_id === bundle.id)
      : undefined
    : undefined;

  return (
    <>
      <Navbar />
      <main id="main" tabIndex={-1} className="py-8 outline-none sm:py-10">
        <Container>
          <Link href={`/${locale}/catalog`} className="text-sm font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
            {t.bundles.detailBack}
          </Link>
          {state.kind === "loading" ? <LoadingState className="mt-8" label={t.bundles.loading} /> : null}
          {state.kind === "failed" ? <div className="mt-8"><Alert tone="error" title={t.bundles.loadFailed} /></div> : null}
          {state.kind === "missing" ? <div className="mt-8"><EmptyState icon={<Layers3 aria-hidden />} title={t.bundles.loadFailed} /></div> : null}
          {bundle ? (
            <article className="mt-7 lg:grid lg:grid-cols-[minmax(0,1fr)_22rem] lg:gap-10">
              <div>
                <p className="text-sm font-semibold text-primary">{t.bundles.courseCount.replace("{count}", String(bundle.course_count))}</p>
                <h1 className="mt-2 text-balance font-display text-4xl font-bold tracking-[-0.03em] text-foreground"><bdi>{bundle.title}</bdi></h1>
                <p className="mt-5 max-w-2xl whitespace-pre-wrap text-pretty leading-7 text-muted-foreground"><bdi>{bundle.description}</bdi></p>
                <section className="mt-10" aria-labelledby="bundle-courses-title">
                  <h2 id="bundle-courses-title" className="font-display text-2xl font-bold">{t.bundles.included}</h2>
                  <ol className="mt-5 space-y-3">
                    {bundle.members.map((member) => (
                      <li key={member.course_id} className="flex gap-4 rounded-lg border border-border p-4">
                        <div className="h-20 w-28 shrink-0 overflow-hidden rounded-md bg-muted">
                          {member.thumbnail?.card_url ? <ThumbnailImage src={member.thumbnail.card_url} className="h-full w-full object-cover" /> : <div className="flex h-full items-center justify-center"><Layers3 className="size-6 text-muted-foreground" /></div>}
                        </div>
                        <div className="min-w-0">
                          <Link href={`/${locale}/catalog/${encodeURIComponent(member.slug || member.course_id)}`} className="font-display font-bold text-foreground underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"><bdi>{member.title}</bdi></Link>
                          <p className="mt-1 text-sm text-muted-foreground"><bdi>{member.instructor_display_name}</bdi></p>
                          {member.subject ? <p className="mt-2 text-xs text-muted-foreground"><bdi>{member.subject.label}</bdi>{member.subject.code ? ` · ${member.subject.code}` : ""}</p> : null}
                        </div>
                      </li>
                    ))}
                  </ol>
                </section>
              </div>
              <aside className="mt-8 rounded-xl bg-gx-blue-50 p-6 dark:bg-card lg:sticky lg:top-24 lg:mt-0" aria-labelledby="bundle-purchase-title">
                <h2 id="bundle-purchase-title" className="font-display text-xl font-bold">{t.bundles.paymentTitle}</h2>
                <PriceDisplay price={bundle.price} locale={routeLocale} className="mt-4" />
                <p className="mt-4 text-sm leading-6 text-muted-foreground">{t.bundles.paymentBody}</p>
                {error ? <div className="mt-4"><Alert tone="error" title={error} /></div> : null}
                {history.kind === "loading" ? (
                  <p
                    className="mt-4 text-sm text-muted-foreground"
                    aria-live="polite"
                    data-testid="bundle-purchase-history-loading"
                  >
                    {t.bundles.historyLoading}
                  </p>
                ) : null}
                {history.kind === "failed" ? (
                  <div className="mt-4">
                    <Alert tone="error" title={t.bundles.historyFailed}>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="mt-2"
                        onClick={() => void loadHistory()}
                        data-testid="bundle-purchase-history-retry"
                      >
                        {t.bundles.retryHistory}
                      </Button>
                    </Alert>
                  </div>
                ) : null}
                {existing?.state === "WAITING_PAYMENT" ? <div className="mt-4"><Alert tone="info" title={t.bundles.pending} /></div> : null}
                {existing?.state === "ACCESS_GRANTED" ? <div className="mt-4"><Alert tone="success" title={t.bundles.granted} /></div> : null}
                {history.kind === "anonymous" ? (
                  <Button asChild className="mt-5 w-full"><Link href={`/login?returnTo=${encodeURIComponent(pathname ?? `/${locale}/catalog`)}`}>{t.access.purchase.signIn}</Link></Button>
                ) : existing ? null : (
                  <Button type="button" className="mt-5 w-full" disabled={submitting || history.kind !== "ready"} onClick={() => void requestPurchase(bundle)} data-testid="bundle-purchase-request">
                    {submitting ? t.access.purchase.submitting : t.bundles.request}
                  </Button>
                )}
              </aside>
            </article>
          ) : null}
        </Container>
      </main>
      <Footer />
    </>
  );
}
