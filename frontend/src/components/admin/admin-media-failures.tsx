"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  listAdminMediaFailures,
  retryAdminMedia,
  retryAdminMediaEnhancements,
  type AdminMediaFailure,
  type AdminMediaFailureState,
  type AdminMediaFailurePage,
} from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatDateTime } from "@/lib/i18n/format";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Select } from "@/components/ui/select";
import { Table, TableBody, TableCaption, TableCell, TableContainer, TableHead, TableHeaderCell, TableRow } from "@/components/ui/table";
import { WorkspacePage, WorkspacePageHeader, WorkspaceToolbar } from "@/components/layout/workspace-page";

const PAGE_LIMIT = 20;

export function AdminMediaFailures() {
  const { locale, t } = useLocale();
  const copy = t.adminMediaFailures;
  const [result, setResult] = useState<AdminMediaFailurePage | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<AdminMediaFailureState>("");
  const [page, setPage] = useState(1);
  const [pending, setPending] = useState<AdminMediaFailure | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ tone: "success" | "error"; message: string } | null>(null);

  const load = useCallback(async () => {
    setState("loading");
    setError(null);
    try {
      setResult(await listAdminMediaFailures(locale, page, PAGE_LIMIT, filter));
      setState("ready");
    } catch (cause) {
      setError(describeApiError(cause, locale));
      setState("failed");
    }
  }, [filter, locale, page]);

  useEffect(() => { void load(); }, [load]);

  const visible = useMemo(() => result?.items ?? [], [result]);
  const runRetry = async () => {
    if (!pending || busy || !pending.retry_action) return;
    setBusy(true);
    setNotice(null);
    try {
      if (pending.retry_action === "retry-enhancements") {
        await retryAdminMediaEnhancements(pending.asset_version_id, locale);
      } else {
        await retryAdminMedia(pending.asset_version_id, locale);
      }
      setNotice({ tone: "success", message: copy.retryQueued });
      setPending(null);
      await load();
    } catch (cause) {
      setNotice({ tone: "error", message: describeApiError(cause, locale) });
    } finally {
      setBusy(false);
    }
  };

  return <WorkspacePage testID="admin-media-failures-page">
    <WorkspacePageHeader title={copy.title} description={copy.description} actions={<Select aria-label={copy.filterLabel} value={filter} onChange={(event) => { setFilter(event.target.value as AdminMediaFailureState); setPage(1); }}><option value="">{copy.allStates}</option><option value="failed">{copy.failed}</option><option value="stuck">{copy.stuck}</option></Select>} />
    <WorkspaceToolbar><p className="max-w-3xl text-sm text-muted-foreground">{copy.safetyNote}</p></WorkspaceToolbar>
    {notice ? <div className="mt-4"><Alert tone={notice.tone} title={notice.message} /></div> : null}
    {state === "failed" ? <ErrorState className="mt-6" title={copy.loadFailed} detail={error} retryLabel={copy.retry} onRetry={() => void load()} /> : null}
    {state === "loading" ? <LoadingState className="mt-6" label={copy.loading} /> : null}
    {state === "ready" && visible.length === 0 ? <div className="mt-6"><EmptyState title={copy.empty} description={copy.emptyDescription} /></div> : null}
    {state === "ready" && visible.length > 0 ? <>
      <div className="mt-6 hidden md:block"><FailureTable items={visible} copy={copy} locale={locale} onRetry={setPending} /></div>
      <ul className="mt-6 space-y-3 md:hidden" aria-label={copy.title}>{visible.map((item) => <li key={item.asset_version_id}><FailureCard item={item} copy={copy} locale={locale} onRetry={setPending} /></li>)}</ul>
    </> : null}
    {state === "ready" && result ? <nav className="mt-6 flex items-center justify-between gap-3" aria-label={copy.pagination}><Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>{copy.previous}</Button><span className="text-sm font-semibold text-muted-foreground">{copy.page} {page}</span><Button type="button" variant="outline" size="sm" disabled={!result.has_more} onClick={() => setPage((value) => value + 1)}>{copy.next}</Button></nav> : null}
    <ConfirmDialog open={pending !== null} onOpenChange={(open) => { if (!busy && !open) { setPending(null); setNotice(null); } }} title={copy.confirmTitle} body={pending?.retry_action === "retry-enhancements" ? copy.confirmEnhancements : copy.confirmRetry} confirmLabel={copy.confirm} cancelLabel={copy.cancel} busy={busy} error={notice?.tone === "error" ? notice.message : undefined} onConfirm={() => void runRetry()} testID="admin-media-failure-confirm" />
  </WorkspacePage>;
}

