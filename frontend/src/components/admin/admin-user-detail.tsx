"use client";

import Link from "next/link";
import * as React from "react";
import {
  accessReasonMessage,
  addAdminNote,
  diagnoseAdminAccess,
  getAdminUser360,
  isRecentAuthRequired,
  listAdminSecurityEvents,
  listAdminCourseOptions,
  provenanceLabel,
  revokeAccountSessions,
  type AccessDiagnostic,
  type AdminCourseOption,
  type AdminEmailDelivery,
  type AdminNote,
  type AdminUser360,
  type UserEntitlement,
  type UserInvitation,
} from "@/lib/api/admin-operations";
import { adjustEntitlementExpiry, createCourseAccessInvitation, revokeEntitlement, type CourseAccessInvitation } from "@/lib/api/access";
import { suspendStaffAccount, reinstateStaffAccount } from "@/lib/api/identity";
import {
  listAdminDevices,
  resetAdminDeviceCooldown,
  revokeAdminDevice,
  revokeAllAdminDevices,
} from "@/lib/api/devices";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
import { ProblemError } from "@/lib/api/problem";
import { formatDate, formatDateTime } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { EmptyState } from "@/components/common/empty-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

type Tab = "overview" | "access" | "progress" | "security" | "notes" | "audit" | "diagnostic";
type Action = "suspend" | "restore" | "signout" | "grant" | "revoke" | "expiry" | "device" | "devices" | "cooldown";
type PendingAction = { kind: Action; entitlement?: UserEntitlement; deviceID?: string };
type AdminStudent = NonNullable<AdminUser360["student"]>;
const securityPageLimit = 10;
const UUID_LIKE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const tabs: Tab[] = ["overview", "access", "progress", "security", "notes", "audit", "diagnostic"];

