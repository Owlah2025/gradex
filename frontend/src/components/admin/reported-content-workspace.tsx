"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  getAdminReport,
  getAdminReports,
  resolveAdminReport,
  type AdminReport,
  type AdminReportAction,
} from "@/lib/api/admin-reports";
import { ProblemError } from "@/lib/api/problem";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

const PAGE_SIZE = 20;

export function ReportedContentWorkspace() {
  const { locale, t } = useLocale();
  const isAr = locale === "ar";
  const labels = t.adminReports;
  const [reports, setReports] = useState<AdminReport[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<AdminReport | null>(null);
  const [reason, setReason] = useState("");
  const [queueLoading, setQueueLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [queueError, setQueueError] = useState(false);
  const [detailError, setDetailError] = useState(false);
  const [actionError, setActionError] = useState<"conflict" | "failed" | "denied" | null>(null);
  const [busy, setBusy] = useState(false);

  const loadQueue = useCallback(async () => {
    setQueueLoading(true);
    setQueueError(false);
    try {
      const response = await getAdminReports(locale, 1, PAGE_SIZE);
      setReports(response.items);
    } catch {
      setQueueError(true);
    } finally {
      setQueueLoading(false);
    }
  }, [locale]);

  useEffect(() => {
    void loadQueue();
  }, [loadQueue]);

  const openDetail = async (reportID: string) => {
    setSelectedID(reportID);
    setDetail(null);
    setDetailError(false);
    setActionError(null);
    setDetailLoading(true);
    try {
      setDetail(await getAdminReport(reportID, locale));
    } catch {
      setDetailError(true);
    } finally {
      setDetailLoading(false);
    }
  };

  const resolve = async (action: AdminReportAction) => {
    if (!detail || !reason.trim()) return;
    const csrf = currentCSRFToken();
    if (!csrf) {
      setActionError("denied");
      return;
    }
    setBusy(true);
    setActionError(null);
    try {
      const resolved = await resolveAdminReport({
        reportID: detail.id,
        action,
        reason: reason.trim(),
        locale,
        csrf,
      });
      setDetail(resolved);
      setReason("");
      await loadQueue();
    } catch (error) {
      if (error instanceof ProblemError && error.problem.status === 409) {
        setActionError("conflict");
        try {
          setDetail(await getAdminReport(detail.id, locale));
        } catch {
          setDetailError(true);
        }
      } else if (error instanceof ProblemError && error.problem.status === 403) {
        setActionError("denied");
      } else {
        setActionError("failed");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <section
      dir={isAr ? "rtl" : "ltr"}
      className="mx-auto grid max-w-container gap-6 p-4 sm:p-6 lg:grid-cols-[minmax(18rem,0.8fr)_minmax(0,1.5fr)]"
      data-testid="reported-content-workspace"
    >
      <header className="lg:col-span-2">
        <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">AD-14</p>
        <h1 className="mt-2 text-2xl font-semibold text-foreground">{labels.title}</h1>
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{labels.intro}</p>
      </header>

      <section aria-labelledby="reported-content-queue-heading" className="space-y-3">
        <div className="flex items-baseline justify-between gap-3">
          <h2 id="reported-content-queue-heading" className="text-sm font-semibold text-foreground">
            {labels.queueHeading}
          </h2>
          {!queueLoading && !queueError ? <span className="text-xs text-muted-foreground">{reports.length}</span> : null}
        </div>

        {queueLoading ? (
          <ul aria-label={labels.loading} aria-busy="true" data-testid="reported-content-loading" className="space-y-2">
            {[1, 2, 3].map((item) => (
              <li key={item} className="h-16 animate-pulse rounded-lg bg-muted/60" />
            ))}
          </ul>
        ) : queueError ? (
          <div role="alert" className="space-y-3 rounded-lg border border-destructive/40 p-4" data-testid="reported-content-queue-error">
            <p className="text-sm text-foreground">{labels.loadFailed}</p>
            <button type="button" onClick={() => void loadQueue()} className="rounded-md border border-border px-3 py-2 text-sm font-semibold">
              {labels.retry}
            </button>
          </div>
        ) : reports.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border p-5" data-testid="reported-content-empty">
            <h3 className="text-sm font-semibold text-foreground">{labels.emptyTitle}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{labels.emptyBody}</p>
          </div>
        ) : (
          <ol className="space-y-2" data-testid="reported-content-queue">
            {reports.map((report) => (
              <li key={report.id} data-testid="reported-content-row">
                <button
                  type="button"
                  onClick={() => void openDetail(report.id)}
                  aria-current={selectedID === report.id ? "true" : undefined}
                  className="w-full rounded-lg border border-border p-4 text-start transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  <span className="flex items-center justify-between gap-3">
                    <span className="font-semibold text-foreground">{targetLabel(report, isAr, labels)}</span>
                    <span className="text-xs text-muted-foreground">{reasonLabel(report, labels)}</span>
                  </span>
                  <span className="mt-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                    <span>{targetTypeLabel(report, labels)}</span>
                    <span>{formatDate(report.created_at, locale)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ol>
        )}
      </section>

      <section aria-labelledby="reported-content-detail-heading" className="min-w-0">
        {detailLoading ? (
          <div role="status" aria-busy="true" className="space-y-3 rounded-lg border border-border p-5" data-testid="reported-content-detail-loading">
            <div className="h-5 w-2/3 animate-pulse rounded bg-muted/60" />
            <div className="h-20 animate-pulse rounded bg-muted/60" />
          </div>
        ) : detailError ? (
          <div role="alert" className="rounded-lg border border-destructive/40 p-5" data-testid="reported-content-detail-error">
            <p className="text-sm text-foreground">{labels.detailFailed}</p>
          </div>
        ) : detail ? (
          <ReportDetail
            report={detail}
            locale={locale}
            labels={labels}
            reason={reason}
            busy={busy}
            actionError={actionError}
            onReasonChange={setReason}
            onResolve={resolve}
          />
        ) : (
          <div className="hidden rounded-lg border border-border p-5 lg:block" data-testid="reported-content-detail-placeholder">
            <h2 id="reported-content-detail-heading" className="text-sm font-semibold text-foreground">{labels.inspect}</h2>
          </div>
        )}
      </section>
    </section>
  );
}

function ReportDetail({
  report,
  locale,
  labels,
  reason,
  busy,
  actionError,
  onReasonChange,
  onResolve,
}: {
  report: AdminReport;
  locale: "ar" | "en";
  labels: ReturnType<typeof useLocale>["t"]["adminReports"];
  reason: string;
  busy: boolean;
  actionError: "conflict" | "failed" | "denied" | null;
  onReasonChange: (value: string) => void;
  onResolve: (action: AdminReportAction) => void;
}) {
  const isAr = locale === "ar";
  const open = report.status === "OPEN";
  const canDelist = open && report.target.available && report.target.course_lifecycle === "PUBLISHED";

  return (
    <article className="space-y-5 rounded-lg border border-border p-5" data-testid="reported-content-detail">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">{targetTypeLabel(report, labels)}</p>
          <h2 id="reported-content-detail-heading" className="mt-1 text-xl font-semibold text-foreground">{targetLabel(report, isAr, labels)}</h2>
        </div>
        <span className="rounded-full border border-border px-3 py-1 text-xs font-semibold" data-status={report.status}>
          {statusLabel(report, labels)}
        </span>
      </header>

      <dl className="grid gap-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-xs text-muted-foreground">{labels.reasonLabel}</dt>
          <dd className="mt-1 font-semibold text-foreground">{reasonLabel(report, labels)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">{labels.reporter}</dt>
          <dd className="mt-1 text-foreground">{report.reporter_display_name || labels.unknownState}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">{labels.submittedAt}</dt>
          <dd className="mt-1 text-foreground">{formatDate(report.created_at, locale)}</dd>
        </div>
        <div className="sm:col-span-2">
          <dt className="text-xs text-muted-foreground">{labels.targetLabel}</dt>
          <dd className="mt-1 text-foreground">{targetContext(report, isAr, labels)}</dd>
        </div>
      </dl>

      <div className="rounded-md bg-muted/45 p-4">
        <h3 className="text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground">{labels.explanationLabel}</h3>
        <p className="mt-2 whitespace-pre-wrap text-sm text-foreground">{report.explanation || labels.noExplanation}</p>
      </div>

      {report.status === "RESOLVED" ? (
        <div className="space-y-2 border-s border-primary ps-4" data-testid="reported-content-resolved">
          {actionError ? (
            <p role="alert" className="text-sm font-medium text-destructive" data-testid="reported-content-action-error">
              {actionError === "conflict" ? labels.conflict : actionError === "denied" ? labels.denied : labels.actionFailed}
            </p>
          ) : null}
          <p className="text-sm font-semibold text-foreground">{labels.resolvedMessage}</p>
          <p className="text-sm text-muted-foreground">{resolutionLabel(report, labels)}</p>
          {report.resolution_reason ? <p className="text-sm text-foreground">{report.resolution_reason}</p> : null}
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(event) => event.preventDefault()}
          data-testid="reported-content-resolution-form"
        >
          <div className="space-y-2">
            <label htmlFor="reported-content-resolution-reason" className="block text-sm font-semibold text-foreground">
              {labels.resolutionReason}
            </label>
            <textarea
              id="reported-content-resolution-reason"
              value={reason}
              onChange={(event) => onReasonChange(event.target.value)}
              disabled={busy}
              required
              rows={3}
              aria-describedby="reported-content-resolution-hint"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            />
            <p id="reported-content-resolution-hint" className="text-xs text-muted-foreground">{labels.resolutionHint}</p>
          </div>

          {actionError ? (
            <p role="alert" className="text-sm font-medium text-destructive" data-testid="reported-content-action-error">
              {actionError === "conflict" ? labels.conflict : actionError === "denied" ? labels.denied : labels.actionFailed}
            </p>
          ) : null}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              disabled={busy || !reason.trim()}
              onClick={() => onResolve("DISMISS")}
              className="rounded-md bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-50"
              data-testid="reported-content-dismiss"
            >
              {busy ? labels.resolving : labels.dismiss}
            </button>
            {canDelist ? (
              <button
                type="button"
                disabled={busy || !reason.trim()}
                onClick={() => onResolve("DELIST")}
                className="rounded-md border border-destructive px-4 py-2 text-sm font-semibold text-destructive disabled:opacity-50"
                data-testid="reported-content-delist"
              >
                {labels.delist}
              </button>
            ) : null}
          </div>
        </form>
      )}

      {report.target.available ? (
        <Link href={`/${locale}/admin/course-lifecycle`} className="inline-flex text-sm font-semibold text-primary underline-offset-4 hover:underline">
          {labels.openLifecycle}
        </Link>
      ) : null}
    </article>
  );
}

function targetLabel(report: AdminReport, isAr: boolean, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  if (!report.target.available) return labels.targetUnavailable;
  const label = isAr ? report.target.target_label_ar : report.target.target_label_en;
  return label || (isAr ? report.target.course_label_ar : report.target.course_label_en) || labels.unknownState;
}

function targetContext(report: AdminReport, isAr: boolean, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  if (!report.target.available) return labels.unavailableBody;
  const course = isAr ? report.target.course_label_ar : report.target.course_label_en;
  const state = report.target.course_lifecycle ? lifecycleLabel(report.target.course_lifecycle, labels) : labels.unknownState;
  const flags = [state];
  if (report.target.access_suspended) flags.push(labels.accessSuspended);
  if (report.target.retired) flags.push(labels.retired);
  return [course || labels.unknownState, ...flags].join(" · ");
}

function reasonLabel(report: AdminReport, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  return labels.reasons[report.reason as keyof typeof labels.reasons] || labels.unknownState;
}

function targetTypeLabel(report: AdminReport, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  return labels.targets[report.target_type as keyof typeof labels.targets] || labels.unknownState;
}

function statusLabel(report: AdminReport, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  return report.status === "OPEN" ? labels.open : labels.resolved;
}

function resolutionLabel(report: AdminReport, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  return labels.actions[report.resolution_action as keyof typeof labels.actions] || labels.resolved;
}

function lifecycleLabel(state: string, labels: ReturnType<typeof useLocale>["t"]["adminReports"]): string {
  return labels.lifecycle[state as keyof typeof labels.lifecycle] || labels.unknownState;
}

function formatDate(value: string, locale: "ar" | "en"): string {
  return new Intl.DateTimeFormat(locale === "ar" ? "ar" : "en", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}
