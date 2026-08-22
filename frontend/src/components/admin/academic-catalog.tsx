"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  createCurriculum,
  createInstitution,
  createProgram,
  createSubject,
  createUnit,
  duplicateSubjectConflict,
  listCurricula,
  listCurriculumSubjects,
  listInstitutions,
  listPrograms,
  listSubjects,
  listUnits,
  mapSubjectToCurriculum,
  retireSubject,
  subjectLabel,
  type AcademicUnit,
  type Curriculum,
  type CurriculumSubject,
  type Institution,
  type Program,
  type RequirementKind,
  type Subject,
} from "@/lib/api/academic";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { SubjectRequestQueue } from "./subject-request-queue";

/**
 * Admin Academic Catalog (AD13, D-091 T1).
 *
 * Separate from Course review by construction: this surface never loads a
 * Course, a revision, or a legacy taxonomy term. It also never renders an
 * identifier as workflow — every selection is by name, and the only identifier
 * on screen is a university's own official Subject code, which is real academic
 * information rather than a database key.
 */

const UNIT_KINDS = ["COLLEGE", "DEPARTMENT", "SERVICE_UNIT"] as const;

const REQUIREMENT_KINDS: RequirementKind[] = [
  "UNIVERSITY_REQUIREMENT",
  "COLLEGE_REQUIREMENT",
  "MAJOR_CORE",
  "MAJOR_ELECTIVE",
  "SUPPORTING",
  "FREE_ELECTIVE",
];

function copy(isAr: boolean) {
  return {
    heading: isAr ? "الكتالوج الأكاديمي" : "Academic Catalog",
    intro: isAr
      ? "إدارة الجامعات والكليات والأقسام والتخصصات والخطط الدراسية والمواد."
      : "Manage universities, colleges, departments, majors, study plans, and subjects.",
    university: isAr ? "الجامعة" : "University",
    universities: isAr ? "الجامعات" : "Universities",
    college: isAr ? "الكلية" : "College",
    department: isAr ? "القسم" : "Department",
    serviceUnit: isAr ? "وحدة خدمية" : "Service unit",
    units: isAr ? "الكليات والأقسام" : "Colleges & departments",
    programs: isAr ? "التخصصات" : "Majors",
    curriculum: isAr ? "الخطة الدراسية" : "Study plan",
    subjects: isAr ? "المواد" : "Subjects",
    emptyCatalog: isAr
      ? "لا توجد جامعات بعد. ابدأ بإضافة جامعة."
      : "No universities yet. Start by adding one.",
    emptyUnits: isAr ? "لا توجد كليات أو أقسام بعد." : "No colleges or departments yet.",
    emptyPrograms: isAr ? "لا توجد تخصصات بعد." : "No majors yet.",
    emptySubjects: isAr ? "لا توجد مواد بعد." : "No subjects yet.",
    emptyCurriculum: isAr ? "لا توجد خطة دراسية بعد." : "No study plan yet.",
    emptyMappings: isAr ? "لم تُضف مواد إلى هذه الخطة بعد." : "No subjects added to this plan yet.",
    add: isAr ? "إضافة" : "Add",
    addUniversity: isAr ? "إضافة جامعة" : "Add university",
    addUnit: isAr ? "إضافة كلية أو قسم" : "Add college or department",
    addProgram: isAr ? "إضافة تخصص" : "Add major",
    addCurriculum: isAr ? "إنشاء خطة دراسية" : "Create study plan",
    addSubject: isAr ? "إضافة مادة" : "Add subject",
    mapSubject: isAr ? "إضافة مادة إلى الخطة" : "Add subject to plan",
    retire: isAr ? "إيقاف" : "Retire",
    nameAr: isAr ? "الاسم بالعربية" : "Arabic name",
    nameEn: isAr ? "الاسم بالإنجليزية" : "English name",
    titleAr: isAr ? "اسم المادة بالعربية" : "Arabic subject title",
    titleEn: isAr ? "اسم المادة بالإنجليزية" : "English subject title",
    slug: isAr ? "المعرّف المختصر" : "Short identifier",
    officialCode: isAr ? "الرمز الرسمي" : "Official code",
    officialCodeHint: isAr ? "مثال: 0410-101" : "For example: 0410-101",
    degreeKind: isAr ? "نوع الدرجة" : "Degree kind",
    countryCode: isAr ? "الدولة" : "Country",
    maxLevel: isAr ? "أعلى مستوى دراسي" : "Highest academic level",
    foundationStage: isAr ? "توجد سنة تأسيسية" : "Has a foundation stage",
    parentUnit: isAr ? "يتبع" : "Belongs to",
    noParent: isAr ? "يتبع الجامعة مباشرة" : "Directly under the university",
    owningUnit: isAr ? "الجهة المالكة" : "Owning unit",
    versionLabel: isAr ? "إصدار الخطة" : "Plan version",
    supersedeActive: isAr ? "استبدال الخطة الحالية" : "Replace the current plan",
    requirementKind: isAr ? "نوع المتطلب" : "Requirement type",
    recommendedLevel: isAr ? "المستوى الدراسي المقترح" : "Recommended level",
    kind: isAr ? "النوع" : "Kind",
    select: isAr ? "اختر" : "Select",
    saving: isAr ? "جارٍ الحفظ..." : "Saving...",
    retired: isAr ? "موقوفة" : "Retired",
    active: isAr ? "الحالية" : "Active",
    superseded: isAr ? "مستبدلة" : "Superseded",
    duplicateSubject: isAr
      ? "هذه المادة موجودة بالفعل في هذه الجامعة:"
      : "This subject already exists at this university:",
    loadFailed: isAr ? "تعذر تحميل الكتالوج الأكاديمي" : "Unable to load the Academic Catalog",
    saveFailed: isAr ? "تعذر الحفظ" : "Unable to save",
    searchSubjects: isAr ? "ابحث بالرمز أو الاسم" : "Search by code or name",
  };
}

