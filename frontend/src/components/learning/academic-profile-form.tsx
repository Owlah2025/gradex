"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  academicLevelLabels,
  getAcademicProfile,
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

function copy(isAr: boolean) {
  return {
    onboardingTitle: isAr ? "خلّينا نعرف دراستك" : "Tell us about your studies",
    onboardingIntro: isAr
      ? "نستخدم هذه المعلومات لترتيب الكتالوج حسب دراستك. تقدر تتخطاها الآن وتكملها لاحقًا."
      : "We use this to order the catalogue around your studies. You can skip now and finish later.",
    editTitle: isAr ? "ملفك الدراسي" : "Your academic profile",
    university: isAr ? "الجامعة" : "University",
    college: isAr ? "الكلية" : "College",
    program: isAr ? "التخصص" : "Major",
    level: isAr ? "المستوى الدراسي" : "Academic level",
    levelUnsure: isAr ? "لست متأكدًا" : "I'm not sure",
    undeclared: isAr ? "لم أحدد تخصصي بعد" : "I haven't chosen my major yet",
    nonDegree: isAr ? "طالب غير مقيد" : "Non-degree student",
    select: isAr ? "اختر" : "Select",
    save: isAr ? "حفظ والمتابعة" : "Save and continue",
    saveEdit: isAr ? "حفظ التغييرات" : "Save changes",
    skip: isAr ? "تخطي الآن" : "Skip for now",
    saving: isAr ? "جارٍ الحفظ..." : "Saving...",
    // This promise is technically true: the server never reads the profile in
    // any access decision.
    accessPromise: isAr
      ? "تغيير تخصصك أو مستواك يغيّر تخصيص الكتالوج فقط. كورساتك ومشترياتك لا تتأثر."
      : "Changing your major or level only changes how the catalogue is personalised. Your courses and purchases are unaffected.",
    saved: isAr ? "تم حفظ ملفك الدراسي." : "Your academic profile was saved.",
    skipped: isAr ? "تم التخطي. تقدر تكمل ملفك في أي وقت." : "Skipped. You can finish your profile any time.",
    loadFailed: isAr ? "تعذر تحميل خيارات الدراسة" : "Unable to load your study options",
    saveFailed: isAr ? "تعذر حفظ ملفك الدراسي" : "Unable to save your academic profile",
    noPrograms: isAr ? "لا توجد تخصصات متاحة لهذه الكلية بعد." : "No majors are available for this college yet.",
    currentlyOn: isAr ? "خطتك الدراسية" : "Your study plan",
  };
}