export function AdminUserDetail({ accountID }: { accountID: string }) {
  const { locale, t } = useLocale();
  const copy = t.adminUserDetail;
  const [model, setModel] = React.useState<AdminUser360 | null>(null);
  const [state, setState] = React.useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = React.useState<string | null>(null);
  const [notFound, setNotFound] = React.useState(false);
  const [activeTab, setActiveTab] = React.useState<Tab>("overview");
  const [pending, setPending] = React.useState<PendingAction | null>(null);
  const [reason, setReason] = React.useState("");
  const [expiryDate, setExpiryDate] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [courseOptions, setCourseOptions] = React.useState<AdminCourseOption[]>([]);
  const [selectedCourse, setSelectedCourse] = React.useState("");
  const [diagnostic, setDiagnostic] = React.useState<AccessDiagnostic | null>(null);
  const [diagnosticState, setDiagnosticState] = React.useState<"idle" | "loading" | "failed">("idle");

  const load = React.useCallback(async () => {
    setState("loading");
    setError(null);
    setNotFound(false);
    try { setModel(await getAdminUser360(accountID, locale)); setState("ready"); }
    catch (cause) {
      setNotFound(cause instanceof ProblemError && cause.problem.status === 404);
      setError(describeApiError(cause, locale));
      setState("failed");
    }
  }, [accountID, locale]);

  React.useEffect(() => { void load(); }, [load]);
  React.useEffect(() => {
    if ((activeTab !== "diagnostic" && activeTab !== "access") || !model) return;
    let active = true;
    void listAdminCourseOptions(accountID, locale).then((result) => { if (active) setCourseOptions(result.courses); }).catch(() => { if (active) setCourseOptions([]); });
    return () => { active = false; };
  }, [accountID, activeTab, locale, model]);

  const updateStudent = React.useCallback((update: (student: AdminStudent) => AdminStudent) => {
    setModel((current) => current?.student ? { ...current, student: update(current.student) } : current);
  }, []);

  const refreshDevices = React.useCallback(async () => {
    const devices = await listAdminDevices(accountID, locale);
    updateStudent((student) => ({ ...student, devices }));
  }, [accountID, locale, updateStudent]);

  const refreshSecurityEvents = React.useCallback(async () => {
    const result = await listAdminSecurityEvents(accountID, locale, 1, securityPageLimit);
    updateStudent((student) => ({ ...student, security_events: result.events }));
  }, [accountID, locale, updateStudent]);

  const handleNoteAdded = React.useCallback((note: AdminNote) => {
    setModel((current) => {
      if (!current) return current;
      if (current.student) return { ...current, student: { ...current.student, notes: [note, ...current.student.notes] } };
      if (current.instructor) return { ...current, instructor: { ...current.instructor, notes: [note, ...current.instructor.notes] } };
      return current;
    });
  }, []);

  const runAction = async () => {
    if (!pending || !model || busy) return;
    const action = pending;
    const csrf = currentCSRFToken() ?? undefined;
    setBusy(true);
    setNotice(null);
    try {
      switch (action.kind) {
        case "suspend":
          await suspendStaffAccount(accountID, reason.trim(), locale, csrf);
          setModel((current) => current ? { ...current, identity: { ...current.identity, status: "SUSPENDED" } } : current);
          break;
        case "restore":
          await reinstateStaffAccount(accountID, reason.trim(), locale, csrf);
          setModel((current) => current ? { ...current, identity: { ...current.identity, status: "ACTIVE" } } : current);
          break;
        case "signout":
          await revokeAccountSessions(accountID, reason.trim(), locale, csrf);
          await refreshSecurityEvents();
          break;
        case "grant": {
          const invitation = await createCourseAccessInvitation(selectedCourse, model.identity.email, reason.trim(), undefined, locale, csrf);
          if (invitation) {
            const title = courseOptions.find((course) => course.id === invitation.course_id)?.title ?? "";
            updateStudent((student) => ({
              ...student,
              invitations: [toUserInvitation(invitation, title), ...student.invitations],
            }));
          }
          break;
        }
        case "expiry":
          if (action.entitlement) {
            const result = await adjustEntitlementExpiry(action.entitlement.id, expiryDate, reason.trim(), { expectedRevision: action.entitlement.revision }, locale, csrf);
            if (result?.entitlement) updateEntitlement(updateStudent, { ...result.entitlement, adjustments: result.adjustments });
          }
          break;
        case "revoke":
          if (action.entitlement) {
            const result = await revokeEntitlement(action.entitlement.id, reason.trim(), { expectedRevision: action.entitlement.revision }, locale, csrf);
            if (result?.entitlement) updateEntitlement(updateStudent, { ...result.entitlement, adjustments: result.adjustments });
          }
          break;
        case "device":
          if (action.deviceID) await revokeAdminDevice(accountID, action.deviceID, locale, csrf ?? "");
          await refreshDevices();
          break;
        case "devices":
          await revokeAllAdminDevices(accountID, locale, csrf ?? "");
          await refreshDevices();
          break;
        case "cooldown":
          await resetAdminDeviceCooldown(accountID, locale, csrf ?? "");
          await refreshDevices();
          break;
      }
      setNotice(copy.completed[action.kind]);
      setPending(null);
      setReason("");
      setExpiryDate("");
    } catch (cause) {
      setNotice(isRecentAuthRequired(cause) ? copy.recentAuth : describeApiError(cause, locale));
    } finally {
      setBusy(false);
    }
  };

  if (state === "loading") return <WorkspacePage testID="admin-user-detail-page"><LoadingState label={copy.loading} testID="admin-user-detail-loading" /></WorkspacePage>;
  if (state === "failed" || !model) return <WorkspacePage testID="admin-user-detail-page">
    <ErrorState
      title={notFound ? copy.notFound : copy.loadFailed}
      detail={error}
      retryLabel={notFound ? undefined : copy.retry}
      onRetry={notFound ? undefined : () => void load()}
      testID="admin-user-detail-error"
    />
    <div className="mt-4">
      <Button asChild variant="outline" size="sm">
        <Link href={`/${locale}/admin/users`}>{copy.backToUsers}</Link>
      </Button>
    </div>
  </WorkspacePage>;

  const account = model.identity;
  const emails = model.emails ?? [];
  const student = model.student;
  const instructor = model.instructor;
  const visibleTabs: Tab[] = account.role === "STUDENT" ? tabs : account.role === "INSTRUCTOR" ? ["overview", "notes", "audit"] : ["overview"];
  return <WorkspacePage testID="admin-user-detail-page">
    <WorkspacePageHeader title={account.display_name} description={copy.description} breadcrumb={<Button asChild variant="ghost" size="sm"><Link href={`/${locale}/admin/users`}>{copy.backToUsers}</Link></Button>} status={<><StatusBadge tone={account.status === "ACTIVE" ? "success" : account.status === "SUSPENDED" ? "accent" : "neutral"} label={copy.status[account.status]} detail={copy.roles[account.role]} /><span className="text-sm text-muted-foreground"><bdi>{account.email}</bdi></span></>} actions={<ActionBar account={account} copy={copy} onAction={(kind) => { setReason(""); if (kind === "grant") setActiveTab("access"); setPending({ kind }); }} />} />
    {notice ? <div className="mt-4"><Alert tone="info" title={notice}>{notice === copy.recentAuth ? <Link className="underline" href={`/${locale}/login?returnTo=${encodeURIComponent(`/${locale}/admin/users/${accountID}`)}`}>{copy.signInAgain}</Link> : undefined}</Alert></div> : null}
    <nav className="mt-6" aria-label={copy.sectionsLabel}>
      <label className="sr-only" htmlFor="admin-user-tab-select">{copy.sectionsLabel}</label>
      <Select id="admin-user-tab-select" className="md:hidden" value={activeTab} onChange={(event) => setActiveTab(event.target.value as Tab)}>{visibleTabs.map((tab) => <option key={tab} value={tab}>{copy.tabs[tab]}</option>)}</Select>
      <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as Tab)} dir={locale === "ar" ? "rtl" : "ltr"} className="mt-4">
        <TabsList className="hidden flex-wrap md:flex" aria-label={copy.sectionsLabel}>{visibleTabs.map((tab) => <TabsTrigger key={tab} value={tab}>{copy.tabs[tab]}</TabsTrigger>)}</TabsList>
        <TabsContent value="overview"><Overview account={account} emails={emails} student={student} instructor={instructor} copy={copy} locale={locale} /></TabsContent>
        {student ? <TabsContent value="access"><AccessPanel student={student} copy={copy} locale={locale} options={courseOptions} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} onGrant={() => setPending({ kind: "grant" })} onAction={(next) => { setReason(""); setExpiryDate(""); setPending(next); }} /></TabsContent> : null}
        {student ? <TabsContent value="progress"><ProgressPanel student={student} copy={copy} locale={locale} /></TabsContent> : null}
        {student ? <TabsContent value="security"><SecurityPanel accountID={accountID} student={student} copy={copy} locale={locale} onAction={(next) => { setReason(""); setPending(next); }} /></TabsContent> : null}
        <TabsContent value="notes"><NotesPanel accountID={accountID} notes={student?.notes ?? instructor?.notes ?? []} copy={copy} locale={locale} onAdded={handleNoteAdded} /></TabsContent>
        <TabsContent value="audit"><AuditPanel events={student?.audit_events ?? instructor?.audit_events ?? []} copy={copy} locale={locale} /></TabsContent>
        {student ? <TabsContent value="diagnostic"><DiagnosticPanel copy={copy} locale={locale} options={courseOptions} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} diagnostic={diagnostic} state={diagnosticState} onDiagnose={async () => { if (!selectedCourse) return; setDiagnosticState("loading"); try { setDiagnostic(await diagnoseAdminAccess(accountID, selectedCourse, locale)); setDiagnosticState("idle"); } catch { setDiagnosticState("failed"); } }} /></TabsContent> : null}
      </Tabs>
    </nav>
    {pending ? <ActionDialog pending={pending} copy={copy} courseOptions={courseOptions} reason={reason} expiryDate={expiryDate} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} onReason={setReason} onExpiryDate={setExpiryDate} onClose={() => { if (!busy) setPending(null); }} busy={busy} onConfirm={() => void runAction()} /> : null}
  </WorkspacePage>;
}

