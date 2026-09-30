"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { isRecentAuthRequired } from "@/lib/api/admin-operations";
import { describeApiError } from "@/lib/api/api-error";
import {
  getAdminInstructorProfile,
  listAdminInstructorProfiles,
  moderateInstructorProfile,
  type InstructorExpertise,
  type InstructorProfile,
  type InstructorPublicationState,
} from "@/lib/api/instructor-profile";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { EmptyState } from "@/components/common/empty-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

type Action = "approve" | "request-changes" | "hide";
type Copy = ReturnType<typeof useLocale>["t"]["adminInstructorProfiles"];
type PublishedSnapshot = NonNullable<InstructorProfile["published_snapshot"]>;
const queueLimit = 25;

function toneForState(state: InstructorPublicationState) {
  if (state === "PUBLISHED") return "success" as const;
  if (state === "CHANGES_REQUESTED" || state === "HIDDEN") return "accent" as const;
  return "default" as const;
}

export function AdminInstructorProfiles() {
  const { locale, t } = useLocale();
  const copy = t.adminInstructorProfiles;
  const [state, setState] = useState<InstructorPublicationState | "">("PENDING_REVIEW");
  const [page, setPage] = useState(1);
  const [queue, setQueue] = useState<Awaited<ReturnType<typeof listAdminInstructorProfiles>> | null>(null);
  const [selected, setSelected] = useState<InstructorProfile | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [recentAuthRequired, setRecentAuthRequired] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [action, setAction] = useState<Action | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listAdminInstructorProfiles(locale, state, page, queueLimit)
      .then((result) => {
        if (cancelled) return;
        setQueue(result);
        setError(null);
        setRecentAuthRequired(false);
      })
      .catch((cause: unknown) => {
        if (cancelled) return;
        setRecentAuthRequired(false);
        setError(describeApiError(cause, locale) || copy.loadFailed);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, copy.loadFailed, locale, page, state]);

  const open = async (accountID: string) => {
    setDetailLoading(true);
    setError(null);
    setRecentAuthRequired(false);
    try {
      setSelected(await getAdminInstructorProfile(accountID, locale));
    } catch (cause: unknown) {
      setError(describeApiError(cause, locale) || copy.loadFailed);
    } finally {
      setDetailLoading(false);
    }
  };

  const confirm = async () => {
    if (!selected || !action || reason.trim() === "") return;
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(copy.sessionEnded);
      setRecentAuthRequired(false);
      return;
    }
    setBusy(true);
    setRecentAuthRequired(false);
    try {
      const updated = await moderateInstructorProfile({
        locale,
        csrf,
        accountID: selected.account_id,
        action,
        revision: selected.revision,
        reason: reason.trim(),
      });
      setSelected(updated);
      setError(null);
      setReason("");
      setAction(null);
      setAttempt((current) => current + 1);
    } catch (cause: unknown) {
      if (isRecentAuthRequired(cause)) {
        setRecentAuthRequired(true);
        setError(null);
      } else {
        setRecentAuthRequired(false);
        setError(describeApiError(cause, locale) || copy.loadFailed);
      }
    } finally {
      setBusy(false);
    }
  };

  const handleStateChange = (nextState: InstructorPublicationState | "") => {
    setState(nextState);
    setPage(1);
    setSelected(null);
    setError(null);
    setRecentAuthRequired(false);
  };

  const movePage = (nextPage: number) => {
    setPage(nextPage);
    setSelected(null);
    setError(null);
  };

  const published = selected?.published_snapshot ?? null;
  const stateLabel = selected ? copy.states[selected.publication_state] : "";
  const queuePage = queue?.page ?? page;

  return (
    <WorkspacePage testID="admin-instructor-profiles">
      <WorkspacePageHeader title={copy.title} description={copy.description} />
      {recentAuthRequired ? (
        <div className="mt-6">
          <Alert tone="error" title={copy.recentAuth}>
            <Link className="underline" href={`/${locale}/login?returnTo=${encodeURIComponent(`/${locale}/admin/instructor-profiles`)}`}>
              {copy.signInAgain}
            </Link>
          </Alert>
        </div>
      ) : error ? <div className="mt-6"><Alert tone="error" title={copy.loadFailed}>{error}</Alert></div> : null}

      <WorkspaceSection title={copy.queueTitle}>
        <Field label={copy.stateFilter} htmlFor="admin-instructor-profile-state">
          <Select id="admin-instructor-profile-state" value={state} onChange={(event) => handleStateChange(event.target.value as InstructorPublicationState | "")}>
            <option value="">{copy.allStates}</option>
            <option value="PENDING_REVIEW">{copy.states.PENDING_REVIEW}</option>
            <option value="CHANGES_REQUESTED">{copy.states.CHANGES_REQUESTED}</option>
            <option value="PUBLISHED">{copy.states.PUBLISHED}</option>
            <option value="HIDDEN">{copy.states.HIDDEN}</option>
            <option value="DRAFT">{copy.states.DRAFT}</option>
          </Select>
        </Field>

        {loading ? <LoadingState className="mt-4" label={copy.loading} /> : null}
        {!loading && queue?.items.length === 0 ? <EmptyState className="mt-4" density="compact" title={copy.empty} /> : null}
        {!loading && queue && queue.items.length > 0 ? (
          <div className="mt-4 grid gap-2" role="list">
            {queue.items.map((item) => (
              <button
                type="button"
                key={item.account_id}
                onClick={() => void open(item.account_id)}
                className="flex w-full flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-4 text-start transition-colors hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="min-w-0">
                  <span className="block font-display font-bold text-foreground"><bdi>{item.display_name}</bdi></span>
                  <span className="mt-1 block text-sm text-muted-foreground"><bdi>{item.public_slug ? "/" + locale + "/instructors/" + item.public_slug : copy.fields.slug}</bdi></span>
                </span>
                <StatusBadge tone={toneForState(item.publication_state)} label={copy.states[item.publication_state]} />
              </button>
            ))}
          </div>
        ) : null}
        {queue && (queuePage > 1 || queue.has_more) ? (
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <Button type="button" variant="outline" size="sm" disabled={loading || queuePage <= 1} onClick={() => movePage(queuePage - 1)}>
              {copy.previousPage}
            </Button>
            <span className="text-sm text-muted-foreground">{copy.pageLabel} {queuePage}</span>
            <Button type="button" variant="outline" size="sm" disabled={loading || !queue.has_more} onClick={() => movePage(queuePage + 1)}>
              {copy.nextPage}
            </Button>
          </div>
        ) : null}
      </WorkspaceSection>

      {detailLoading ? <LoadingState className="mt-8" label={copy.loading} /> : null}
      {selected ? (
        <WorkspaceSection
          title={selected.display_name}
          description={stateLabel}
          actions={
            <div className="flex flex-wrap gap-2">
              <Button type="button" onClick={() => setAction("approve")} disabled={selected.publication_state !== "PENDING_REVIEW"}>{copy.approve}</Button>
              <Button type="button" variant="outline" onClick={() => setAction("request-changes")} disabled={selected.publication_state !== "PENDING_REVIEW"}>{copy.requestChanges}</Button>
              <Button type="button" variant="destructive" onClick={() => setAction("hide")} disabled={selected.publication_state === "HIDDEN"}>{copy.hide}</Button>
            </div>
          }
        >
          <ProfileComparisonCard title={copy.diffTitle} profile={selected} snapshot={published} copy={copy} />
        </WorkspaceSection>
      ) : null}

      <ConfirmDialog
        open={action !== null}
        onOpenChange={(open) => { if (!open && !busy) { setAction(null); setReason(""); } }}
        title={action === "approve" ? copy.confirmApprove : action === "hide" ? copy.confirmHide : copy.confirmRequestChanges}
        body={copy.reasonTitle}
        confirmLabel={action === "approve" ? copy.confirmApprove : action === "hide" ? copy.confirmHide : copy.confirmRequestChanges}
        cancelLabel={copy.cancel}
        busy={busy}
        onConfirm={() => void confirm()}
        confirmDisabled={reason.trim() === ""}
      >
        <Field label={copy.reasonLabel} htmlFor="moderation-reason">
          <Textarea id="moderation-reason" value={reason} onChange={(event) => setReason(event.target.value)} placeholder={copy.reasonPlaceholder} maxLength={4000} rows={5} />
        </Field>
      </ConfirmDialog>
    </WorkspacePage>
  );
}