export function AcademicProfileForm({ mode }: { mode: "onboarding" | "edit" }) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const t = useMemo(() => copy(isAr), [isAr]);
  const router = useRouter();

  const [profile, setProfile] = useState<AcademicProfile | null>(null);
  const [institutions, setInstitutions] = useState<InstitutionOption[]>([]);
  const [colleges, setColleges] = useState<CollegeOption[]>([]);
  const [programs, setPrograms] = useState<ProgramOption[]>([]);

  const [institutionID, setInstitutionID] = useState("");
  const [collegeID, setCollegeID] = useState("");
  // Holds either a Program identifier or one of the two Program-less sentinels.
  const [programChoice, setProgramChoice] = useState("");
  const [levelChoice, setLevelChoice] = useState("");

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
        const [existing, options] = await Promise.all([
          getAcademicProfile(locale),
          listInstitutionOptions(locale),
        ]);
        if (cancelled) return;
        setProfile(existing);
        setInstitutions(options);
        // Pre-populate an existing profile so editing starts from the truth.
        if (existing.institution_id) {
          setInstitutionID(existing.institution_id);
        } else if (options.length === 1) {
          // One launch institution: choosing it for the Student removes a step
          // that has only one answer, without hardcoding which one it is.
          setInstitutionID(options[0].id);
        }
        if (existing.current_level) setLevelChoice(String(existing.current_level));
        if (existing.enrollment_status === "NON_DEGREE") setProgramChoice(NON_DEGREE);
      } catch (loadError) {
        if (!cancelled) {
          setError(loadError instanceof Error ? loadError.message : t.loadFailed);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [locale, t.loadFailed]);

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
      } catch (loadError) {
        if (!cancelled) setError(loadError instanceof Error ? loadError.message : t.loadFailed);
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
      } catch (loadError) {
        if (!cancelled) setError(loadError instanceof Error ? loadError.message : t.loadFailed);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [institutionID, collegeID, locale, t.loadFailed]);

  // Once the Colleges are known, restore the one an existing profile implies —
  // the stored College for an undeclared Student, or the derived College name
  // for an enrolled one.
  useEffect(() => {
    if (!profile || collegeID || colleges.length === 0) return;
    if (profile.academic_unit_id) {
      setCollegeID(profile.academic_unit_id);
      setProgramChoice(UNDECLARED);
      return;
    }
    if (profile.college_name) {
      const match = colleges.find((college) => name(college) === profile.college_name ||
        college.name_en === profile.college_name);
      if (match) setCollegeID(match.id);
    }
  }, [profile, colleges, collegeID, name]);

  // And then the Major itself, once its College's Majors have loaded.
  useEffect(() => {
    if (!profile?.program_id || programChoice || programs.length === 0) return;
    if (programs.some((program) => program.id === profile.program_id)) {
      setProgramChoice(profile.program_id);
    }
  }, [profile, programs, programChoice]);

  const changeInstitution = (next: string) => {
    setInstitutionID(next);
    // Cascading clears everything downstream, so no impossible combination can
    // survive a change of University.
    setCollegeID("");
    setProgramChoice("");
    setLevelChoice("");
    setError(null);
  };

  const changeCollege = (next: string) => {
    setCollegeID(next);
    setProgramChoice("");
    setError(null);
  };

  const withCSRF = async (action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await action(csrf);
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : t.saveFailed);
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
      setProfile(saved);
      setMessage(t.saved);
      if (mode === "onboarding") {
        // Onboarding hands the Student back to their normal destination.
        router.push(`/${locale}/learn/dashboard`);
      }
    });

  const skip = () =>
    void withCSRF(async (csrf) => {
      const skipped = await skipAcademicOnboarding({ locale, csrf });
      setProfile(skipped);
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

  const fieldClass =
    "mt-1 w-full rounded-md border border-border bg-background p-2.5 text-sm text-foreground";

  return (
    <section
      data-testid="academic-profile-form"
      dir={isAr ? "rtl" : "ltr"}
      className="rounded-2xl border border-border bg-card p-6 shadow-sm"
    >
      <h2 className="font-display text-2xl font-bold text-foreground">
        {mode === "onboarding" ? t.onboardingTitle : t.editTitle}
      </h2>
      {mode === "onboarding" ? (
        <p className="mt-2 text-muted-foreground">{t.onboardingIntro}</p>
      ) : null}

      <div className="mt-6 grid gap-4 sm:grid-cols-2">
        <label className="block text-sm font-semibold text-foreground">
          {t.university}
          <select
            data-testid="profile-university"
            className={fieldClass}
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
          </select>
        </label>

        <label className="block text-sm font-semibold text-foreground">
          {t.college}
          <select
            data-testid="profile-college"
            className={fieldClass}
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
          </select>
        </label>

        <label className="block text-sm font-semibold text-foreground">
          {t.program}
          <select
            data-testid="profile-program"
            className={fieldClass}
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
            {/* Neither of these is a Program row. */}
            <option value={UNDECLARED}>{t.undeclared}</option>
            <option value={NON_DEGREE}>{t.nonDegree}</option>
          </select>
          {/* Department is context, never a step. */}
          {(() => {
            const chosen = programs.find((program) => program.id === programChoice);
            const department = chosen && (isAr ? chosen.department_name_ar : chosen.department_name_en);
            const college = colleges.find((item) => item.id === collegeID);
            if (!chosen) {
              return collegeID !== "" && programs.length === 0 ? (
                <span data-testid="profile-no-programs" className="mt-1 block text-xs text-muted-foreground">
                  {t.noPrograms}
                </span>
              ) : null;
            }
            return (
              <span data-testid="profile-program-context" className="mt-1 block text-xs text-muted-foreground">
                {[department, college ? name(college) : null].filter(Boolean).join(" · ")}
              </span>
            );
          })()}
        </label>

        <label className="block text-sm font-semibold text-foreground">
          {t.level}
          <select
            data-testid="profile-level"
            className={fieldClass}
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
          </select>
        </label>
      </div>

      {profile?.curriculum_version_label ? (
        <p data-testid="profile-curriculum" className="mt-4 text-xs text-muted-foreground">
          {t.currentlyOn}: {profile.curriculum_version_label}
        </p>
      ) : null}

      <p data-testid="profile-access-promise" className="mt-4 text-sm text-muted-foreground">
        {t.accessPromise}
      </p>

      {message ? (
        <p role="status" data-testid="profile-message" className="mt-4 text-sm font-semibold text-foreground">
          {message}
        </p>
      ) : null}
      {error ? (
        <p role="alert" data-testid="profile-error" className="mt-4 text-sm font-semibold text-foreground">
          {error}
        </p>
      ) : null}

      <div className="mt-6 flex flex-wrap gap-3">
        <button
          type="button"
          data-testid="profile-save"
          disabled={busy || !canSubmit}
          onClick={submit}
          className="rounded-md border border-border bg-foreground px-4 py-2 font-semibold text-background hover:opacity-90 disabled:opacity-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {busy ? t.saving : mode === "onboarding" ? t.save : t.saveEdit}
        </button>
        {mode === "onboarding" ? (
          <button
            type="button"
            data-testid="profile-skip"
            disabled={busy}
            onClick={skip}
            className="rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent disabled:opacity-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {t.skip}
          </button>
        ) : null}
      </div>
    </section>
  );
}
