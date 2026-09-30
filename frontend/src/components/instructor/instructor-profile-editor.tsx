"use client";

import { useEffect, useMemo, useState } from "react";
import {
  listAuthoringInstitutions,
  searchAuthoringSubjects,
  subjectLabel,
  type AuthoringSubject,
  type InstitutionOption,
} from "@/lib/api/authoring-academic";
import {
  getInstructorProfile,
  saveInstructorProfile,
  submitInstructorProfile,
  type InstructorExpertise,
  type InstructorProfile,
} from "@/lib/api/instructor-profile";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { instructorInitials } from "@/components/catalog/course-detail-presentation";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";
import { Alert } from "@/components/ui/alert";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

type Copy = ReturnType<typeof useLocale>["t"]["instructor"]["profile"];

type Draft = {
  publicSlug: string;
  headlineAr: string;
  headlineEn: string;
  bioAr: string;
  bioEn: string;
  expertise: InstructorExpertise[];
};

function draftFromProfile(profile: InstructorProfile): Draft {
  return {
    publicSlug: profile.public_slug ?? "",
    headlineAr: profile.headline_ar,
    headlineEn: profile.headline_en,
    bioAr: profile.bio_ar,
    bioEn: profile.bio_en,
    expertise: profile.expertise,
  };
}

function stateTone(state: InstructorProfile["publication_state"]) {
  if (state === "PUBLISHED") return "success" as const;
  if (state === "CHANGES_REQUESTED" || state === "HIDDEN") return "accent" as const;
  return "default" as const;
}

