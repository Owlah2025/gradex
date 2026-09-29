"use client";

import { useEffect, useState } from "react";
import {
  getAdminInstructorProfile,
  listAdminInstructorProfiles,
  moderateInstructorProfile,
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

function toneForState(state: InstructorPublicationState) {
  if (state === "PUBLISHED") return "success" as const;
  if (state === "CHANGES_REQUESTED" || state === "HIDDEN") return "accent" as const;
  return "default" as const;
}

export function AdminInstructorProfiles() {
  const { locale, t } = useLocale();
  const copy = t.adminInstructorProfiles;
  const [state, setState] = useState<InstructorPublicationState | "">("PENDING_REVIEW");
  const [queue, setQueue] = useState<Awaited<ReturnType<typeof listAdminInstructorProfiles>> | null>(null);
  const [selected, setSelected] = useState<InstructorProfile | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [action, setAction] = useState<Action | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listAdminInstructorProfiles(locale, state)
      .then((result) => {
        if (cancelled) return;
        setQueue(result);
        setError(null);
        if (selected && !result.items.some((item) => item.account_id === selected.account_id)) setSelected(null);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : copy.loadFailed);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, copy.loadFailed, locale, selected, state]);

  const open = async (accountID: string) => {
    setDetailLoading(true);
    setError(null);
    try {
      setSelected(await getAdminInstructorProfile(accountID, locale));
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setDetailLoading(false);
    }
  };

  const confirm = async () => {
    if (!selected || !action || reason.trim() === "") return;
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(locale === "ar" ? "انتهت الجلسة." : "Your session ended.");
      return;
    }
    setBusy(true);
    try {
      const updated = await moderateInstructorProfile({
        locale,
        csrf,
        accountID: selected.account_id,
        action,
        reason: reason.trim(),
      });
      setSelected(updated);
      setReason("");
      setAction(null);
      setAttempt((current) => current + 1);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setBusy(false);
    }
  };

  const published = selected?.published_snapshot ?? null;
  const stateLabel = selected ? copy.states[selected.publication_state] : "";

  return (
    <WorkspacePage testID="admin-instructor-profiles">
      <WorkspacePageHeader title={copy.title} description={copy.description} />
      {error ? <div className="mt-6"><Alert tone="error" title={copy.loadFailed}>{error}</Alert></div> : null}

      <WorkspaceSection title={copy.queueTitle}>
        <Field label={copy.stateFilter} htmlFor="admin-instructor-profile-state">
          <Select id="admin-instructor-profile-state" value={state} onChange={(event) => setState(event.target.value as InstructorPublicationState | "")}>
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
          <div className="grid gap-4 lg:grid-cols-2">
            <ProfileVersionCard title={copy.draftTitle} profile={selected} copy={copy} />
            <PublishedVersionCard title={copy.publishedTitle} snapshot={published} copy={copy} />
          </div>
        </WorkspaceSection>
      ) : null}

      <ConfirmDialog
        open={action !== null}
        onOpenChange={(open) => { if (!open && !busy) { setAction(null); setReason(""); } }}
        title={action === "approve" ? copy.confirmApprove : action === "hide" ? copy.confirmHide : copy.confirmRequestChanges}
        body={copy.reasonTitle}
        confirmLabel={action === "approve" ? copy.confirmApprove : action === "hide" ? copy.confirmHide : copy.confirmRequestChanges}
        cancelLabel={locale === "ar" ? "إلغاء" : "Cancel"}
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

function ProfileVersionCard({
  title,
  profile,
  copy,
}: {
  title: string;
  profile: InstructorProfile;
  copy: Copy;
}) {
  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        <DiffRow label={copy.fields.slug} draft={profile.public_slug ?? "—"} published={undefined} />
        <DiffRow label={copy.fields.headline} draft={profile.headline_en || profile.headline_ar || "—"} published={undefined} />
        <DiffRow label={copy.fields.bio} draft={profile.bio_en || profile.bio_ar || "—"} published={undefined} multiline />
        <div>
          <p className="text-xs font-semibold text-muted-foreground">{copy.fields.expertise}</p>
          <div className="mt-2 flex flex-wrap gap-2">
            {profile.expertise.length > 0 ? profile.expertise.map((item) => <Badge key={item.id} variant="neutral"><bdi>{item.title_en || item.title_ar}</bdi></Badge>) : <span className="text-sm text-muted-foreground">—</span>}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function PublishedVersionCard({
  title,
  snapshot,
  copy,
}: {
  title: string;
  snapshot: InstructorProfile["published_snapshot"];
  copy: Copy;
}) {
  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        {snapshot ? (
          <>
            <DiffRow label={copy.fields.slug} draft={snapshot.public_slug} published={snapshot.public_slug} />
            <DiffRow label={copy.fields.headline} draft={snapshot.headline_en || snapshot.headline_ar || "—"} published={snapshot.headline_en || snapshot.headline_ar || "—"} />
            <DiffRow label={copy.fields.bio} draft={snapshot.bio_en || snapshot.bio_ar || "—"} published={snapshot.bio_en || snapshot.bio_ar || "—"} multiline />
          </>
        ) : <p className="text-sm text-muted-foreground">{copy.noPublished}</p>}
      </CardContent>
    </Card>
  );
}

function DiffRow({
  label,
  draft,
  published,
  multiline = false,
}: {
  label: string;
  draft: string;
  published?: string;
  multiline?: boolean;
}) {
  const changed = published !== undefined && draft !== published;
  return (
    <div className="border-b border-border pb-3 last:border-b-0">
      <p className="text-xs font-semibold text-muted-foreground">{label}</p>
      <div className="mt-1 grid gap-2 text-sm md:grid-cols-2">
        <p className={multiline ? "whitespace-pre-wrap text-foreground" : "break-all text-foreground"}>{draft}</p>
        {published !== undefined ? <p className={changed ? "whitespace-pre-wrap rounded-md bg-gx-orange-50 p-2 text-foreground" : "whitespace-pre-wrap text-muted-foreground"}>{published}</p> : null}
      </div>
    </div>
  );
}