function ProfileComparisonCard({
  title,
  profile,
  snapshot,
  copy,
}: {
  title: string;
  profile: InstructorProfile;
  snapshot: PublishedSnapshot | null;
  copy: Copy;
}) {
  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-2 text-xs font-semibold text-muted-foreground md:grid-cols-2">
          <span>{copy.draftColumn}</span>
          <span>{copy.publishedColumn}</span>
        </div>
        <DiffRow label={copy.fields.slug} draft={profile.public_slug ?? ""} published={snapshot?.public_slug} draftDir="ltr" publishedDir="ltr" emptyValue={copy.emptyValue} />
        <DiffRow label={copy.fields.headlineAr} draft={profile.headline_ar} published={snapshot?.headline_ar} draftDir="rtl" publishedDir="rtl" emptyValue={copy.emptyValue} />
        <DiffRow label={copy.fields.headlineEn} draft={profile.headline_en} published={snapshot?.headline_en} draftDir="ltr" publishedDir="ltr" emptyValue={copy.emptyValue} />
        <DiffRow label={copy.fields.bioAr} draft={profile.bio_ar} published={snapshot?.bio_ar} draftDir="rtl" publishedDir="rtl" multiline emptyValue={copy.emptyValue} />
        <DiffRow label={copy.fields.bioEn} draft={profile.bio_en} published={snapshot?.bio_en} draftDir="ltr" publishedDir="ltr" multiline emptyValue={copy.emptyValue} />
        <ExpertiseDiff draft={profile.expertise} published={snapshot?.expertise} copy={copy} />
        {!snapshot ? <p className="text-sm text-muted-foreground">{copy.noPublished}</p> : null}
      </CardContent>
    </Card>
  );
}