export function InstructorProfileEditor() {
  const { locale, t } = useLocale();
  const copy = t.instructor.profile;
  const [profile, setProfile] = useState<InstructorProfile | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [institutions, setInstitutions] = useState<InstitutionOption[]>([]);
  const [institutionID, setInstitutionID] = useState("");
  const [subjectQuery, setSubjectQuery] = useState("");
  const [subjectResults, setSubjectResults] = useState<AuthoringSubject[]>([]);
  const [busy, setBusy] = useState(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    Promise.all([getInstructorProfile(locale), listAuthoringInstitutions(locale)])
      .then(([nextProfile, nextInstitutions]) => {
        if (cancelled) return;
        setProfile(nextProfile);
        setDraft(draftFromProfile(nextProfile));
        setInstitutions(nextInstitutions);
        if (nextInstitutions.length === 1) setInstitutionID(nextInstitutions[0].id);
        setError(null);
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
  }, [copy.loadFailed, locale]);

  useEffect(() => {
    if (!institutionID || subjectQuery.trim() === "") {
      setSubjectResults([]);
      return;
    }
    let cancelled = false;
    searchAuthoringSubjects({ institutionID, query: subjectQuery, locale })
      .then((results) => {
        if (!cancelled) setSubjectResults(results);
      })
      .catch(() => {
        if (!cancelled) setSubjectResults([]);
      });
    return () => {
      cancelled = true;
    };
  }, [institutionID, locale, subjectQuery]);

  const readiness = useMemo(() => {
    if (!draft) return { slug: false, headline: false, bio: false, ready: false };
    const slug = /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(draft.publicSlug) && draft.publicSlug.length >= 3 && draft.publicSlug.length <= 60;
    const headline = draft.headlineAr.trim() !== "" || draft.headlineEn.trim() !== "";
    const bio = draft.bioAr.trim() !== "" || draft.bioEn.trim() !== "";
    return { slug, headline, bio, ready: slug && headline && bio };
  }, [draft]);
  const editingLocked = profile?.publication_state === "PENDING_REVIEW";

  const updateDraft = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    setDraft((current) => (current ? { ...current, [key]: value } : current));
    setStatusMessage(null);
  };

  const toggleExpertise = (subject: AuthoringSubject) => {
    if (!draft) return;
    const exists = draft.expertise.some((item) => item.id === subject.id);
    const expertise = exists
      ? draft.expertise.filter((item) => item.id !== subject.id)
      : [
          ...draft.expertise,
          {
            id: subject.id,
            official_code: subject.official_code,
            title_ar: subject.title_ar,
            title_en: subject.title_en,
          },
        ];
    updateDraft("expertise", expertise);
    setSubjectQuery("");
    setSubjectResults([]);
  };

  const persist = async (): Promise<InstructorProfile | null> => {
    if (!profile || !draft || editingLocked) return null;
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(copy.sessionEnded);
      return null;
    }
    setBusy(true);
    setError(null);
    setStatusMessage(null);
    try {
      const saved = await saveInstructorProfile({
        locale,
        csrf,
        revision: profile.revision,
        publicSlug: draft.publicSlug,
        headlineAr: draft.headlineAr,
        headlineEn: draft.headlineEn,
        bioAr: draft.bioAr,
        bioEn: draft.bioEn,
        expertiseIDs: draft.expertise.map((item) => item.id),
      });
      setProfile(saved);
      setDraft(draftFromProfile(saved));
      setStatusMessage(copy.saved);
      return saved;
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
      return null;
    } finally {
      setBusy(false);
    }
  };

  const submit = async () => {
    if (!readiness.ready || !profile || busy || editingLocked) return;
    const saved = await persist();
    if (!saved) return;
    const csrf = currentCSRFToken();
    if (!csrf) return;
    setBusy(true);
    setError(null);
    try {
      const submitted = await submitInstructorProfile({ locale, csrf, revision: saved.revision });
      setProfile(submitted);
      setDraft(draftFromProfile(submitted));
      setStatusMessage(copy.submitted);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <LoadingState label={copy.loading} />;
  if (error && !profile) {
    return <ErrorState title={copy.loadFailed} detail={error} retryLabel={copy.retry} onRetry={() => window.location.reload()} />;
  }
  if (!profile || !draft) return null;

  const displayHeadline = locale === "ar"
    ? draft.headlineAr || draft.headlineEn
    : draft.headlineEn || draft.headlineAr;
  const displayBio = locale === "ar" ? draft.bioAr || draft.bioEn : draft.bioEn || draft.bioAr;
  const stateLabel = copy.stateLabels[profile.publication_state];

  return (
    <WorkspacePage testID="instructor-profile-editor">
      <WorkspacePageHeader
        title={copy.title}
        description={copy.description}
        status={
          <StatusBadge
            tone={stateTone(profile.publication_state)}
            label={stateLabel}
            detail={profile.publication_state === "PUBLISHED" ? copy.previewBody : undefined}
          />
        }
        actions={
          <Button type="button" onClick={() => void persist()} disabled={busy || editingLocked}>
            {busy ? copy.saving : copy.save}
          </Button>
        }
      />

      {error ? <div className="mt-6"><Alert tone="error" title={copy.loadFailed}>{error}</Alert></div> : null}
      {statusMessage ? <div className="mt-6"><Alert tone="success" title={statusMessage} /></div> : null}
      {editingLocked ? <div className="mt-6"><Alert tone="info" title={copy.pendingReviewTitle}>{copy.pendingReviewBody}</Alert></div> : null}
      {profile.decision_note && profile.publication_state === "CHANGES_REQUESTED" ? (
        <div className="mt-6"><Alert tone="info" title={copy.feedbackTitle}>{profile.decision_note}</Alert></div>
      ) : null}

      <form onSubmit={(event) => { event.preventDefault(); void submit(); }}>
        <WorkspaceSection title={copy.slug} description={copy.slugHint}>
          <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
            <Field label={copy.slug} htmlFor="instructor-profile-slug">
              <Input
                id="instructor-profile-slug"
                value={draft.publicSlug}
                onChange={(event) => updateDraft("publicSlug", event.target.value)}
                placeholder="your-name"
                maxLength={60}
                disabled={busy || editingLocked}
              />
            </Field>
            <div className="rounded-lg border border-border bg-muted/30 p-4">
              <p className="text-xs font-semibold text-muted-foreground">{copy.publicURL}</p>
              <p className="mt-2 break-all font-mono text-sm text-foreground">
                /{locale}/instructors/{draft.publicSlug || "your-slug"}
              </p>
            </div>
          </div>
        </WorkspaceSection>

        <WorkspaceSection title={copy.headlineTitle}>
          <div className="grid gap-4 md:grid-cols-2">
            <Field label={copy.headlineAr} htmlFor="instructor-profile-headline-ar">
              <Input id="instructor-profile-headline-ar" value={draft.headlineAr} maxLength={120} onChange={(event) => updateDraft("headlineAr", event.target.value)} dir="rtl" disabled={busy || editingLocked} />
            </Field>
            <Field label={copy.headlineEn} htmlFor="instructor-profile-headline-en">
              <Input id="instructor-profile-headline-en" value={draft.headlineEn} maxLength={120} onChange={(event) => updateDraft("headlineEn", event.target.value)} dir="ltr" disabled={busy || editingLocked} />
            </Field>
          </div>
        </WorkspaceSection>

        <WorkspaceSection title={copy.bioTitle}>
          <div className="grid gap-4 md:grid-cols-2">
            <Field label={copy.bioAr} htmlFor="instructor-profile-bio-ar">
              <Textarea id="instructor-profile-bio-ar" value={draft.bioAr} maxLength={4000} onChange={(event) => updateDraft("bioAr", event.target.value)} dir="rtl" rows={8} disabled={busy || editingLocked} />
            </Field>
            <Field label={copy.bioEn} htmlFor="instructor-profile-bio-en">
              <Textarea id="instructor-profile-bio-en" value={draft.bioEn} maxLength={4000} onChange={(event) => updateDraft("bioEn", event.target.value)} dir="ltr" rows={8} disabled={busy || editingLocked} />
            </Field>
          </div>
        </WorkspaceSection>

        <WorkspaceSection title={copy.expertiseTitle}>
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div>
              <Field label={copy.expertiseInstitution} htmlFor="instructor-profile-institution">
                <select
                  id="instructor-profile-institution"
                  className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={institutionID}
                  onChange={(event) => { setInstitutionID(event.target.value); setSubjectQuery(""); }}
                  disabled={busy || editingLocked}
                >
                  <option value="">{copy.expertiseInstitution}</option>
                  {institutions.map((institution) => (
                    <option key={institution.id} value={institution.id}>
                      {locale === "ar" ? institution.name_ar : institution.name_en}
                    </option>
                  ))}
                </select>
              </Field>
              <Field className="mt-4" label={copy.expertiseSearch} htmlFor="instructor-profile-expertise-search">
                <Input id="instructor-profile-expertise-search" value={subjectQuery} onChange={(event) => setSubjectQuery(event.target.value)} disabled={!institutionID || busy || editingLocked} />
              </Field>
              {subjectResults.length > 0 ? (
                <ul className="mt-3 space-y-2" aria-label={copy.expertiseSearch}>
                  {subjectResults.map((subject) => (
                    <li key={subject.id}>
                      <button type="button" disabled={busy || editingLocked} className="flex min-h-11 w-full items-center justify-between rounded-md border border-border px-3 text-start text-sm hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => toggleExpertise(subject)}>
                        <span><bdi>{subjectLabel(subject, locale)}</bdi></span>
                        <span className="text-xs font-semibold text-primary">{draft.expertise.some((item) => item.id === subject.id) ? copy.removeExpertise : copy.addExpertise}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              ) : subjectQuery.trim() !== "" && institutionID ? (
                <p className="mt-3 text-sm text-muted-foreground">{copy.expertiseEmpty}</p>
              ) : null}
            </div>
            <div className="rounded-lg border border-border bg-card p-4">
              <h3 className="font-display text-sm font-bold text-foreground">{copy.expertiseSelected}</h3>
              {draft.expertise.length > 0 ? (
                <ul className="mt-3 flex flex-wrap gap-2">
                  {draft.expertise.map((item) => (
                    <li key={item.id} className="inline-flex items-center gap-2 rounded-pill bg-muted px-3 py-1.5 text-sm">
                      <bdi>{locale === "ar" ? item.title_ar : item.title_en}</bdi>
                      <button type="button" disabled={busy || editingLocked} className="font-bold text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => updateDraft("expertise", draft.expertise.filter((candidate) => candidate.id !== item.id))} aria-label={copy.removeExpertise}>×</button>
                    </li>
                  ))}
                </ul>
              ) : <p className="mt-3 text-sm text-muted-foreground">{copy.noExpertise}</p>}
            </div>
          </div>
        </WorkspaceSection>

        <WorkspaceSection title={copy.readinessTitle}>
          <div className="grid gap-2 sm:grid-cols-3">
            {([
              [readiness.slug, copy.checks.slug],
              [readiness.headline, copy.checks.headline],
              [readiness.bio, copy.checks.bio],
            ] as const).map(([done, label]) => (
              <div key={label} className="rounded-md border border-border px-3 py-3 text-sm">
                <span className={done ? "text-primary" : "text-muted-foreground"} aria-hidden>{done ? "✓" : "○"}</span>{" "}
                {label}
              </div>
            ))}
          </div>
          <p className="mt-3 text-sm font-semibold text-muted-foreground">
            {readiness.ready ? copy.checks.ready : copy.checks.needsWork}
          </p>
          <Button className="mt-4" type="submit" disabled={!readiness.ready || busy || editingLocked}>
            {busy ? copy.submitting : copy.submit}
          </Button>
        </WorkspaceSection>
      </form>

      <WorkspaceSection title={copy.previewTitle} description={copy.previewBody}>
        <Card className="max-w-2xl overflow-hidden">
          <div className="bg-gx-navy px-6 py-7 text-white">
            <div className="flex items-center gap-4">
              <Avatar size="lg" aria-hidden><AvatarFallback>{instructorInitials(profile.display_name)}</AvatarFallback></Avatar>
              <div className="min-w-0">
                <p className="text-xs font-semibold text-white/70">{copy.stateLabels[profile.publication_state]}</p>
                <h2 className="mt-1 font-display text-2xl font-bold"><bdi>{profile.display_name}</bdi></h2>
                <p className="mt-1 text-sm text-white/85"><bdi>{displayHeadline || "—"}</bdi></p>
              </div>
            </div>
          </div>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm leading-6 text-foreground"><bdi>{displayBio || "—"}</bdi></p>
          </CardContent>
        </Card>
      </WorkspaceSection>
    </WorkspacePage>
  );
}