function requirementLabel(kind: RequirementKind, isAr: boolean): string {
  const labels: Record<RequirementKind, [string, string]> = {
    UNIVERSITY_REQUIREMENT: ["متطلب جامعة", "University requirement"],
    COLLEGE_REQUIREMENT: ["متطلب كلية", "College requirement"],
    MAJOR_CORE: ["إجباري تخصص", "Major core"],
    MAJOR_ELECTIVE: ["اختياري تخصص", "Major elective"],
    SUPPORTING: ["تخصص مساند", "Supporting"],
    FREE_ELECTIVE: ["اختياري حر", "Free elective"],
  };
  return isAr ? labels[kind][0] : labels[kind][1];
}

export function AcademicCatalog() {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const t = useMemo(() => copy(isAr), [isAr]);

  const [institutions, setInstitutions] = useState<Institution[]>([]);
  const [institutionID, setInstitutionID] = useState("");
  const [units, setUnits] = useState<AcademicUnit[]>([]);
  const [programs, setPrograms] = useState<Program[]>([]);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [programID, setProgramID] = useState("");
  const [curricula, setCurricula] = useState<Curriculum[]>([]);
  const [mappings, setMappings] = useState<CurriculumSubject[]>([]);
  const [subjectQuery, setSubjectQuery] = useState("");

  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [conflict, setConflict] = useState<Subject | null>(null);
  const [loaded, setLoaded] = useState(false);

  const selectedInstitution = institutions.find((i) => i.id === institutionID) ?? null;
  const activeCurriculum = curricula.find((c) => c.status === "ACTIVE") ?? null;

  const name = useCallback(
    (entity: { name_ar: string; name_en: string }) => (isAr ? entity.name_ar : entity.name_en),
    [isAr],
  );

  const report = (error: unknown, fallback: string) => {
    const existing = duplicateSubjectConflict(error);
    if (existing) {
      setConflict(existing);
      setMessage(null);
      return;
    }
    setConflict(null);
    setMessage(error instanceof Error ? error.message : fallback);
  };

  const refreshInstitutions = useCallback(async () => {
    try {
      setInstitutions(await listInstitutions(locale));
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t.loadFailed);
    } finally {
      setLoaded(true);
    }
  }, [locale, t.loadFailed]);

  useEffect(() => {
    void refreshInstitutions();
  }, [refreshInstitutions]);

  const refreshInstitutionChildren = useCallback(
    async (id: string) => {
      if (!id) {
        setUnits([]);
        setPrograms([]);
        setSubjects([]);
        return;
      }
      try {
        const [nextUnits, nextPrograms, nextSubjects] = await Promise.all([
          listUnits(id, locale),
          listPrograms(id, locale),
          listSubjects(id, locale, subjectQuery),
        ]);
        setUnits(nextUnits);
        setPrograms(nextPrograms);
        setSubjects(nextSubjects);
      } catch (error) {
        setMessage(error instanceof Error ? error.message : t.loadFailed);
      }
    },
    [locale, subjectQuery, t.loadFailed],
  );

  useEffect(() => {
    void refreshInstitutionChildren(institutionID);
  }, [institutionID, refreshInstitutionChildren]);

  const refreshProgramPlan = useCallback(
    async (id: string) => {
      if (!id) {
        setCurricula([]);
        setMappings([]);
        return;
      }
      try {
        const nextCurricula = await listCurricula(id, locale);
        setCurricula(nextCurricula);
        const active = nextCurricula.find((c) => c.status === "ACTIVE");
        setMappings(active ? await listCurriculumSubjects(active.id, locale) : []);
      } catch (error) {
        setMessage(error instanceof Error ? error.message : t.loadFailed);
      }
    },
    [locale, t.loadFailed],
  );

  useEffect(() => {
    void refreshProgramPlan(programID);
  }, [programID, refreshProgramPlan]);

  /** Every mutation goes through here so CSRF handling has exactly one owner. */
  const run = async (action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setMessage(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }
    setBusy(true);
    setMessage(null);
    setConflict(null);
    try {
      await action(csrf);
    } catch (error) {
      report(error, t.saveFailed);
    } finally {
      setBusy(false);
    }
  };

  const formField = (label: string, node: React.ReactNode) => (
    <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
      {label}
      {node}
    </label>
  );

  const inputClass =
    "mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900";

  return (
    <section className="mx-auto max-w-container space-y-6 px-5 py-8 sm:px-6" data-testid="academic-catalog">
      <header>
        <h1 className="text-lg font-bold text-slate-900 dark:text-slate-100">{t.heading}</h1>
        <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">{t.intro}</p>
      </header>

      <SubjectRequestQueue />

      {message && (
        <p role="status" data-testid="academic-message" className="rounded border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200">
          {message}
        </p>
      )}
      {conflict && (
        <p role="status" data-testid="academic-duplicate-subject" className="rounded border border-rose-300 bg-rose-50 p-3 text-xs text-rose-900 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
          {t.duplicateSubject} <strong>{subjectLabel(conflict, locale)}</strong>
        </p>
      )}

      {/* Universities */}
      <div className="rounded-xl border border-slate-200 p-5 dark:border-slate-800">
        <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{t.universities}</h2>
        {loaded && institutions.length === 0 ? (
          <p data-testid="academic-empty" className="mt-2 text-xs text-slate-600 dark:text-slate-400">
            {t.emptyCatalog}
          </p>
        ) : (
          <label className="mt-3 block text-xs font-semibold text-slate-700 dark:text-slate-300">
            {t.university}
            <select
              data-testid="academic-institution"
              className={inputClass}
              value={institutionID}
              onChange={(event) => {
                setInstitutionID(event.target.value);
                setProgramID("");
              }}
            >
              <option value="">{t.select}</option>
              {institutions.map((institution) => (
                <option key={institution.id} value={institution.id}>
                  {name(institution)}
                </option>
              ))}
            </select>
          </label>
        )}

        <form
          className="mt-4 grid gap-3 md:grid-cols-3"
          data-testid="academic-institution-form"
          onSubmit={(event) => {
            event.preventDefault();
            const form = new FormData(event.currentTarget);
            void run(async (csrf) => {
              const created = await createInstitution({
                locale,
                csrf,
                countryCode: String(form.get("country_code") ?? "KW"),
                slug: String(form.get("slug") ?? ""),
                nameAr: String(form.get("name_ar") ?? ""),
                nameEn: String(form.get("name_en") ?? ""),
                maxAcademicLevel: Number(form.get("max_academic_level") ?? 4),
                hasFoundationStage: form.get("has_foundation_stage") === "on",
              });
              await refreshInstitutions();
              setInstitutionID(created.id);
            });
          }}
        >
          {formField(t.nameAr, <input name="name_ar" required className={inputClass} data-testid="institution-name-ar" />)}
          {formField(t.nameEn, <input name="name_en" required className={inputClass} data-testid="institution-name-en" />)}
          {formField(t.slug, <input name="slug" required pattern="[a-z0-9]+(-[a-z0-9]+)*" className={inputClass} data-testid="institution-slug" />)}
          {formField(t.countryCode, <input name="country_code" defaultValue="KW" required maxLength={2} className={inputClass} data-testid="institution-country" />)}
          {formField(
            t.maxLevel,
            <input name="max_academic_level" type="number" min={1} max={12} defaultValue={4} required className={inputClass} data-testid="institution-max-level" />,
          )}
          <label className="mt-6 flex items-center gap-2 text-xs font-semibold text-slate-700 dark:text-slate-300">
            <input name="has_foundation_stage" type="checkbox" data-testid="institution-foundation" />
            {t.foundationStage}
          </label>
          <button type="submit" disabled={busy} className="mt-1 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="institution-submit">
            {busy ? t.saving : t.addUniversity}
          </button>
        </form>
      </div>

      {selectedInstitution && (
        <>
          {/* Colleges and departments */}
          <div className="rounded-xl border border-slate-200 p-5 dark:border-slate-800">
            <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{t.units}</h2>
            {units.length === 0 ? (
              <p className="mt-2 text-xs text-slate-600 dark:text-slate-400" data-testid="units-empty">{t.emptyUnits}</p>
            ) : (
              <ul className="mt-3 space-y-1 text-xs text-slate-700 dark:text-slate-300" data-testid="units-list">
                {units.map((unit) => {
                  const parent = units.find((candidate) => candidate.id === unit.parent_unit_id);
                  return (
                    <li key={unit.id} className={unit.parent_unit_id ? "ms-5" : ""}>
                      <span className="font-semibold">{name(unit)}</span>{" "}
                      <span className="text-slate-500">
                        (
                        {unit.kind === "COLLEGE" ? t.college : unit.kind === "DEPARTMENT" ? t.department : t.serviceUnit}
                        {parent ? ` — ${t.parentUnit} ${name(parent)}` : ` — ${t.noParent}`})
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}

            <form
              className="mt-4 grid gap-3 md:grid-cols-3"
              data-testid="academic-unit-form"
              onSubmit={(event) => {
                event.preventDefault();
                const form = new FormData(event.currentTarget);
                void run(async (csrf) => {
                  await createUnit({
                    locale,
                    csrf,
                    institutionID,
                    kind: String(form.get("kind") ?? "COLLEGE") as AcademicUnit["kind"],
                    slug: String(form.get("slug") ?? ""),
                    nameAr: String(form.get("name_ar") ?? ""),
                    nameEn: String(form.get("name_en") ?? ""),
                    parentUnitID: String(form.get("parent_unit_id") ?? "") || null,
                  });
                  await refreshInstitutionChildren(institutionID);
                });
              }}
            >
              {formField(t.nameAr, <input name="name_ar" required className={inputClass} data-testid="unit-name-ar" />)}
              {formField(t.nameEn, <input name="name_en" required className={inputClass} data-testid="unit-name-en" />)}
              {formField(t.slug, <input name="slug" required pattern="[a-z0-9]+(-[a-z0-9]+)*" className={inputClass} data-testid="unit-slug" />)}
              {formField(
                t.kind,
                <select name="kind" className={inputClass} data-testid="unit-kind" defaultValue="COLLEGE">
                  {UNIT_KINDS.map((kind) => (
                    <option key={kind} value={kind}>
                      {kind === "COLLEGE" ? t.college : kind === "DEPARTMENT" ? t.department : t.serviceUnit}
                    </option>
                  ))}
                </select>,
              )}
              {formField(
                t.parentUnit,
                <select name="parent_unit_id" className={inputClass} data-testid="unit-parent" defaultValue="">
                  <option value="">{t.noParent}</option>
                  {units.map((unit) => (
                    <option key={unit.id} value={unit.id}>
                      {name(unit)}
                    </option>
                  ))}
                </select>,
              )}
              <button type="submit" disabled={busy} className="mt-6 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="unit-submit">
                {busy ? t.saving : t.addUnit}
              </button>
            </form>
          </div>

          {/* Majors */}
          <div className="rounded-xl border border-slate-200 p-5 dark:border-slate-800">
            <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{t.programs}</h2>
            {programs.length === 0 ? (
              <p className="mt-2 text-xs text-slate-600 dark:text-slate-400" data-testid="programs-empty">{t.emptyPrograms}</p>
            ) : (
              <label className="mt-3 block text-xs font-semibold text-slate-700 dark:text-slate-300">
                {t.programs}
                <select
                  data-testid="academic-program"
                  className={inputClass}
                  value={programID}
                  onChange={(event) => setProgramID(event.target.value)}
                >
                  <option value="">{t.select}</option>
                  {programs.map((program) => (
                    <option key={program.id} value={program.id}>
                      {name(program)}
                    </option>
                  ))}
                </select>
              </label>
            )}

            <form
              className="mt-4 grid gap-3 md:grid-cols-3"
              data-testid="academic-program-form"
              onSubmit={(event) => {
                event.preventDefault();
                const form = new FormData(event.currentTarget);
                void run(async (csrf) => {
                  await createProgram({
                    locale,
                    csrf,
                    institutionID,
                    slug: String(form.get("slug") ?? ""),
                    nameAr: String(form.get("name_ar") ?? ""),
                    nameEn: String(form.get("name_en") ?? ""),
                    degreeKind: String(form.get("degree_kind") ?? "BSC"),
                    owningUnitID: String(form.get("owning_unit_id") ?? "") || null,
                  });
                  await refreshInstitutionChildren(institutionID);
                });
              }}
            >
              {formField(t.nameAr, <input name="name_ar" required className={inputClass} data-testid="program-name-ar" />)}
              {formField(t.nameEn, <input name="name_en" required className={inputClass} data-testid="program-name-en" />)}
              {formField(t.slug, <input name="slug" required pattern="[a-z0-9]+(-[a-z0-9]+)*" className={inputClass} data-testid="program-slug" />)}
              {formField(t.degreeKind, <input name="degree_kind" defaultValue="BSC" required className={inputClass} data-testid="program-degree" />)}
              {formField(
                t.owningUnit,
                <select name="owning_unit_id" className={inputClass} data-testid="program-owning-unit" defaultValue="">
                  <option value="">{t.noParent}</option>
                  {units.map((unit) => (
                    <option key={unit.id} value={unit.id}>
                      {name(unit)}
                    </option>
                  ))}
                </select>,
              )}
              <button type="submit" disabled={busy} className="mt-6 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="program-submit">
                {busy ? t.saving : t.addProgram}
              </button>
            </form>
          </div>

          {/* Subjects */}
          <div className="rounded-xl border border-slate-200 p-5 dark:border-slate-800">
            <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{t.subjects}</h2>
            <input
              className={inputClass}
              placeholder={t.searchSubjects}
              data-testid="subject-search"
              value={subjectQuery}
              onChange={(event) => setSubjectQuery(event.target.value)}
            />
            {subjects.length === 0 ? (
              <p className="mt-2 text-xs text-slate-600 dark:text-slate-400" data-testid="subjects-empty">{t.emptySubjects}</p>
            ) : (
              <ul className="mt-3 space-y-1 text-xs text-slate-700 dark:text-slate-300" data-testid="subjects-list">
                {subjects.map((subject) => (
                  <li key={subject.id} className="flex items-center justify-between gap-3">
                    <span>{subjectLabel(subject, locale)}</span>
                    <button
                      type="button"
                      disabled={busy}
                      data-testid={`subject-retire-${subject.official_code ?? subject.title_en}`}
                      className="rounded border border-slate-300 px-2 py-1 text-[11px] font-semibold hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:hover:bg-slate-800"
                      onClick={() =>
                        void run(async (csrf) => {
                          await retireSubject({ locale, csrf, subjectID: subject.id });
                          await refreshInstitutionChildren(institutionID);
                        })
                      }
                    >
                      {t.retire}
                    </button>
                  </li>
                ))}
              </ul>
            )}

            <form
              className="mt-4 grid gap-3 md:grid-cols-3"
              data-testid="academic-subject-form"
              onSubmit={(event) => {
                event.preventDefault();
                const form = new FormData(event.currentTarget);
                void run(async (csrf) => {
                  await createSubject({
                    locale,
                    csrf,
                    institutionID,
                    titleAr: String(form.get("title_ar") ?? ""),
                    titleEn: String(form.get("title_en") ?? ""),
                    officialCode: String(form.get("official_code") ?? "") || null,
                    owningUnitID: String(form.get("owning_unit_id") ?? "") || null,
                  });
                  await refreshInstitutionChildren(institutionID);
                });
              }}
            >
              {formField(t.titleAr, <input name="title_ar" required className={inputClass} data-testid="subject-title-ar" />)}
              {formField(t.titleEn, <input name="title_en" required className={inputClass} data-testid="subject-title-en" />)}
              {formField(
                `${t.officialCode} (${t.officialCodeHint})`,
                <input name="official_code" className={inputClass} data-testid="subject-code" />,
              )}
              {formField(
                t.owningUnit,
                <select name="owning_unit_id" className={inputClass} data-testid="subject-owning-unit" defaultValue="">
                  <option value="">{t.noParent}</option>
                  {units.map((unit) => (
                    <option key={unit.id} value={unit.id}>
                      {name(unit)}
                    </option>
                  ))}
                </select>,
              )}
              <button type="submit" disabled={busy} className="mt-6 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="subject-submit">
                {busy ? t.saving : t.addSubject}
              </button>
            </form>
          </div>

          {/* Study plan */}
          {programID && (
            <div className="rounded-xl border border-slate-200 p-5 dark:border-slate-800">
              <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{t.curriculum}</h2>
              {curricula.length === 0 ? (
                <p className="mt-2 text-xs text-slate-600 dark:text-slate-400" data-testid="curriculum-empty">{t.emptyCurriculum}</p>
              ) : (
                <ul className="mt-2 text-xs text-slate-700 dark:text-slate-300" data-testid="curriculum-list">
                  {curricula.map((curriculum) => (
                    <li key={curriculum.id}>
                      {curriculum.version_label} —{" "}
                      {curriculum.status === "ACTIVE" ? t.active : t.superseded}
                    </li>
                  ))}
                </ul>
              )}

              <form
                className="mt-4 grid gap-3 md:grid-cols-3"
                data-testid="academic-curriculum-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  const form = new FormData(event.currentTarget);
                  void run(async (csrf) => {
                    await createCurriculum({
                      locale,
                      csrf,
                      programID,
                      versionLabel: String(form.get("version_label") ?? ""),
                      supersedeActive: form.get("supersede_active") === "on",
                    });
                    await refreshProgramPlan(programID);
                  });
                }}
              >
                {formField(t.versionLabel, <input name="version_label" required className={inputClass} data-testid="curriculum-version" />)}
                <label className="mt-6 flex items-center gap-2 text-xs font-semibold text-slate-700 dark:text-slate-300">
                  <input name="supersede_active" type="checkbox" data-testid="curriculum-supersede" />
                  {t.supersedeActive}
                </label>
                <button type="submit" disabled={busy} className="mt-1 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="curriculum-submit">
                  {busy ? t.saving : t.addCurriculum}
                </button>
              </form>

              {activeCurriculum && (
                <>
                  {mappings.length === 0 ? (
                    <p className="mt-4 text-xs text-slate-600 dark:text-slate-400" data-testid="mappings-empty">{t.emptyMappings}</p>
                  ) : (
                    <ul className="mt-4 space-y-1 text-xs text-slate-700 dark:text-slate-300" data-testid="mappings-list">
                      {mappings.map((mapping) => (
                        <li key={mapping.id}>
                          {mapping.subject_official_code ? `${mapping.subject_official_code} · ` : ""}
                          {isAr ? mapping.subject_title_ar : mapping.subject_title_en}
                          {" — "}
                          {requirementLabel(mapping.requirement_kind, isAr)}
                          {mapping.recommended_level ? ` — ${t.recommendedLevel} ${mapping.recommended_level}` : ""}
                        </li>
                      ))}
                    </ul>
                  )}

                  <form
                    className="mt-4 grid gap-3 md:grid-cols-3"
                    data-testid="academic-mapping-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      const form = new FormData(event.currentTarget);
                      void run(async (csrf) => {
                        const level = String(form.get("recommended_level") ?? "");
                        await mapSubjectToCurriculum({
                          locale,
                          csrf,
                          curriculumID: activeCurriculum.id,
                          subjectID: String(form.get("subject_id") ?? ""),
                          requirementKind: String(form.get("requirement_kind") ?? "MAJOR_CORE") as RequirementKind,
                          recommendedLevel: level === "" ? null : Number(level),
                        });
                        await refreshProgramPlan(programID);
                      });
                    }}
                  >
                    {formField(
                      t.subjects,
                      <select name="subject_id" required className={inputClass} data-testid="mapping-subject" defaultValue="">
                        <option value="">{t.select}</option>
                        {subjects.map((subject) => (
                          <option key={subject.id} value={subject.id}>
                            {subjectLabel(subject, locale)}
                          </option>
                        ))}
                      </select>,
                    )}
                    {formField(
                      t.requirementKind,
                      <select name="requirement_kind" className={inputClass} data-testid="mapping-requirement" defaultValue="MAJOR_CORE">
                        {REQUIREMENT_KINDS.map((kind) => (
                          <option key={kind} value={kind}>
                            {requirementLabel(kind, isAr)}
                          </option>
                        ))}
                      </select>,
                    )}
                    {formField(
                      `${t.recommendedLevel} (1–${selectedInstitution.max_academic_level})`,
                      <input
                        name="recommended_level"
                        type="number"
                        min={1}
                        max={selectedInstitution.max_academic_level}
                        className={inputClass}
                        data-testid="mapping-level"
                      />,
                    )}
                    <button type="submit" disabled={busy} className="mt-6 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50" data-testid="mapping-submit">
                      {busy ? t.saving : t.mapSubject}
                    </button>
                  </form>
                </>
              )}
            </div>
          )}
        </>
      )}
    </section>
  );

}
