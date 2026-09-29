"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import {
  auditActionLabel,
  auditModuleLabel,
  listAdminAuditEvents,
  type AdminAuditEvent,
  type AdminAuditFilters,
  type AdminAuditPage,
} from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import { formatDateTime } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { EmptyState } from "@/components/common/empty-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceToolbar,
} from "@/components/layout/workspace-page";

type AuditDraft = {
  actor: string;
  action: string;
  module: string;
  targetType: string;
  from: string;
  to: string;
};

const EMPTY_DRAFT: AuditDraft = {
  actor: "",
  action: "",
  module: "",
  targetType: "",
  from: "",
  to: "",
};

const PAGE_LIMIT = 20;
const UUID_LIKE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;

export function AdminAudit() {
  const { locale, t } = useLocale();
  const copy = t.adminAudit;
  const [draft, setDraft] = useState<AuditDraft>(EMPTY_DRAFT);
  const [filters, setFilters] = useState<AdminAuditFilters>({ page: 1, limit: PAGE_LIMIT });
  const [result, setResult] = useState<AdminAuditPage | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = useState<string | null>(null);
  const requestSequence = useRef(0);

  const loadEvents = useCallback(async () => {
    const requestID = ++requestSequence.current;
    setState("loading");
    setError(null);
    try {
      const next = await listAdminAuditEvents(locale, filters);
      if (requestID !== requestSequence.current) return;
      setResult(next);
      setState("ready");
    } catch (cause) {
      if (requestID !== requestSequence.current) return;
      setError(describeApiError(cause, locale));
      setState("failed");
    }
  }, [filters, locale]);

  useEffect(() => {
    void loadEvents();
  }, [loadEvents]);

  const updateDraft = <K extends keyof AuditDraft>(key: K, value: AuditDraft[K]) => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const applyFilters = (event: FormEvent) => {
    event.preventDefault();
    setFilters({
      actor: draft.actor,
      action: draft.action,
      module: draft.module,
      targetType: draft.targetType,
      from: draft.from,
      to: draft.to,
      page: 1,
      limit: PAGE_LIMIT,
    });
  };

  const clearFilters = () => {
    setDraft(EMPTY_DRAFT);
    setFilters({ page: 1, limit: PAGE_LIMIT });
  };

  return (
    <WorkspacePage testID="admin-audit-page">
      <WorkspacePageHeader
        title={copy.title}
        description={copy.description}
        status={
          result ? (
            <span className="text-sm font-semibold text-muted-foreground" aria-live="polite">
              {result.audit_events.length} {copy.resultCount}
            </span>
          ) : null
        }
      />
      <WorkspaceToolbar>
        <form onSubmit={applyFilters} className="grid w-full gap-3 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-6">
          <Field label={copy.actorLabel} htmlFor="admin-audit-actor" className="sm:col-span-2">
            <Input
              id="admin-audit-actor"
              value={draft.actor}
              onChange={(event) => updateDraft("actor", event.target.value)}
              placeholder={copy.actorPlaceholder}
              autoComplete="off"
            />
          </Field>
          <Field label={copy.actionLabel} htmlFor="admin-audit-action">
            <Select
              id="admin-audit-action"
              value={draft.action}
              onChange={(event) => updateDraft("action", event.target.value)}
            >
              <option value="">{copy.allActions}</option>
              {Object.entries(copy.actions).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </Select>
          </Field>
          <Field label={copy.moduleLabel} htmlFor="admin-audit-module">
            <Select
              id="admin-audit-module"
              value={draft.module}
              onChange={(event) => updateDraft("module", event.target.value)}
            >
              <option value="">{copy.allModules}</option>
              {Object.entries(copy.modules).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </Select>
          </Field>
          <Field label={copy.targetLabel} htmlFor="admin-audit-target">
            <Select
              id="admin-audit-target"
              value={draft.targetType}
              onChange={(event) => updateDraft("targetType", event.target.value)}
            >
              <option value="">{copy.allTargets}</option>
              {Object.entries(copy.targets).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-3 sm:col-span-2 lg:col-span-2">
            <Field label={copy.from} htmlFor="admin-audit-from">
              <Input
                id="admin-audit-from"
                type="date"
                value={draft.from}
                onChange={(event) => updateDraft("from", event.target.value)}
              />
            </Field>
            <Field label={copy.to} htmlFor="admin-audit-to">
              <Input
                id="admin-audit-to"
                type="date"
                value={draft.to}
                onChange={(event) => updateDraft("to", event.target.value)}
              />
            </Field>
          </div>
          <div className="flex flex-wrap items-end gap-2 sm:col-span-2 lg:col-span-6">
            <Button type="submit">{copy.applyFilters}</Button>
            <Button type="button" variant="outline" onClick={clearFilters}>
              {copy.clearFilters}
            </Button>
          </div>
        </form>
      </WorkspaceToolbar>

      {state === "failed" ? (
        <ErrorState
          className="mt-6"
          title={copy.loadFailed}
          detail={error}
          retryLabel={copy.retry}
          onRetry={() => setFilters((current) => ({ ...current }))}
          testID="admin-audit-error"
        />
      ) : null}
      {state === "loading" ? (
        <LoadingState className="mt-6" label={copy.loading} testID="admin-audit-loading" />
      ) : null}
      {state === "ready" && result && result.audit_events.length === 0 ? (
        <div className="mt-6">
          <EmptyState density="compact" headingLevel={2} title={copy.empty} description={copy.emptyDescription} />
        </div>
      ) : null}
      {state === "ready" && result && result.audit_events.length > 0 ? (
        <>
          <div className="mt-6 hidden md:block">
            <AuditTable events={result.audit_events} locale={locale} copy={copy} />
          </div>
          <ul className="mt-6 space-y-3 md:hidden" aria-label={copy.title}>
            {result.audit_events.map((event) => (
              <li key={event.id}>
                <AuditCard event={event} locale={locale} copy={copy} />
              </li>
            ))}
          </ul>
          <AuditPagination
            page={result.page}
            hasMore={result.has_more}
            onPageChange={(page) => setFilters((current) => ({
              ...current,
              page,
              asOf: page === 1 ? undefined : result.as_of,
            }))}
            copy={copy}
          />
        </>
      ) : null}
    </WorkspacePage>
  );
}

function AuditTable({
  events,
  locale,
  copy,
}: {
  events: AdminAuditEvent[];
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  return (
    <TableContainer>
      <Table>
        <TableCaption>{copy.tableCaption}</TableCaption>
        <TableHead>
          <TableRow>
            <TableHeaderCell scope="col">{copy.occurredAt}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.actionLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.actorLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.targetLabel}</TableHeaderCell>
            <TableHeaderCell scope="col">{copy.reason}</TableHeaderCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {events.map((event) => (
            <TableRow key={event.id}>
              <TableCell><time dateTime={event.occurred_at}>{formatDateTime(event.occurred_at, locale)}</time></TableCell>
              <TableCell><AuditAction event={event} copy={copy} /></TableCell>
              <TableCell><AuditActor event={event} copy={copy} /></TableCell>
              <TableCell><AuditTarget event={event} copy={copy} /></TableCell>
              <TableCell className="max-w-xs">{humanLabel(event.reason, copy.reasonUnavailable)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function AuditCard({
  event,
  locale,
  copy,
}: {
  event: AdminAuditEvent;
  locale: "ar" | "en";
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  return (
    <article className="rounded-lg border border-border bg-card p-4 shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <AuditAction event={event} copy={copy} />
        <time className="text-xs text-muted-foreground" dateTime={event.occurred_at}>
          {formatDateTime(event.occurred_at, locale)}
        </time>
      </div>
      <dl className="mt-4 grid gap-2 text-sm">
        <div className="flex flex-wrap gap-2">
          <dt className="text-muted-foreground">{copy.actorLabel}:</dt>
          <dd><AuditActor event={event} copy={copy} /></dd>
        </div>
        <div className="flex flex-wrap gap-2">
          <dt className="text-muted-foreground">{copy.targetLabel}:</dt>
          <dd><AuditTarget event={event} copy={copy} /></dd>
        </div>
        <div className="flex flex-wrap gap-2">
          <dt className="text-muted-foreground">{copy.reason}:</dt>
          <dd className="text-foreground">{humanLabel(event.reason, copy.reasonUnavailable)}</dd>
        </div>
      </dl>
    </article>
  );
}

function AuditAction({
  event,
  copy,
}: {
  event: AdminAuditEvent;
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  return (
    <div>
      <p className="font-display font-bold text-foreground">
        {auditActionLabel(event.action, copy.labels)}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        {auditModuleLabel(event.module, copy.labels)}
      </p>
    </div>
  );
}

function AuditActor({
  event,
  copy,
}: {
  event: AdminAuditEvent;
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  const label = humanLabel(event.actor_display_name, copy.unknownActor);
  const roleLabel = copy.roles[event.actor_role as keyof typeof copy.roles] ?? event.actor_role;
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <span className="font-semibold text-foreground">{label}</span>
      <StatusBadge tone="neutral" size="sm" label={roleLabel} />
    </span>
  );
}

function AuditTarget({
  event,
  copy,
}: {
  event: AdminAuditEvent;
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  const typeLabel = copy.targets[event.target_type as keyof typeof copy.targets] ?? event.target_type;
  const targetLabel = humanLabel(event.target_label || typeLabel, copy.unknownTarget);
  return <span className="font-semibold text-foreground">{targetLabel}</span>;
}

function AuditPagination({
  page,
  hasMore,
  onPageChange,
  copy,
}: {
  page: number;
  hasMore: boolean;
  onPageChange: (page: number) => void;
  copy: ReturnType<typeof useLocale>["t"]["adminAudit"];
}) {
  return (
    <nav className="mt-6 flex items-center justify-between gap-3" aria-label={copy.pagination}>
      <Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
        {copy.previous}
      </Button>
      <span className="text-sm font-semibold text-muted-foreground">{copy.page} {page}</span>
      <Button type="button" variant="outline" size="sm" disabled={!hasMore} onClick={() => onPageChange(page + 1)}>
        {copy.next}
      </Button>
    </nav>
  );
}

function humanLabel(value: string, fallback: string): string {
  if (!value.trim()) return fallback;
  return value.replace(UUID_LIKE, fallback);
}
