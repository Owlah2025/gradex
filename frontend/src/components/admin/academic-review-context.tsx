import type { CourseRevisionWire } from "@/lib/api/catalog";
import type { ReviewedCourse } from "@/lib/api/review";

export function AcademicReviewContext({
  course,
  revision,
  locale,
}: {
  course: ReviewedCourse;
  revision: CourseRevisionWire;
  locale: "ar" | "en";
}) {
  const isAr = locale === "ar";
  const subject = course.academic_context?.subject;
  const academicUnit = [
    isAr ? subject?.parent_unit_name_ar : subject?.parent_unit_name_en,
    isAr ? subject?.owning_unit_name_ar : subject?.owning_unit_name_en,
  ].filter(Boolean).join(" · ");

  return (
    <>
      <div><p className="text-xs font-semibold text-slate-500">{isAr ? "الجامعة" : "University"}</p><p data-testid="submitted-academic-university" className="mt-1 text-slate-900 dark:text-slate-100">{isAr ? course.academic_context?.institution_name_ar : course.academic_context?.institution_name_en}</p></div>
      <div><p className="text-xs font-semibold text-slate-500">{isAr ? "المادة" : "Subject"}</p><p data-testid="submitted-academic-subject" className="mt-1 text-slate-900 dark:text-slate-100">{[subject?.official_code, isAr ? subject?.title_ar : subject?.title_en].filter(Boolean).join(" · ") || "—"}</p></div>
      {academicUnit && <div><p className="text-xs font-semibold text-slate-500">{isAr ? "الجهة الأكاديمية" : "Academic unit"}</p><p data-testid="submitted-academic-unit" className="mt-1 text-slate-900 dark:text-slate-100">{academicUnit}</p></div>}
      <div className="md:col-span-2" data-testid="submitted-academic-audience">
        <p className="text-xs font-semibold text-slate-500">{isAr ? "الجمهور" : "Audience"}</p>
        <p className="mt-1 text-sm font-medium">{revision.audience?.mode === "CUSTOMIZED" ? (isAr ? "مخصص" : "Customized") : (isAr ? "تلقائي مستنتج" : "Automatic · inferred")}</p>
        <ul className="mt-1 list-inside list-disc text-sm text-slate-700 dark:text-slate-300">
          {(revision.audience?.programs ?? []).map((program) => <li key={program.program_id}>{isAr ? program.name_ar : program.name_en}</li>)}
        </ul>
      </div>
    </>
  );
}
