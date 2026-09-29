"use client";

import Link from "next/link";
import { ArrowUpRight, ClipboardCheck, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { getAdminInbox, getAdminMetricsOverview, metricNumber, type AdminInbox, type AdminMetricsOverview } from "@/lib/api/admin-metrics";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

const headlineKeys = [
  "students.total",
  "courses.pending_review",
  "learning_activity.7d",
  "enrollments.new_30d",
] as const;

export function AdminOperatorHome() {
  const { locale, t } = useLocale();
  const copy = t.adminHome;
  const [overview, setOverview] = useState<AdminMetricsOverview | null>(null);
  const [inbox, setInbox] = useState<AdminInbox | null>(null);
  const [overviewError, setOverviewError] = useState<string | null>(null);
  const [inboxError, setInboxError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  const load = useCallback(async () => {
    setOverview(null);
    setInbox(null);
    setOverviewError(null);
    setInboxError(null);
    const [overviewResult, inboxResult] = await Promise.allSettled([
      getAdminMetricsOverview(locale),
      getAdminInbox(locale),
    ]);
    if (overviewResult.status === "fulfilled") {
      setOverview(overviewResult.value);
    } else {
      setOverviewError(describeApiError(overviewResult.reason, locale));
    }
    if (inboxResult.status === "fulfilled") {
      setInbox(inboxResult.value);
    } else {
      setInboxError(describeApiError(inboxResult.reason, locale));
    }
  }, [locale]);

  useEffect(() => {
    void load();
  }, [load, attempt]);

  return (
    <WorkspacePage>
      <WorkspacePageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <>
            <Button asChild variant="outline">
              <Link href={`/${locale}/admin/analytics`}>{t.nav.adminAnalytics}</Link>
            </Button>
            <Button type="button" variant="outline" onClick={() => setAttempt((value) => value + 1)}>
              <RefreshCw aria-hidden />
              {copy.refresh}
            </Button>
          </>
        }
      />

      <WorkspaceSection title={copy.needsAttention} testID="admin-operator-inbox">
        {inboxError ? (
          <ErrorState
            title={copy.loadFailed}
            detail={inboxError}
            retryLabel={copy.retry}
            onRetry={() => setAttempt((value) => value + 1)}
          />
        ) : inbox === null ? (
          <LoadingState label={copy.loading} />
        ) : inbox.sections.length === 0 || inbox.sections.every((section) => section.count === 0) ? (
          <EmptyState
            density="compact"
            icon={<ClipboardCheck aria-hidden />}
            title={copy.noItems}
            description={copy.noItemsDescription}
          />
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {inbox.sections.map((section) => (
              <InboxCard key={section.key} section={section} locale={locale} copy={copy} />
            ))}
          </div>
        )}
      </WorkspaceSection>

      <WorkspaceSection title={copy.headlineMetrics} testID="admin-operator-headlines">
        {overviewError ? (
          <ErrorState
            title={copy.loadFailed}
            detail={overviewError}
            retryLabel={copy.retry}
            onRetry={() => setAttempt((value) => value + 1)}
          />
        ) : overview === null ? (
          <LoadingState label={copy.loading} />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {headlineKeys.map((key) => (
              <Card key={key} className="border-t-4 border-t-primary">
                <CardHeader className="pb-2">
                  <p className="text-sm text-muted-foreground">{copy.metricLabels[key]}</p>
                </CardHeader>
                <CardContent>
                  <p className="font-display text-3xl font-bold tabular-nums text-foreground">
                    {metricNumber(overview.metrics, key).toLocaleString(locale)}
                  </p>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </WorkspaceSection>
    </WorkspacePage>
  );
}

function InboxCard({
  section,
  locale,
  copy,
}: {
  section: AdminInbox["sections"][number];
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminHome"];
}) {
  const label = copy.inboxLabels[section.key as keyof typeof copy.inboxLabels] ?? section.key;
  return (
    <Card className="overflow-hidden">
      <CardHeader className="flex-row items-center justify-between gap-3 border-b border-border bg-muted/35">
        <CardTitle className="text-base">{label}</CardTitle>
        <Badge variant={section.count > 0 ? "accent" : "neutral"}>{section.count.toLocaleString(locale)}</Badge>
      </CardHeader>
      <CardContent className="p-0">
        {section.items.length === 0 ? (
          <p className="px-6 py-5 text-sm text-muted-foreground">{copy.noItems}</p>
        ) : (
          <ul className="divide-y divide-border">
            {section.items.map((item) => (
              <li key={`${item.kind}-${item.target_id}`}>
                <Link
                  href={`/${locale}${item.route}`}
                  className="group flex items-center justify-between gap-4 px-6 py-4 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
                >
                  <span className="min-w-0">
                    <span className="block truncate font-semibold text-foreground">{item.label}</span>
                    <span className="mt-1 block text-xs text-muted-foreground">
                      {formatAge(item.age_seconds, locale)}
                    </span>
                  </span>
                  <ArrowUpRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:-translate-y-0.5 group-hover:translate-x-0.5" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function formatAge(seconds: number, locale: "ar" | "en"): string {
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (seconds < 60) return formatter.format(-Math.max(1, Math.floor(seconds)), "second");
  if (seconds < 3600) return formatter.format(-Math.floor(seconds / 60), "minute");
  if (seconds < 86_400) return formatter.format(-Math.floor(seconds / 3600), "hour");
  return formatter.format(-Math.floor(seconds / 86_400), "day");
}
