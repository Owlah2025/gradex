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
  type AdminNote,
  type AdminUser360,
  type UserEntitlement,
} from "@/lib/api/admin-operations";
import { adjustEntitlementExpiry, createCourseAccessInvitation, revokeEntitlement } from "@/lib/api/access";
import { suspendStaffAccount, reinstateStaffAccount } from "@/lib/api/identity";
import {
  resetAdminDeviceCooldown,
  revokeAdminDevice,
  revokeAllAdminDevices,
} from "@/lib/api/devices";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
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
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

type Tab = "overview" | "access" | "progress" | "security" | "notes" | "audit" | "diagnostic";
type Action = "suspend" | "restore" | "signout" | "grant" | "revoke" | "expiry" | "device" | "devices" | "cooldown";
type PendingAction = { kind: Action; entitlement?: UserEntitlement; deviceID?: string };
const tabs: Tab[] = ["overview", "access", "progress", "security", "notes", "audit", "diagnostic"];

export function AdminUserDetail({ accountID }: { accountID: string }) {
  const { locale, t } = useLocale();
  const copy = t.adminUserDetail;
  const [model, setModel] = React.useState<AdminUser360 | null>(null);
  const [state, setState] = React.useState<"loading" | "ready" | "failed">("loading");
  const [error, setError] = React.useState<string | null>(null);
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
    try { setModel(await getAdminUser360(accountID, locale)); setState("ready"); }
    catch (cause) { setError(describeApiError(cause, locale)); setState("failed"); }
  }, [accountID, locale]);

  React.useEffect(() => { void load(); }, [load]);
  React.useEffect(() => {
    if ((activeTab !== "diagnostic" && activeTab !== "access") || !model) return;
    let active = true;
    void listAdminCourseOptions(accountID, locale).then((result) => { if (active) setCourseOptions(result.courses); }).catch(() => { if (active) setCourseOptions([]); });
    return () => { active = false; };
  }, [accountID, activeTab, locale, model]);

  const runAction = async () => {
    if (!pending || !model || busy) return;
    const csrf = currentCSRFToken() ?? undefined;
    setBusy(true); setNotice(null);
    try {
      if (pending.kind === "suspend") await suspendStaffAccount(accountID, reason.trim(), locale, csrf);
      if (pending.kind === "restore") await reinstateStaffAccount(accountID, reason.trim(), locale, csrf);
      if (pending.kind === "signout") await revokeAccountSessions(accountID, reason.trim(), locale, csrf);
      if (pending.kind === "grant") await createCourseAccessInvitation(selectedCourse, model.identity.email, reason.trim(), undefined, locale, csrf);
      if (pending.kind === "expiry" && pending.entitlement) await adjustEntitlementExpiry(pending.entitlement.id, expiryDate, reason.trim(), { expectedRevision: pending.entitlement.revision }, locale, csrf);
      if (pending.kind === "revoke" && pending.entitlement) await revokeEntitlement(pending.entitlement.id, reason.trim(), { expectedRevision: pending.entitlement.revision }, locale, csrf);
      if (pending.kind === "device" && pending.deviceID) await revokeAdminDevice(accountID, pending.deviceID, locale, csrf ?? "");
      if (pending.kind === "devices") await revokeAllAdminDevices(accountID, locale, csrf ?? "");
      if (pending.kind === "cooldown") await resetAdminDeviceCooldown(accountID, locale, csrf ?? "");
      setNotice(copy.completed[pending.kind]); setPending(null); setReason(""); setExpiryDate(""); await load();
    } catch (cause) { setNotice(isRecentAuthRequired(cause) ? copy.recentAuth : describeApiError(cause, locale)); }
    finally { setBusy(false); }
  };

  if (state === "loading") return <WorkspacePage testID="admin-user-detail-page"><LoadingState label={copy.loading} testID="admin-user-detail-loading" /></WorkspacePage>;
  if (state === "failed" || !model) return <WorkspacePage testID="admin-user-detail-page"><ErrorState title={copy.loadFailed} detail={error} retryLabel={copy.retry} onRetry={() => void load()} testID="admin-user-detail-error" /></WorkspacePage>;

  const account = model.identity;
  const student = model.student;
  const instructor = model.instructor;
  const visibleTabs: Tab[] = account.role === "STUDENT" ? tabs : account.role === "INSTRUCTOR" ? ["overview", "notes", "audit"] : ["overview"];
  return <WorkspacePage testID="admin-user-detail-page">
    <WorkspacePageHeader title={account.display_name} description={copy.description} breadcrumb={<Button asChild variant="ghost" size="sm"><Link href={`/${locale}/admin/users`}>{copy.backToUsers}</Link></Button>} status={<><StatusBadge tone={account.status === "ACTIVE" ? "success" : account.status === "SUSPENDED" ? "accent" : "neutral"} label={copy.status[account.status]} detail={copy.roles[account.role]} /><span className="text-sm text-muted-foreground"><bdi>{account.email}</bdi></span></>} actions={<ActionBar account={account} copy={copy} onAction={(kind) => { setReason(""); if (kind === "grant") setActiveTab("access"); setPending({ kind }); }} />} />
    {notice ? <div className="mt-4"><Alert tone="info" title={notice}>{notice === copy.recentAuth ? <Link className="underline" href={`/${locale}/login?returnTo=${encodeURIComponent(`/${locale}/admin/users/${accountID}`)}`}>{copy.signInAgain}</Link> : undefined}</Alert></div> : null}
    <nav className="mt-6" aria-label={copy.sectionsLabel}>
      <label className="sr-only" htmlFor="admin-user-tab-select">{copy.sectionsLabel}</label>
      <Select id="admin-user-tab-select" className="md:hidden" value={activeTab} onChange={(event) => setActiveTab(event.target.value as Tab)}>{visibleTabs.map((tab) => <option key={tab} value={tab}>{copy.tabs[tab]}</option>)}</Select>
      <div className="hidden flex-wrap gap-2 md:flex" role="tablist">{visibleTabs.map((tab) => <button key={tab} type="button" role="tab" aria-selected={activeTab === tab} onClick={() => setActiveTab(tab)} className={`rounded-pill border px-4 py-2 text-sm font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring ${activeTab === tab ? "border-primary bg-primary text-primary-foreground" : "border-border bg-card text-foreground hover:bg-accent"}`}>{copy.tabs[tab]}</button>)}</div>
    </nav>
    {activeTab === "overview" ? <Overview account={account} student={student} instructor={instructor} copy={copy} locale={locale} /> : null}
    {activeTab === "access" && student ? <AccessPanel student={student} copy={copy} locale={locale} options={courseOptions} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} onGrant={() => setPending({ kind: "grant" })} onAction={(next) => { setReason(""); setExpiryDate(""); setPending(next); }} /> : null}
    {activeTab === "progress" && student ? <ProgressPanel student={student} copy={copy} /> : null}
    {activeTab === "security" && student ? <SecurityPanel accountID={accountID} student={student} copy={copy} locale={locale} onAction={(next) => { setReason(""); setPending(next); }} /> : null}
    {activeTab === "notes" ? <NotesPanel accountID={accountID} notes={student?.notes ?? instructor?.notes ?? []} copy={copy} locale={locale} onAdded={() => void load()} /> : null}
    {activeTab === "audit" ? <AuditPanel events={student?.audit_events ?? instructor?.audit_events ?? []} copy={copy} locale={locale} /> : null}
    {activeTab === "diagnostic" && student ? <DiagnosticPanel copy={copy} options={courseOptions} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} diagnostic={diagnostic} state={diagnosticState} onDiagnose={async () => { if (!selectedCourse) return; setDiagnosticState("loading"); try { setDiagnostic(await diagnoseAdminAccess(accountID, selectedCourse, locale)); setDiagnosticState("idle"); } catch { setDiagnosticState("failed"); } }} /> : null}
    {pending ? <ActionDialog pending={pending} copy={copy} courseOptions={courseOptions} reason={reason} expiryDate={expiryDate} selectedCourse={selectedCourse} onCourseChange={setSelectedCourse} onReason={setReason} onExpiryDate={setExpiryDate} onClose={() => { if (!busy) setPending(null); }} busy={busy} onConfirm={() => void runAction()} /> : null}
  </WorkspacePage>;
}