function Overview({ account, emails, student, instructor, copy, locale }: { account: AdminUser360["identity"]; emails: AdminEmailDelivery[]; student?: AdminUser360["student"]; instructor?: AdminUser360["instructor"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <>
    <WorkspaceSection title={copy.identitySection} description={copy.identityDescription}><div className="rounded-lg border border-border bg-card p-5"><dl className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3"><IdentityFact label={copy.name} value={account.display_name} /><IdentityFact label={copy.email} value={account.email} direction="ltr" /><IdentityFact label={copy.role} value={copy.roles[account.role]} /><IdentityFact label={copy.statusLabel} value={copy.status[account.status]} /><IdentityFact label={copy.locale} value={account.locale === "ar" ? copy.arabic : copy.english} /><IdentityFact label={copy.emailVerified} value={account.email_verified ? copy.verified : copy.notVerified} /><IdentityFact label={copy.joined} value={formatDate(account.created_at, locale)} /><IdentityFact label={copy.lastActivity} value={account.last_sign_in_activity_at ? formatDateTime(account.last_sign_in_activity_at, locale) : copy.noActivity} /><IdentityFact label={copy.learningActivity} value={account.last_learning_activity_at ? formatDateTime(account.last_learning_activity_at, locale) : copy.noActivity} /></dl></div></WorkspaceSection>
    <EmailPanel emails={emails} copy={copy} locale={locale} />
    {student?.academic_profile ? <WorkspaceSection title={copy.academicProfile} description={copy.academicDescription}><Facts values={[[copy.institution, student.academic_profile.institution_label || copy.notAvailable], [copy.academicUnit, student.academic_profile.academic_unit_label || copy.notAvailable], [copy.program, student.academic_profile.program_label || copy.notAvailable], [copy.curriculum, student.academic_profile.curriculum_label || copy.notAvailable], [copy.enrollmentStatus, labelFor(copy.enrollmentStatuses, student.academic_profile.enrollment_status, copy.unknownValue)], [copy.level, student.academic_profile.current_level ? String(student.academic_profile.current_level) : copy.notAvailable]]} /></WorkspaceSection> : null}
    {instructor ? <WorkspaceSection title={copy.ownedCourses} description={copy.instructorDescription}><CourseTable courses={instructor.owned_courses} copy={copy} locale={locale} instructor /></WorkspaceSection> : null}
  </>;
}

function EmailPanel({ emails, copy, locale }: { emails: AdminEmailDelivery[]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <WorkspaceSection title={copy.emailsTitle} description={copy.emailsDescription}>
    {emails.length === 0 ? <EmptyState density="compact" title={copy.noEmails} /> : (
      <div className="space-y-3">
        {emails.map((email) => (
          <article key={email.id} className="rounded-lg border border-border bg-card p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="font-semibold text-foreground">{(copy.emailKinds as Record<string, string>)[email.kind] ?? copy.unknownValue}</p>
                <p className="mt-1 text-sm text-muted-foreground" dir="ltr"><bdi>{email.recipient}</bdi></p>
              </div>
              <StatusBadge tone={email.state === "delivered" ? "success" : email.state === "failed" ? "accent" : "neutral"} label={copy.emailStates[email.state]} />
            </div>
            <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <IdentityFact label={copy.emailQueued} value={formatDateTime(email.queued_at, locale)} />
              <IdentityFact label={copy.emailAttempted} value={email.attempted_at ? formatDateTime(email.attempted_at, locale) : copy.notAvailable} />
              <IdentityFact label={copy.emailAttempts} value={String(email.attempt_count)} />
              <IdentityFact label={copy.emailLastError} value={email.last_error_class || copy.notAvailable} />
            </dl>
          </article>
        ))}
      </div>
    )}
  </WorkspaceSection>;
}

function AccessPanel({ student, copy, locale, options, selectedCourse, onCourseChange, onGrant, onAction }: { student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; options: AdminCourseOption[]; selectedCourse: string; onCourseChange: (value: string) => void; onGrant: () => void; onAction: (action: PendingAction) => void }) {
  return <WorkspaceSection title={copy.accessTitle} description={copy.accessDescription} actions={<div className="flex flex-wrap gap-2"><Select aria-label={copy.courseIdLabel} value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.courseIdPlaceholder}</option>{options.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select><Button size="sm" onClick={onGrant} disabled={!selectedCourse}>{copy.grantAccess}</Button></div>}>
    {student.entitlements.length === 0 ? <EmptyState density="compact" title={copy.noEntitlements} description={copy.noEntitlementsDescription} /> : <div className="space-y-3">{student.entitlements.map((entitlement) => <article key={entitlement.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="font-display font-bold"><bdi>{entitlement.course_title}</bdi></h3><p className="mt-1 text-sm text-muted-foreground">{provenanceLabel(entitlement.grant_source, copy.provenance)}{safeReference(entitlement.source_reference_label, "") ? <> · <bdi>{safeReference(entitlement.source_reference_label, "")}</bdi></> : null}</p><p className="text-sm text-muted-foreground">{copy.grantedBy}: {entitlement.granted_by_display_name || copy.notAvailable}</p></div><StatusBadge tone={entitlement.state === "ACTIVE" ? "success" : entitlement.state === "REVOKED" ? "accent" : "neutral"} label={labelFor(copy.entitlementStates, entitlement.state, copy.unknownValue)} /></div><dl className="mt-3 grid gap-2 text-sm sm:grid-cols-3"><div><dt className="text-muted-foreground">{copy.scope}</dt><dd>{labelFor(copy.scopeKinds, entitlement.scope_kind, copy.unknownValue)}</dd></div><div><dt className="text-muted-foreground">{copy.accessEnds}</dt><dd>{formatDateTime(entitlement.access_ends_at, locale)}</dd></div><div><dt className="text-muted-foreground">{copy.originalAccessEnds}</dt><dd>{formatDateTime(entitlement.original_access_ends_at, locale)}</dd></div></dl>{entitlement.state !== "REVOKED" ? <div className="mt-3 flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => onAction({ kind: "expiry", entitlement })}>{copy.adjustExpiry}</Button><Button size="sm" variant="destructive" onClick={() => onAction({ kind: "revoke", entitlement })}>{copy.revokeAccess}</Button></div> : null}{entitlement.adjustments.length ? <ul className="mt-3 border-t border-border pt-3 text-sm text-muted-foreground">{entitlement.adjustments.map((adjustment) => <li key={adjustment.id}>{formatDateTime(adjustment.adjusted_at, locale)} · <bdi>{adjustment.reason}</bdi></li>)}</ul> : null}</article>)}</div>}
    <AccessHistory student={student} copy={copy} locale={locale} />
  </WorkspaceSection>;
}

function AccessHistory({ student, copy, locale }: { student: AdminStudent; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <div className="mt-6 grid gap-5 lg:grid-cols-2">
    <section className="rounded-lg border border-border bg-card p-4" aria-labelledby="admin-invitations-heading"><h3 id="admin-invitations-heading" className="font-display font-bold">{copy.invitationsTitle}</h3>{student.invitations.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">{copy.noInvitations}</p> : <ul className="mt-3 space-y-3">{student.invitations.map((invitation) => <li key={invitation.id} className="border-t border-border pt-3 first:border-t-0 first:pt-0"><p className="font-semibold"><bdi>{invitation.course_title || copy.notAvailable}</bdi></p><dl className="mt-1 grid gap-1 text-sm text-muted-foreground sm:grid-cols-2"><div><dt className="inline">{copy.invitationState}: </dt><dd className="inline">{labelFor(copy.invitationStates, invitation.state, copy.unknownValue)}</dd></div><div><dt className="inline">{copy.invitationCreated}: </dt><dd className="inline">{formatDateTime(invitation.created_at, locale)}</dd></div><div><dt className="inline">{copy.reference}: </dt><dd className="inline"><bdi>{safeReference(invitation.external_reference, copy.notAvailable)}</bdi></dd></div></dl></li>)}</ul>}</section>
    <section className="rounded-lg border border-border bg-card p-4" aria-labelledby="admin-purchases-heading"><h3 id="admin-purchases-heading" className="font-display font-bold">{copy.purchaseRequestsTitle}</h3>{student.purchase_requests.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">{copy.noPurchaseRequests}</p> : <ul className="mt-3 space-y-3">{student.purchase_requests.map((request) => <li key={request.id} className="border-t border-border pt-3 first:border-t-0 first:pt-0"><p className="font-semibold"><bdi>{request.course_title || request.bundle_title || copy.notAvailable}</bdi></p><dl className="mt-1 grid gap-1 text-sm text-muted-foreground sm:grid-cols-2"><div><dt className="inline">{copy.purchaseState}: </dt><dd className="inline">{labelFor(copy.purchaseRequestStates, request.state, copy.unknownValue)}</dd></div><div><dt className="inline">{copy.requestedAt}: </dt><dd className="inline">{formatDateTime(request.requested_at, locale)}</dd></div><div><dt className="inline">{copy.reference}: </dt><dd className="inline"><bdi>{safeReference(request.reference, copy.notAvailable)}</bdi></dd></div></dl></li>)}</ul>}</section>
  </div>;
}

function ProgressPanel({ student, copy, locale }: { student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <WorkspaceSection title={copy.progressTitle} description={copy.progressDescription}><CourseTable courses={student.courses} copy={copy} locale={locale} /></WorkspaceSection>;
}

function SecurityPanel({ accountID, student, copy, locale, onAction }: { accountID: string; student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; onAction: (action: PendingAction) => void }) {
  const [events, setEvents] = React.useState(student.security_events);
  const [page, setPage] = React.useState(1);
  const [hasMore, setHasMore] = React.useState(student.security_events.length >= securityPageLimit);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [moreError, setMoreError] = React.useState(false);
  React.useEffect(() => { setEvents(student.security_events); setPage(1); setHasMore(student.security_events.length >= securityPageLimit); }, [student.security_events]);
  const loadMore = async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    setMoreError(false);
    try {
      const next = await listAdminSecurityEvents(accountID, locale, page + 1, securityPageLimit);
      setEvents((current) => [...current, ...next.events]);
      setPage(next.page);
      setHasMore(next.has_more && next.events.length > 0);
    } catch { setMoreError(true); } finally { setLoadingMore(false); }
  };
  return <WorkspaceSection title={copy.securityTitle} description={copy.securityDescription} actions={<div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => onAction({ kind: "devices" })}>{copy.revokeAllDevices}</Button><Button size="sm" variant="outline" onClick={() => onAction({ kind: "cooldown" })}>{copy.resetCooldown}</Button></div>}>
    {student.devices.devices.length === 0 ? <EmptyState density="compact" title={copy.noDevices} description={copy.noDevicesDescription} /> : <div className="space-y-3">{student.devices.devices.map((device) => <div key={device.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-4"><div><p className="font-semibold">{device.label}</p><p className="text-sm text-muted-foreground">{device.browser_family} · {device.platform_family}</p><dl className="mt-2 grid gap-1 text-sm sm:grid-cols-2"><div><dt className="inline text-muted-foreground">{copy.deviceState}: </dt><dd className="inline">{labelFor(copy.deviceStates, device.state, copy.unknownValue)}</dd></div><div><dt className="inline text-muted-foreground">{copy.lastActive}: </dt><dd className="inline">{formatDateTime(device.last_active_at, locale)}</dd></div></dl></div>{device.state === "TRUSTED" ? <Button size="sm" variant="destructive" onClick={() => onAction({ kind: "device", deviceID: device.id })}>{copy.revokeDevice}</Button> : null}</div>)}</div>}
    <div className="mt-6 space-y-2">{events.length === 0 ? <EmptyState density="compact" title={copy.noSecurityEvents} description={copy.noSecurityEventsDescription} /> : events.map((event) => <div key={event.id} className="flex flex-wrap justify-between gap-3 border-b border-border py-3 text-sm"><span className="font-semibold">{labelFor(copy.securityEvents, event.event_type, copy.unknownValue)}</span><time className="text-muted-foreground" dateTime={event.occurred_at}>{formatDateTime(event.occurred_at, locale)}</time></div>)}</div>{moreError ? <div className="mt-4"><Alert tone="error" title={copy.securityLoadFailed} /></div> : null}{hasMore ? <div className="mt-4"><Button size="sm" variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>{copy.loadMoreSecurity}</Button></div> : null}
  </WorkspaceSection>;
}

function NotesPanel({ accountID, notes, copy, locale, onAdded }: { accountID: string; notes: AdminNote[]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; onAdded: (note: AdminNote) => void }) {
  const [body, setBody] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const add = async () => {
    if (!body.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      const note = await addAdminNote(accountID, body, locale);
      setBody("");
      onAdded(note);
    } catch (cause) {
      setError(describeApiError(cause, locale));
    } finally {
      setBusy(false);
    }
  };
  return <WorkspaceSection title={copy.notesTitle} description={copy.notesDescription}><div className="rounded-lg border border-border bg-card p-4"><Field label={copy.noteBody} htmlFor="admin-note-body"><textarea id="admin-note-body" className="min-h-28 w-full rounded-md border border-input bg-background p-3 text-sm focus-visible:outline focus-visible:ring-2 focus-visible:ring-ring" maxLength={4000} value={body} onChange={(event) => setBody(event.target.value)} /></Field>{error ? <div className="mt-3"><Alert tone="error" title={copy.noteAddFailed}>{error}</Alert></div> : null}<div className="mt-3 flex justify-end"><Button size="sm" onClick={() => void add()} disabled={busy || !body.trim()}>{copy.addNote}</Button></div></div><div className="mt-4 space-y-3">{notes.map((note) => <article key={note.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap justify-between gap-2 text-sm"><span className="font-semibold">{note.author_name}</span><time className="text-muted-foreground" dateTime={note.created_at}>{formatDateTime(note.created_at, locale)}</time></div><p className="mt-3 whitespace-pre-wrap text-sm leading-6"><bdi>{note.body}</bdi></p></article>)}</div></WorkspaceSection>;
}

function AuditPanel({ events, copy, locale }: { events: NonNullable<AdminUser360["student"]>["audit_events"] | NonNullable<AdminUser360["instructor"]>["audit_events"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <WorkspaceSection title={copy.auditTitle} description={copy.auditDescription}>{events.length === 0 ? <EmptyState density="compact" title={copy.noAudit} description={copy.noAuditDescription} /> : <div className="space-y-2">{events.map((event) => { const knownReason = (copy.auditReasons as Record<string, string>)[event.reason]; return <div key={event.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap justify-between gap-2"><span className="font-semibold">{labelFor(copy.auditActions, event.action, copy.unknownValue)}</span><time className="text-sm text-muted-foreground" dateTime={event.occurred_at}>{formatDateTime(event.occurred_at, locale)}</time></div><p className="mt-2 text-sm text-muted-foreground">{knownReason ?? (event.reason.trim() ? <><span>{copy.operatorReason}: </span><bdi>{operatorReasonText(event.reason, copy)}</bdi></> : copy.reasonUnavailable)}</p></div>; })}</div>}</WorkspaceSection>;
}

function DiagnosticPanel({ copy, locale, options, selectedCourse, onCourseChange, diagnostic, state, onDiagnose }: { copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; options: AdminCourseOption[]; selectedCourse: string; onCourseChange: (value: string) => void; diagnostic: AccessDiagnostic | null; state: "idle" | "loading" | "failed"; onDiagnose: () => void }) {
  return <WorkspaceSection title={copy.diagnosticTitle} description={copy.diagnosticDescription}><div className="flex flex-wrap items-end gap-2"><Field label={copy.coursePicker} htmlFor="diagnostic-course"><Select id="diagnostic-course" value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.chooseCourse}</option>{options.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select></Field><Button onClick={onDiagnose} disabled={!selectedCourse || state === "loading"}>{copy.diagnose}</Button></div>{state === "failed" ? <div className="mt-4"><Alert tone="error" title={copy.diagnosticFailed} /></div> : null}{diagnostic ? <div className="mt-5 rounded-lg border border-border bg-card p-5"><StatusBadge tone={diagnostic.allowed ? "success" : "accent"} label={accessReasonMessage(diagnostic.primary_reason_code, copy.reasonCodes)} /><h3 className="mt-3 font-display text-lg font-bold"><bdi>{diagnostic.course_title}</bdi></h3><ul className="mt-4 space-y-2 text-sm">{diagnostic.facts.map((fact) => <li key={fact.code + ":" + (fact.value ?? "")} className="flex flex-wrap gap-2"><span className="font-semibold">{labelFor(copy.factLabels, fact.code, copy.unknownValue)}</span><span className="text-muted-foreground">{factValueLabel(fact, copy, locale)}</span></li>)}</ul></div> : null}</WorkspaceSection>;
}

function CourseTable({ courses, copy, locale, instructor = false }: { courses: NonNullable<AdminUser360["student"]>["courses"] | NonNullable<AdminUser360["instructor"]>["owned_courses"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; instructor?: boolean }) {
  return <div className="overflow-x-auto rounded-lg border border-border"><table className="min-w-full text-sm"><thead className="bg-muted/50"><tr><th className="px-4 py-3 text-start">{copy.course}</th><th className="px-4 py-3 text-start">{copy.lifecycle}</th><th className="px-4 py-3 text-start">{instructor ? copy.enrollments : copy.progress}</th>{!instructor ? <th className="px-4 py-3 text-start">{copy.lastWatched}</th> : null}</tr></thead><tbody>{courses.map((course) => <tr key={course.id} className="border-t border-border"><td className="px-4 py-3 font-semibold"><bdi>{course.title}</bdi></td><td className="px-4 py-3">{labelFor(copy.lifecycleStates, course.lifecycle, copy.unknownValue)}</td><td className="px-4 py-3">{instructor ? course.enrollments_count ?? 0 : (course.completed_lessons ?? 0) + "/" + (course.total_lessons ?? 0) + " · " + Math.round(course.progress_percent ?? 0) + "%"}</td>{!instructor ? <td className="px-4 py-3">{course.last_watched_at ? formatDateTime(course.last_watched_at, locale) : copy.noActivity}</td> : null}</tr>)}</tbody></table></div>;
}

function safeReference(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim() ?? "";
  return trimmed && !UUID_LIKE.test(trimmed) ? trimmed : fallback;
}

function operatorReasonText(value: string, copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]): string {
  const trimmed = value.trim();
  return trimmed && !UUID_LIKE.test(trimmed) ? trimmed : copy.notAvailable;
}

function factValueLabel(fact: AccessDiagnostic["facts"][number], copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"], locale: "ar" | "en"): string {
  const value = fact.value ?? "";
  switch (fact.code) {
    case "ACCOUNT_STATUS": return labelFor(copy.status, value, copy.unknownValue);
    case "COURSE_LIFECYCLE": return labelFor(copy.lifecycleStates, value, copy.unknownValue);
    case "EMAIL_VERIFIED": return value === "true" ? copy.verified : value === "false" ? copy.notVerified : copy.unknownValue;
    case "EVALUATOR_REASON":
    case "COURSE_PUBLICATION": return accessReasonMessage(value, copy.reasonCodes);
    case "COURSE_ACCESS_SUSPENDED": return value === "true" ? copy.yes : value === "false" ? copy.no : copy.unknownValue;
    case "COURSE_RETIRED": return Number.isNaN(Date.parse(value)) ? copy.notAvailable : formatDateTime(value, locale);
    case "ENTITLEMENT_COUNT": return value || copy.notAvailable;
    default: return copy.unknownValue;
  }
}

function toUserInvitation(invitation: CourseAccessInvitation, courseTitle: string): UserInvitation {
  return {
    id: invitation.id,
    course_id: invitation.course_id,
    course_title: courseTitle,
    state: invitation.state,
    created_at: invitation.created_at,
    decided_at: invitation.decided_at,
    accepted_at: invitation.accepted_at,
    external_reference: invitation.external_reference ?? undefined,
  };
}

function updateEntitlement(updateStudent: (update: (student: AdminStudent) => AdminStudent) => void, updated: { id: string; access_ends_at: string; revoked_at?: string | null; state: string; revision: number; adjustments: AdminStudent["entitlements"][number]["adjustments"] }): void {
  updateStudent((student) => ({ ...student, entitlements: student.entitlements.map((current) => current.id === updated.id ? { ...current, access_ends_at: updated.access_ends_at, revoked_at: updated.revoked_at, state: updated.state, revision: updated.revision, adjustments: updated.adjustments } : current) }));
}

function Facts({ values }: { values: [string, string][] }) { return <dl className="grid gap-4 rounded-lg border border-border bg-card p-5 sm:grid-cols-2 lg:grid-cols-3">{values.map(([label, value]) => <div key={label}><dt className="text-sm text-muted-foreground">{label}</dt><dd className="mt-1 font-semibold"><bdi>{value}</bdi></dd></div>)}</dl>; }

function ActionBar({ account, copy, onAction }: { account: AdminUser360["identity"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; onAction: (kind: Action) => void }) { return <div className="flex flex-wrap gap-2"><Button size="sm" variant={account.status === "SUSPENDED" ? "outline" : "destructive"} onClick={() => onAction(account.status === "SUSPENDED" ? "restore" : "suspend")}>{account.status === "SUSPENDED" ? copy.restore : copy.suspend}</Button><Button size="sm" variant="outline" onClick={() => onAction("signout")}>{copy.signOut}</Button>{account.role === "STUDENT" ? <Button size="sm" variant="accent" onClick={() => onAction("grant")}>{copy.grantAccess}</Button> : null}</div>; }

function requiresReason(kind: Action): boolean {
  return kind !== "device" && kind !== "devices" && kind !== "cooldown";
}

function ActionDialog({ pending, copy, courseOptions, reason, expiryDate, selectedCourse, onCourseChange, onReason, onExpiryDate, onClose, busy, onConfirm }: { pending: PendingAction; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; courseOptions: AdminCourseOption[]; reason: string; expiryDate: string; selectedCourse: string; onCourseChange: (value: string) => void; onReason: (value: string) => void; onExpiryDate: (value: string) => void; onClose: () => void; busy: boolean; onConfirm: () => void }) {
  const needsReason = requiresReason(pending.kind);
  return <ConfirmDialog open onOpenChange={(open) => { if (!open) onClose(); }} title={copy.actionTitles[pending.kind]} body={copy.actionBodies[pending.kind]} confirmLabel={copy.confirm} cancelLabel={copy.cancel} busy={busy} confirmDisabled={(needsReason && !reason.trim()) || (pending.kind === "grant" && !selectedCourse) || (pending.kind === "expiry" && !expiryDate)} onConfirm={onConfirm} testID="admin-user-action-dialog"><div className="space-y-3">{pending.kind === "grant" ? <Field label={copy.courseIdLabel} htmlFor="admin-user-action-course"><Select id="admin-user-action-course" value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.courseIdPlaceholder}</option>{courseOptions.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select></Field> : null}{needsReason ? <Field label={copy.reasonLabel} htmlFor="admin-user-action-reason"><Input id="admin-user-action-reason" value={reason} onChange={(event) => onReason(event.target.value)} autoComplete="off" /></Field> : <p className="text-sm text-muted-foreground">{copy.reasonNotRecorded}</p>}{pending.kind === "expiry" ? <Field label={copy.newExpiry} htmlFor="admin-user-action-expiry"><Input id="admin-user-action-expiry" type="date" value={expiryDate} onChange={(event) => onExpiryDate(event.target.value)} /></Field> : null}</div></ConfirmDialog>;
}

function IdentityFact({ label, value, direction }: { label: string; value: string; direction?: "ltr" }) { return <div><dt className="text-sm font-semibold text-muted-foreground">{label}</dt><dd className="mt-1 font-medium text-foreground" dir={direction}><bdi>{value}</bdi></dd></div>; }

function labelFor(labels: Record<string, string>, value: string | undefined, fallback?: string): string {
  if (!value) return fallback ?? labels.UNKNOWN ?? "";
  return labels[value] ?? fallback ?? labels.UNKNOWN ?? value;
}
