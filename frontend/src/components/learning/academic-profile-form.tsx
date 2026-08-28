"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import {
  academicLevelLabels,
  listCollegeOptions,
  listInstitutionOptions,
  listProgramOptions,
  saveAcademicProfile,
  skipAcademicOnboarding,
  type AcademicProfile,
  type CollegeOption,
  type EnrollmentStatus,
  type InstitutionOption,
  type ProgramOption,
} from "@/lib/api/academic-profile";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import { academicContextNames } from "@/lib/academic/anonymous-context";

/**
 * Student academic onboarding and profile editing (D-092, T3).
 *
 * University → College → Major → level. The Student is never asked to choose a
 * Department: it appears as context beneath a Major and nothing more.
 *
 * Every option comes from the Academic Catalog API. Nothing about Kuwait
 * University, its Colleges, its Programs, or its level range is written here,
 * so the launch Program list is whatever the catalog actually holds.
 *
 * This form collects data. It never affects access: the copy says so, and the
 * server guarantees it.
 */

/** Not a Program row — a state of the Student (D-092 §5). */
const UNDECLARED = "__undeclared__";
const NON_DEGREE = "__non_degree__";
/**
 * Also not a Program row.
 *
 * `EnrollmentStatus` has always had four members and this form offered three.
 * A Student the server recorded as FOUNDATION loaded a form with no way to say
 * so, and the only saveable answers moved them out of it — the form quietly
 * disagreeing with the account about a fact the account owned. The option is
 * offered only where the institution actually has a foundation stage, which is
 * a field on the institution rather than an assumption made here.
 */
const FOUNDATION = "__foundation__";