function FailureTable({ items, copy, locale, onRetry }: { items: AdminMediaFailure[]; copy: ReturnType<typeof useLocale>["t"]["adminMediaFailures"]; locale: "ar" | "en"; onRetry: (item: AdminMediaFailure) => void }) {
  return <TableContainer><Table><TableCaption>{copy.caption}</TableCaption><TableHead><TableRow><TableHeaderCell scope="col">{copy.course}</TableHeaderCell><TableHeaderCell scope="col">{copy.lesson}</TableHeaderCell><TableHeaderCell scope="col">{copy.owner}</TableHeaderCell><TableHeaderCell scope="col">{copy.stateLabel}</TableHeaderCell><TableHeaderCell scope="col">{copy.failure}</TableHeaderCell><TableHeaderCell scope="col">{copy.updated}</TableHeaderCell><TableHeaderCell scope="col">{copy.action}</TableHeaderCell></TableRow></TableHead><TableBody>{items.map((item) => <FailureRow key={item.asset_version_id} item={item} copy={copy} locale={locale} onRetry={onRetry} />)}</TableBody></Table></TableContainer>;
}

function FailureRow({ item, copy, locale, onRetry }: { item: AdminMediaFailure; copy: ReturnType<typeof useLocale>["t"]["adminMediaFailures"]; locale: "ar" | "en"; onRetry: (item: AdminMediaFailure) => void }) {
  const kinds = copy.kinds as Record<string, string>;
  const failures = copy.failureCategories as Record<string, string>;
  return <TableRow><TableCell><span className="font-semibold">{item.course_title || copy.unknownTitle}</span><span className="mt-1 block text-xs text-muted-foreground">{kinds[item.kind] ?? copy.unknownKind}</span></TableCell><TableCell>{item.lesson_title || copy.courseLevel}</TableCell><TableCell>{item.owner_display_name || copy.unknownOwner}</TableCell><TableCell><StatusBadge tone={item.state === "failed" ? "accent" : "neutral"} label={item.state === "failed" ? copy.failed : copy.stuck} /></TableCell><TableCell>{item.failure_category ? (failures[item.failure_category] ?? copy.unknownFailure) : copy.notAvailable}</TableCell><TableCell><time dateTime={item.updated_at}>{formatDateTime(item.updated_at, locale)}</time></TableCell><TableCell>{item.retry_action ? <Button type="button" size="sm" variant="outline" onClick={() => onRetry(item)}>{item.retry_action === "retry-enhancements" ? copy.retryEnhancements : copy.retryAction}</Button> : <span className="text-sm text-muted-foreground">{copy.recoveryInProgress}</span>}</TableCell></TableRow>;
}

function FailureCard({ item, copy, locale, onRetry }: { item: AdminMediaFailure; copy: ReturnType<typeof useLocale>["t"]["adminMediaFailures"]; locale: "ar" | "en"; onRetry: (item: AdminMediaFailure) => void }) {
  const failures = copy.failureCategories as Record<string, string>;
  return <article className="rounded-lg border border-border bg-card p-4"><div className="flex items-start justify-between gap-3"><div><p className="font-semibold text-foreground">{item.course_title || copy.unknownTitle}</p><p className="mt-1 text-sm text-muted-foreground">{item.lesson_title || copy.courseLevel}</p></div><StatusBadge tone={item.state === "failed" ? "accent" : "neutral"} label={item.state === "failed" ? copy.failed : copy.stuck} /></div><dl className="mt-4 grid gap-2 text-sm"><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{copy.owner}</dt><dd>{item.owner_display_name || copy.unknownOwner}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{copy.failure}</dt><dd>{item.failure_category ? (failures[item.failure_category] ?? copy.unknownFailure) : copy.notAvailable}</dd></div></dl>{item.retry_action ? <Button type="button" variant="outline" className="mt-4 w-full" onClick={() => onRetry(item)}>{item.retry_action === "retry-enhancements" ? copy.retryEnhancements : copy.retryAction}</Button> : null}</article>;
}