function Overview({ account, student, instructor, copy, locale }: { account: AdminUser360["identity"]; student?: AdminUser360["student"]; instructor?: AdminUser360["instructor"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) {
  return <>
    <WorkspaceSection title={copy.identitySection} description={copy.identityDescription}><div className="rounded-lg border border-border bg-card p-5"><dl className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3"><IdentityFact label={copy.name} value={account.display_name} /><IdentityFact label={copy.email} value={account.email} direction="ltr" /><IdentityFact label={copy.role} value={copy.roles[account.role]} /><IdentityFact label={copy.statusLabel} value={copy.status[account.status]} /><IdentityFact label={copy.locale} value={account.locale === "ar" ? copy.arabic : copy.english} /><IdentityFact label={copy.emailVerified} value={account.email_verified ? copy.verified : copy.notVerified} /><IdentityFact label={copy.joined} value={formatDate(account.created_at, locale)} /><IdentityFact label={copy.lastActivity} value={account.last_sign_in_activity_at ? formatDateTime(account.last_sign_in_activity_at, locale) : copy.noActivity} /><IdentityFact label={copy.learningActivity} value={account.last_learning_activity_at ? formatDateTime(account.last_learning_activity_at, locale) : copy.noActivity} /></dl></div></WorkspaceSection>
    {student?.academic_profile ? <WorkspaceSection title={copy.academicProfile} description={copy.academicDescription}><Facts values={[[copy.institution, student.academic_profile.institution_label || copy.notAvailable], [copy.academicUnit, student.academic_profile.academic_unit_label || copy.notAvailable], [copy.program, student.academic_profile.program_label || copy.notAvailable], [copy.curriculum, student.academic_profile.curriculum_label || copy.notAvailable], [copy.enrollmentStatus, student.academic_profile.enrollment_status || copy.notAvailable], [copy.level, student.academic_profile.current_level ? String(student.academic_profile.current_level) : copy.notAvailable]]} /></WorkspaceSection> : null}
    {instructor ? <WorkspaceSection title={copy.ownedCourses} description={copy.instructorDescription}><CourseTable courses={instructor.owned_courses} copy={copy} instructor /></WorkspaceSection> : null}
  </>;
}

function AccessPanel({ student, copy, locale, options, selectedCourse, onCourseChange, onGrant, onAction }: { student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; options: AdminCourseOption[]; selectedCourse: string; onCourseChange: (value: string) => void; onGrant: () => void; onAction: (action: PendingAction) => void }) {
  return <WorkspaceSection title={copy.accessTitle} description={copy.accessDescription} actions={<div className="flex flex-wrap gap-2"><Select aria-label={copy.courseIdLabel} value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.courseIdPlaceholder}</option>{options.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select><Button size="sm" onClick={onGrant} disabled={!selectedCourse}>{copy.grantAccess}</Button></div>}>{student.entitlements.length === 0 ? <EmptyState density="compact" title={copy.noEntitlements} description={copy.noEntitlementsDescription} /> : <div className="space-y-3">{student.entitlements.map((entitlement) => <article key={entitlement.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="font-display font-bold"><bdi>{entitlement.course_title}</bdi></h3><p className="mt-1 text-sm text-muted-foreground">{provenanceLabel(entitlement.grant_source, copy.provenance)} · {entitlement.source_reference_label || copy.notAvailable}</p><p className="text-sm text-muted-foreground">{copy.grantedBy}: {entitlement.granted_by_display_name || copy.notAvailable}</p></div><StatusBadge tone={entitlement.state === "ACTIVE" ? "success" : entitlement.state === "REVOKED" ? "accent" : "neutral"} label={labelFor(copy.entitlementStates, entitlement.state)} /></div><dl className="mt-3 grid gap-2 text-sm sm:grid-cols-3"><div><dt className="text-muted-foreground">{copy.scope}</dt><dd>{entitlement.scope_kind}</dd></div><div><dt className="text-muted-foreground">{copy.accessEnds}</dt><dd>{formatDateTime(entitlement.access_ends_at, locale)}</dd></div><div><dt className="text-muted-foreground">{copy.originalAccessEnds}</dt><dd>{formatDateTime(entitlement.original_access_ends_at, locale)}</dd></div></dl><div className="mt-3 flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => onAction({ kind: "expiry", entitlement })}>{copy.adjustExpiry}</Button><Button size="sm" variant="destructive" onClick={() => onAction({ kind: "revoke", entitlement })}>{copy.revokeAccess}</Button></div>{entitlement.adjustments.length ? <ul className="mt-3 border-t border-border pt-3 text-sm text-muted-foreground">{entitlement.adjustments.map((adjustment) => <li key={adjustment.id}>{formatDateTime(adjustment.adjusted_at, locale)} · {adjustment.reason}</li>)}</ul> : null}</article>)}</div>}</WorkspaceSection>;
}

function ProgressPanel({ student, copy }: { student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"] }) { return <WorkspaceSection title={copy.progressTitle} description={copy.progressDescription}><CourseTable courses={student.courses} copy={copy} /></WorkspaceSection>; }

function SecurityPanel({ accountID, student, copy, locale, onAction }: { accountID: string; student: NonNullable<AdminUser360["student"]>; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; onAction: (action: PendingAction) => void }) {
  const [events, setEvents] = React.useState(student.security_events);
  const [page, setPage] = React.useState(1);
  const [hasMore, setHasMore] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [moreError, setMoreError] = React.useState(false);
  React.useEffect(() => { setEvents(student.security_events); setPage(1); setHasMore(true); }, [student.security_events]);
  const loadMore = async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    setMoreError(false);
    try {
      const next = await listAdminSecurityEvents(accountID, locale, page + 1, 20);
      setEvents((current) => [...current, ...next.events]);
      setPage(next.page);
      setHasMore(next.has_more && next.events.length > 0);
    } catch { setMoreError(true); } finally { setLoadingMore(false); }
  };
  return <WorkspaceSection title={copy.securityTitle} description={copy.securityDescription} actions={<div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => onAction({ kind: "devices" })}>{copy.revokeAllDevices}</Button><Button size="sm" variant="outline" onClick={() => onAction({ kind: "cooldown" })}>{copy.resetCooldown}</Button></div>}><div className="space-y-3">{student.devices.devices.map((device) => <div key={device.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-4"><div><p className="font-semibold">{device.label}</p><p className="text-sm text-muted-foreground">{device.browser_family} · {device.platform_family}</p></div><Button size="sm" variant="destructive" onClick={() => onAction({ kind: "device", deviceID: device.id })}>{copy.revokeDevice}</Button></div>)}</div><div className="mt-6 space-y-2">{events.map((event) => <div key={event.id} className="flex flex-wrap justify-between gap-3 border-b border-border py-3 text-sm"><span className="font-semibold">{labelFor(copy.securityEvents, event.event_type)}</span><time className="text-muted-foreground" dateTime={event.occurred_at}>{formatDateTime(event.occurred_at, locale)}</time></div>)}</div>{moreError ? <div className="mt-4"><Alert tone="error" title={copy.securityLoadFailed} /></div> : null}{hasMore ? <div className="mt-4"><Button size="sm" variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>{copy.loadMoreSecurity}</Button></div> : null}</WorkspaceSection>;
}

function NotesPanel({ accountID, notes, copy, locale, onAdded }: { accountID: string; notes: AdminNote[]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en"; onAdded: () => void }) { const [body, setBody] = React.useState(""); const [busy, setBusy] = React.useState(false); const add = async () => { if (!body.trim() || busy) return; setBusy(true); try { await addAdminNote(accountID, body, locale); setBody(""); onAdded(); } finally { setBusy(false); } }; return <WorkspaceSection title={copy.notesTitle} description={copy.notesDescription}><div className="rounded-lg border border-border bg-card p-4"><Field label={copy.noteBody} htmlFor="admin-note-body"><textarea id="admin-note-body" className="min-h-28 w-full rounded-md border border-input bg-background p-3 text-sm focus-visible:outline focus-visible:ring-2 focus-visible:ring-ring" maxLength={4000} value={body} onChange={(event) => setBody(event.target.value)} /></Field><div className="mt-3 flex justify-end"><Button size="sm" onClick={() => void add()} disabled={busy || !body.trim()}>{copy.addNote}</Button></div></div><div className="mt-4 space-y-3">{notes.map((note) => <article key={note.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap justify-between gap-2 text-sm"><span className="font-semibold">{note.author_name}</span><time className="text-muted-foreground" dateTime={note.created_at}>{formatDateTime(note.created_at, locale)}</time></div><p className="mt-3 whitespace-pre-wrap text-sm leading-6"><bdi>{note.body}</bdi></p></article>)}</div></WorkspaceSection>; }

function AuditPanel({ events, copy, locale }: { events: NonNullable<AdminUser360["student"]>["audit_events"] | NonNullable<AdminUser360["instructor"]>["audit_events"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; locale: "ar" | "en" }) { return <WorkspaceSection title={copy.auditTitle} description={copy.auditDescription}>{events.length === 0 ? <EmptyState density="compact" title={copy.noAudit} description={copy.noAuditDescription} /> : <div className="space-y-2">{events.map((event) => <div key={event.id} className="rounded-lg border border-border bg-card p-4"><div className="flex flex-wrap justify-between gap-2"><span className="font-semibold">{labelFor(copy.auditActions, event.action)}</span><time className="text-sm text-muted-foreground" dateTime={event.occurred_at}>{formatDateTime(event.occurred_at, locale)}</time></div><p className="mt-2 text-sm text-muted-foreground"><bdi>{event.reason}</bdi></p></div>)}</div>}</WorkspaceSection>; }

function DiagnosticPanel({ copy, options, selectedCourse, onCourseChange, diagnostic, state, onDiagnose }: { copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; options: AdminCourseOption[]; selectedCourse: string; onCourseChange: (value: string) => void; diagnostic: AccessDiagnostic | null; state: "idle" | "loading" | "failed"; onDiagnose: () => void }) { return <WorkspaceSection title={copy.diagnosticTitle} description={copy.diagnosticDescription}><div className="flex flex-wrap items-end gap-2"><Field label={copy.coursePicker} htmlFor="diagnostic-course"><Select id="diagnostic-course" value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.chooseCourse}</option>{options.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select></Field><Button onClick={onDiagnose} disabled={!selectedCourse || state === "loading"}>{copy.diagnose}</Button></div>{state === "failed" ? <div className="mt-4"><Alert tone="error" title={copy.diagnosticFailed} /></div> : null}{diagnostic ? <div className="mt-5 rounded-lg border border-border bg-card p-5"><StatusBadge tone={diagnostic.allowed ? "success" : "accent"} label={accessReasonMessage(diagnostic.primary_reason_code, copy.reasonCodes)} /><h3 className="mt-3 font-display text-lg font-bold"><bdi>{diagnostic.course_title}</bdi></h3><ul className="mt-4 space-y-2 text-sm">{diagnostic.facts.map((fact) => <li key={`${fact.code}-${fact.value}`} className="flex flex-wrap gap-2"><span className="font-semibold">{labelFor(copy.factLabels, fact.code)}</span><span className="text-muted-foreground">{fact.value}</span></li>)}</ul></div> : null}</WorkspaceSection>; }

function CourseTable({ courses, copy, instructor = false }: { courses: NonNullable<AdminUser360["student"]>["courses"] | NonNullable<AdminUser360["instructor"]>["owned_courses"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; instructor?: boolean }) { return <div className="overflow-x-auto rounded-lg border border-border"><table className="min-w-full text-sm"><thead className="bg-muted/50"><tr><th className="px-4 py-3 text-start">{copy.course}</th><th className="px-4 py-3 text-start">{copy.lifecycle}</th><th className="px-4 py-3 text-start">{instructor ? copy.enrollments : copy.progress}</th></tr></thead><tbody>{courses.map((course) => <tr key={course.id} className="border-t border-border"><td className="px-4 py-3 font-semibold"><bdi>{course.title}</bdi></td><td className="px-4 py-3">{labelFor(copy.lifecycleStates, course.lifecycle)}</td><td className="px-4 py-3">{instructor ? course.enrollments_count ?? 0 : `${course.completed_lessons ?? 0}/${course.total_lessons ?? 0} · ${Math.round(course.progress_percent ?? 0)}%`}</td></tr>)}</tbody></table></div>; }

function Facts({ values }: { values: [string, string][] }) { return <dl className="grid gap-4 rounded-lg border border-border bg-card p-5 sm:grid-cols-2 lg:grid-cols-3">{values.map(([label, value]) => <div key={label}><dt className="text-sm text-muted-foreground">{label}</dt><dd className="mt-1 font-semibold"><bdi>{value}</bdi></dd></div>)}</dl>; }

function ActionBar({ account, copy, onAction }: { account: AdminUser360["identity"]; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; onAction: (kind: Action) => void }) { return <div className="flex flex-wrap gap-2"><Button size="sm" variant={account.status === "SUSPENDED" ? "outline" : "destructive"} onClick={() => onAction(account.status === "SUSPENDED" ? "restore" : "suspend")}>{account.status === "SUSPENDED" ? copy.restore : copy.suspend}</Button><Button size="sm" variant="outline" onClick={() => onAction("signout")}>{copy.signOut}</Button>{account.role === "STUDENT" ? <Button size="sm" variant="accent" onClick={() => onAction("grant")}>{copy.grantAccess}</Button> : null}</div>; }

function ActionDialog({ pending, copy, courseOptions, reason, expiryDate, selectedCourse, onCourseChange, onReason, onExpiryDate, onClose, busy, onConfirm }: { pending: PendingAction; copy: ReturnType<typeof useLocale>["t"]["adminUserDetail"]; courseOptions: AdminCourseOption[]; reason: string; expiryDate: string; selectedCourse: string; onCourseChange: (value: string) => void; onReason: (value: string) => void; onExpiryDate: (value: string) => void; onClose: () => void; busy: boolean; onConfirm: () => void }) { return <ConfirmDialog open onOpenChange={(open) => { if (!open) onClose(); }} title={copy.actionTitles[pending.kind]} body={copy.actionBodies[pending.kind]} confirmLabel={copy.confirm} cancelLabel={copy.cancel} busy={busy} confirmDisabled={!reason.trim() || (pending.kind === "grant" && !selectedCourse) || (pending.kind === "expiry" && !expiryDate)} onConfirm={onConfirm} testID="admin-user-action-dialog"><div className="space-y-3">{pending.kind === "grant" ? <Field label={copy.courseIdLabel} htmlFor="admin-user-action-course"><Select id="admin-user-action-course" value={selectedCourse} onChange={(event) => onCourseChange(event.target.value)}><option value="">{copy.courseIdPlaceholder}</option>{courseOptions.map((course) => <option key={course.id} value={course.id}>{course.title}</option>)}</Select></Field> : null}<Field label={copy.reasonLabel} htmlFor="admin-user-action-reason"><Input id="admin-user-action-reason" value={reason} onChange={(event) => onReason(event.target.value)} autoComplete="off" /></Field>{pending.kind === "expiry" ? <Field label={copy.newExpiry} htmlFor="admin-user-action-expiry"><Input id="admin-user-action-expiry" type="date" value={expiryDate} onChange={(event) => onExpiryDate(event.target.value)} /></Field> : null}</div></ConfirmDialog>; }

function IdentityFact({ label, value, direction }: { label: string; value: string; direction?: "ltr" }) { return <div><dt className="text-sm font-semibold text-muted-foreground">{label}</dt><dd className="mt-1 font-medium text-foreground" dir={direction}><bdi>{value}</bdi></dd></div>; }

function labelFor(labels: Record<string, string>, value: string): string { return labels[value] ?? value; }