export function AcademicProfileForm({ mode }: { mode: "onboarding" | "edit" }) {
  const { locale, t: dictionary } = useLocale();
  const t = dictionary.academicProfile;
  const isAr = locale === "ar";
  const router = useRouter();
  // What the Student chose while browsing, shown here as guidance and nothing more. See the
  // handoff note below the form for why it cannot become a preselection.
  //
  // `profile` is the account's own answer, already held above the page. This form used to issue
  // its own `GET /me/academic-profile` beside that one — the same principal-scoped resource, read
  // twice in one browser session, and re-read on every language switch even though the response
  // does not depend on language.
  const { anonymous, profile, adoptProfile } = useAcademicContext();

  const [institutions, setInstitutions] = useState<InstitutionOption[]>([]);
  const [colleges, setColleges] = useState<CollegeOption[]>([]);
  const [programs, setPrograms] = useState<ProgramOption[]>([]);

  const [institutionID, setInstitutionID] = useState("");
  const [collegeID, setCollegeID] = useState("");
  // Holds either a Program identifier or one of the Program-less sentinels.
  const [programChoice, setProgramChoice] = useState("");
  const [levelChoice, setLevelChoice] = useState("");
  /**
   * Whether the Student has touched the University since the form loaded.
   *
   * Restoring a stored profile and honouring a deliberate change are the same
   * two effects looking at the same empty College. Without this they fought:
   * changing University cleared the College, the restore effect saw an empty
   * College and put the *old* University's College back, and the Major list was
   * then requested for a College belonging to a different institution.
   */
  const [institutionTouched, setInstitutionTouched] = useState(false);

  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const name = useCallback(
    (entity: { name_ar: string; name_en: string }) => (isAr ? entity.name_ar : entity.name_en),
    [isAr],
  );

  const selectedInstitution = institutions.find((item) => item.id === institutionID) ?? null;

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const options = await listInstitutionOptions(locale);
        if (cancelled) return;
        setInstitutions(options);
      } catch {
        // The reason a list did not arrive is the transport's business. What
        // the reader needs is the consequence and a way forward, which is one
        // sentence and is the same sentence every time.
        if (!cancelled) setError(t.loadFailed);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [locale, t.loadFailed]);

  // Seed the form from the account's profile once, and only while the Student
  // has not started editing.
  useEffect(() => {
    if (!profile || institutionTouched || institutionID) return;
    if (profile.institution_id) {
      setInstitutionID(profile.institution_id);
    } else if (institutions.length === 1) {
      // One launch institution: choosing it for the Student removes a step
      // that has only one answer, without hardcoding which one it is.
      setInstitutionID(institutions[0].id);
    }
    if (profile.current_level) setLevelChoice(String(profile.current_level));
    if (profile.enrollment_status === "NON_DEGREE") setProgramChoice(NON_DEGREE);
    if (profile.enrollment_status === "FOUNDATION") setProgramChoice(FOUNDATION);
  }, [profile, institutions, institutionID, institutionTouched]);

  // Colleges cascade from the University.
  useEffect(() => {
    let cancelled = false;
    if (!institutionID) {
      setColleges([]);
      return;
    }
    void (async () => {
      try {
        const options = await listCollegeOptions(institutionID, locale);
        if (cancelled) return;
        setColleges(options);
      } catch {
        if (!cancelled) setError(t.loadFailed);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [institutionID, locale, t.loadFailed]);

  // Majors cascade from the College, and come only from its own subtree.
  useEffect(() => {
    let cancelled = false;
    if (!institutionID || !collegeID) {
      setPrograms([]);
      return;
    }
    void (async () => {
      try {
        const options = await listProgramOptions(institutionID, collegeID, locale);
        if (cancelled) return;
        setPrograms(options);
      } catch {
        if (!cancelled) setError(t.loadFailed);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [institutionID, collegeID, locale, t.loadFailed]);

  /**
   * Restore the College an existing profile implies.
   *
   * `academic_unit_id` is a real identifier and is used as one. `college_name`
   * is display prose and is only ever used to *preselect* a list the Student is
   * then asked to confirm — never to compose what gets saved. The Major below
   * is bound by identifier and is dropped entirely if the restored College does
   * not contain it, so a name collision costs a preselection and cannot cost a
   * Student the wrong Major on their account.
   */
  useEffect(() => {
    if (!profile || institutionTouched || collegeID || colleges.length === 0) return;
    if (profile.academic_unit_id) {
      setCollegeID(profile.academic_unit_id);
      setProgramChoice(UNDECLARED);
      return;
    }
    if (profile.college_name) {
      const match = colleges.find(
        (college) =>
          name(college) === profile.college_name ||
          college.name_en === profile.college_name,
      );
      if (match) setCollegeID(match.id);
    }
  }, [profile, colleges, collegeID, institutionTouched, name]);

  // And then the Major itself, once its College's Majors have loaded.
  useEffect(() => {
    if (!profile?.program_id || institutionTouched || programChoice || programs.length === 0) return;
    if (programs.some((program) => program.id === profile.program_id)) {
      setProgramChoice(profile.program_id);
    }
  }, [profile, programs, programChoice, institutionTouched]);

  const changeInstitution = (next: string) => {
    setInstitutionTouched(true);
    setInstitutionID(next);
    // Cascading clears everything downstream, so no impossible combination can
    // survive a change of University.
    setCollegeID("");
    setProgramChoice("");
    setLevelChoice("");
    setError(null);
  };

  const changeCollege = (next: string) => {
    setInstitutionTouched(true);
    setCollegeID(next);
    setProgramChoice("");
    setError(null);
  };

  const withCSRF = async (action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(t.sessionEnded);
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await action(csrf);
    } catch {
      setError(t.saveFailed);
    } finally {
      setBusy(false);
    }
  };

  const submit = () =>
    void withCSRF(async (csrf) => {
      let status: EnrollmentStatus = "ENROLLED";
      let programID: string | null = null;
      let academicUnitID: string | null = null;
      if (programChoice === UNDECLARED) {
        status = "UNDECLARED";
        academicUnitID = collegeID || null;
      } else if (programChoice === NON_DEGREE) {
        status = "NON_DEGREE";
      } else if (programChoice === FOUNDATION) {
        status = "FOUNDATION";
        academicUnitID = collegeID || null;
      } else {
        programID = programChoice;
      }
      const saved = await saveAcademicProfile({
        locale,
        csrf,
        institutionID,
        enrollmentStatus: status,
        programID,
        academicUnitID,
        currentLevel: levelChoice === "" ? null : Number(levelChoice),
      });
      // The application holds the account's profile above the page, and a client navigation away
      // from this form does not remount it. Handing it the server's own answer keeps every surface
      // that reads the profile — the Dashboard's onboarding invitation, and precedence over a
      // browsing preference — from acting on a state the account has just left behind.
      adoptProfile(saved);
      setMessage(t.saved);
      if (mode === "onboarding") {
        // Onboarding hands the Student back to their normal destination.
        router.push(`/${locale}/learn/dashboard`);
      }
    });

  const skip = () =>
    void withCSRF(async (csrf) => {
      const skipped = await skipAcademicOnboarding({ locale, csrf });
      adoptProfile(skipped);
      setMessage(t.skipped);
      router.push(`/${locale}/learn/dashboard`);
    });

  const levels = selectedInstitution
    ? academicLevelLabels(selectedInstitution.max_academic_level, locale)
    : profile?.max_academic_level
      ? academicLevelLabels(profile.max_academic_level, locale)
      : [];

  const canSubmit = institutionID !== "" && programChoice !== "" &&
    (programChoice !== UNDECLARED || collegeID !== "");

  const foundationOffered =
    selectedInstitution?.has_foundation_stage === true ||
    profile?.enrollment_status === "FOUNDATION";

  /**
   * What the account's setup state means for this reader, in a sentence.
   *
   * The states themselves — NOT_STARTED, SKIPPED, COMPLETED — are the server's
   * vocabulary and stay there. Only shown while editing: during onboarding the
   * screen's whole purpose already says it.
   */
  const setupNote =
    mode !== "edit" || !profile
      ? null
      : profile.setup_state === "NOT_STARTED"
        ? { title: t.notStartedTitle, body: t.notStartedBody }
        : profile.setup_state === "SKIPPED"
          ? { title: t.skippedTitle, body: t.skippedBody }
          : null;

  const chosenProgram = programs.find((program) => program.id === programChoice);
  const chosenCollege = colleges.find((item) => item.id === collegeID);
  const programContext = chosenProgram
    ? [
        isAr ? chosenProgram.department_name_ar : chosenProgram.department_name_en,
        chosenCollege ? name(chosenCollege) : null,
      ]
        .filter(Boolean)
        .join(" · ")
    : "";

  return (
    <section
      data-testid="academic-profile-form"
      className="rounded-2xl border border-border bg-card p-6 shadow-sm"
    >
      <h1 className="font-display text-2xl font-bold text-foreground">
        {mode === "onboarding" ? t.onboardingTitle : t.editTitle}
      </h1>
      <p className="mt-2 text-muted-foreground">
        {mode === "onboarding" ? t.onboardingIntro : t.editIntro}
      </p>

      {setupNote ? (
        <div className="mt-5" data-testid="academic-profile-setup-note">
          <Alert title={setupNote.title}>{setupNote.body}</Alert>
        </div>
      ) : null}

      {/**
       * The handoff from anonymous browsing to a real account.
       *
       * This is guidance, deliberately. The public catalogue names academic entities by **slug**;
       * `PUT /me/academic-profile` requires the internal identifier its own option lists return,
       * and those lists carry no slug. There is no contract that turns one into the other, so
       * there is no deterministic mapping to preselect from.
       *
       * The one bridge available without a contract — comparing the localized names on the two
       * lists — is exactly the bridge that must not be built. Display names are editable,
       * translated prose that two different programs can legitimately share, and matching on them
       * would write the wrong program onto a real account while telling the Student it was theirs.
       *
       * So the Student is shown what they chose and asked to confirm it against the real options.
       * The copy says the earlier choice lived on their device, because it did, and nothing here
       * claims it was ever saved to the account.
       */}
      {mode === "onboarding" && anonymous ? (
        <div
          data-testid="academic-profile-handoff"
          className="mt-5 rounded-lg border border-gx-blue-200 bg-gx-blue-50 p-4 text-gx-navy"
        >
          <p className="font-display font-bold">
            {dictionary.academicContext.handoffTitle}
          </p>
          <p className="mt-1 text-sm">{dictionary.academicContext.handoffLead}</p>
          <p
            className="mt-1 font-display text-sm font-bold"
            data-testid="academic-profile-handoff-context"
          >
            {(() => {
              const names = academicContextNames(anonymous, locale);
              const institution = names.institution || anonymous.institutionSlug;
              return names.program === ""
                ? institution
                : `${institution} · ${names.program}`;
            })()}
          </p>
          <p className="mt-2 text-sm">{dictionary.academicContext.handoffNote}</p>
        </div>
      ) : null}

      <div className="mt-6 grid gap-4 sm:grid-cols-2">
        <Field label={t.university} htmlFor="profile-university">
          <Select
            id="profile-university"
            data-testid="profile-university"
            value={institutionID}
            disabled={busy}
            onChange={(event) => changeInstitution(event.target.value)}
          >
            <option value="">{t.select}</option>
            {institutions.map((institution) => (
              <option key={institution.id} value={institution.id}>
                {name(institution)}
              </option>
            ))}
          </Select>
        </Field>

        <Field
          label={t.college}
          htmlFor="profile-college"
          hint={institutionID === "" ? t.selectCollegeFirst : undefined}
        >
          <Select
            id="profile-college"
            data-testid="profile-college"
            value={collegeID}
            disabled={busy || institutionID === ""}
            onChange={(event) => changeCollege(event.target.value)}
          >
            <option value="">{t.select}</option>
            {colleges.map((college) => (
              <option key={college.id} value={college.id}>
                {name(college)}
              </option>
            ))}
          </Select>
        </Field>

        <Field
          label={t.program}
          htmlFor="profile-program"
          hint={
            collegeID === ""
              ? t.selectProgramFirst
              : !chosenProgram && programs.length === 0
                ? t.noPrograms
                : undefined
          }
        >
          <Select
            id="profile-program"
            data-testid="profile-program"
            value={programChoice}
            disabled={busy || collegeID === ""}
            onChange={(event) => setProgramChoice(event.target.value)}
          >
            <option value="">{t.select}</option>
            {programs.map((program) => (
              <option key={program.id} value={program.id}>
                {name(program)}
              </option>
            ))}
            {/* None of these is a Program row. */}
            <option value={UNDECLARED}>{t.undeclared}</option>
            {foundationOffered ? (
              <option value={FOUNDATION}>{t.foundation}</option>
            ) : null}
            <option value={NON_DEGREE}>{t.nonDegree}</option>
          </Select>
          {/* Department is context, never a step. */}
          {programContext !== "" ? (
            <span
              data-testid="profile-program-context"
              className="block text-sm text-muted-foreground"
            >
              {programContext}
            </span>
          ) : null}
        </Field>

        <Field
          label={t.level}
          htmlFor="profile-level"
          hint={institutionID === "" ? t.selectCollegeFirst : undefined}
        >
          <Select
            id="profile-level"
            data-testid="profile-level"
            value={levelChoice}
            disabled={busy || institutionID === ""}
            onChange={(event) => setLevelChoice(event.target.value)}
          >
            {/* Level is optional: a Student is never forced to know their standing. */}
            <option value="">{t.levelUnsure}</option>
            {levels.map((level) => (
              <option key={level.value} value={String(level.value)}>
                {level.label}
              </option>
            ))}
          </Select>
        </Field>
      </div>

      {profile?.curriculum_version_label ? (
        <p data-testid="profile-curriculum" className="mt-4 text-xs text-muted-foreground">
          {t.currentlyOn}: {profile.curriculum_version_label}
        </p>
      ) : null}

      <p data-testid="profile-access-promise" className="mt-4 text-sm text-muted-foreground">
        {t.accessPromise}
      </p>

      {/* Success and failure used to render as the same sentence in the same
          weight and the same colour, distinguishable only by reading them. */}
      {message ? (
        <div className="mt-4" data-testid="profile-message">
          <Alert tone="success" title={message} />
        </div>
      ) : null}
      {error ? (
        <div className="mt-4" data-testid="profile-error">
          <Alert tone="error" title={error} />
        </div>
      ) : null}

      <div className="mt-6 flex flex-wrap gap-3">
        <Button
          type="button"
          data-testid="profile-save"
          disabled={busy || !canSubmit}
          onClick={submit}
        >
          {busy ? t.saving : mode === "onboarding" ? t.save : t.saveEdit}
        </Button>
        {mode === "onboarding" ? (
          <Button
            type="button"
            variant="outline"
            data-testid="profile-skip"
            disabled={busy}
            onClick={skip}
          >
            {t.skip}
          </Button>
        ) : null}
      </div>
    </section>
  );
}