function ExpertiseDiff({
  draft,
  published,
  copy,
}: {
  draft: InstructorExpertise[];
  published?: InstructorExpertise[];
  copy: Copy;
}) {
  const changed = published !== undefined && draft.map((item) => item.id).join(",") !== published.map((item) => item.id).join(",");
  return (
    <div className="border-b border-border pb-3 last:border-b-0">
      <p className="text-xs font-semibold text-muted-foreground">{copy.fields.expertise}</p>
      <div className="mt-2 grid gap-3 md:grid-cols-2">
        <ExpertiseList items={draft} changed={changed} copy={copy} />
        {published !== undefined ? <ExpertiseList items={published} changed={changed} copy={copy} /> : <p className="text-sm text-muted-foreground">{copy.emptyValue}</p>}
      </div>
    </div>
  );
}

function ExpertiseList({ items, changed, copy }: { items: InstructorExpertise[]; changed: boolean; copy: Copy }) {
  if (items.length === 0) return <p className="text-sm text-muted-foreground">{copy.emptyValue}</p>;
  return (
    <ul className={changed ? "rounded-md bg-gx-orange-50 p-2" : "space-y-2"}>
      {items.map((item) => (
        <li key={item.id}>
          <Badge variant="neutral">
            <span dir="rtl"><bdi>{item.title_ar || copy.emptyValue}</bdi></span>
            <span aria-hidden> · </span>
            <span dir="ltr"><bdi>{item.title_en || copy.emptyValue}</bdi></span>
          </Badge>
        </li>
      ))}
    </ul>
  );
}

function DiffRow({
  label,
  draft,
  published,
  draftDir,
  publishedDir,
  multiline = false,
  emptyValue,
}: {
  label: string;
  draft: string;
  published?: string;
  draftDir: "rtl" | "ltr";
  publishedDir: "rtl" | "ltr";
  multiline?: boolean;
  emptyValue: string;
}) {
  const changed = published !== undefined && draft !== published;
  const valueClass = multiline ? "whitespace-pre-wrap" : "break-all";
  const draftClass = changed ? "rounded-md bg-gx-orange-50 p-2 text-foreground" : "text-foreground";
  const publishedClass = changed ? "rounded-md bg-gx-orange-50 p-2 text-foreground" : "text-muted-foreground";
  return (
    <div className="border-b border-border pb-3 last:border-b-0">
      <p className="text-xs font-semibold text-muted-foreground">{label}</p>
      <div className="mt-1 grid gap-2 text-sm md:grid-cols-2">
        <p dir={draftDir} className={`${valueClass} ${draftClass}`}><bdi>{draft || emptyValue}</bdi></p>
        {published !== undefined ? <p dir={publishedDir} className={`${valueClass} ${publishedClass}`}><bdi>{published || emptyValue}</bdi></p> : <p className="text-muted-foreground">{emptyValue}</p>}
      </div>
    </div>
  );
}
