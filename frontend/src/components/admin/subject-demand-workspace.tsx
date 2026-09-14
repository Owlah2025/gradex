"use client";

import * as React from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { describeApiError } from "@/lib/api/api-error";
import { Alert } from "@/components/ui/alert";
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
  TableSkeletonRows,
} from "@/components/ui/table";
import {
  listSubjectDemandCounts,
  type SubjectDemandCount,
} from "@/lib/api/subject-catalogue";
import { listInstitutions, type Institution } from "@/lib/api/academic";

/**
 * Admin Subject demand (D-106 §6).
 *
 * # WHAT THIS IS FOR
 *
 * Deciding which Course to produce next. That is the only question these
 * numbers answer, and the surface is shaped to answer it: which Subjects are
 * students asking for, at which university, and is one already taught.
 *
 * # WHAT IT DELIBERATELY DOES NOT SHOW
 *
 * Who asked. The endpoint returns counts only, and no per-Student roster exists
 * to render. Knowing what to build needs a count; knowing who wants it is a
 * different question with a different privacy weight that this workflow never
 * asks.
 *
 * Demand numbers are Admin-only. Nothing on a public or Student surface renders
 * them: a visible count would turn a prioritisation signal into a popularity
 * display, and a Subject with one request would read as a failed product rather
 * than as an early one.
 *
 * # SERVED DEMAND IS NOT A DEFECT
 *
 * A Subject that already has a published Course can still accumulate demand.
 * That is signal, not noise — it says the existing Course is not reaching the
 * students who want it — so those rows are shown and flagged rather than
 * filtered away.
 */

type SortKey = "students" | "subject";
type Availability = "all" | "served" | "unserved";

const copy = {
  ar: {
    title: "طلبات الطلبة على المواد",
    intro:
      "عدد الطلبة اللي طلبوا كل مادة. الأرقام دي تظهر للأدمن فقط، وهي مدخل لترتيب أولويات الإنتاج — مش وعد بإنتاج كورس.",
    loading: "جارٍ تحميل الطلبات…",
    empty: "لا توجد طلبات مسجّلة بعد.",
    institution: "الجامعة",
    allInstitutions: "كل الجامعات",
    availability: "التوفر",
    all: "الكل",
    served: "متاحة",
    unserved: "غير متاحة",
    code: "الرمز",
    subject: "المادة",
    students: "عدد الطلبة",
    status: "الحالة",
    sortBy: "ترتيب حسب",
    sortStudents: "عدد الطلبة",
    sortSubject: "اسم المادة",
    servedNote: "لها كورس بالفعل",
  },
  en: {
    title: "Subject demand",
    intro:
      "How many students asked for each subject. These numbers are Admin-only and are an input to production priority — not a commitment to produce a course.",
    loading: "Loading demand…",
    empty: "No demand has been recorded yet.",
    institution: "University",
    allInstitutions: "All universities",
    availability: "Availability",
    all: "All",
    served: "Served",
    unserved: "Unserved",
    code: "Code",
    subject: "Subject",
    students: "Students",
    status: "Status",
    sortBy: "Sort by",
    sortStudents: "Student count",
    sortSubject: "Subject name",
    servedNote: "already has a course",
  },
};

export function SubjectDemandWorkspace() {
  const { locale } = useLocale();
  const t = copy[locale];

  const [counts, setCounts] = React.useState<SubjectDemandCount[] | null>(null);
  const [institutions, setInstitutions] = React.useState<Institution[]>([]);
  const [institution, setInstitution] = React.useState("");
  const [availability, setAvailability] = React.useState<Availability>("all");
  const [sort, setSort] = React.useState<SortKey>("students");
  const [message, setMessage] = React.useState("");

  React.useEffect(() => {
    let live = true;
    listInstitutions(locale)
      .then((items) => {
        if (live) setInstitutions(items.filter((item) => !item.retired_at));
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [locale]);

  React.useEffect(() => {
    let live = true;
    setCounts(null);
    setMessage("");
    // Institution narrowing is applied by the server, which already scopes and
    // orders the aggregate. Availability and sort are applied below, over a
    // bounded page, so changing either does not re-query.
    listSubjectDemandCounts(locale, {
      institution: institution || undefined,
      limit: 500,
    })
      .then((items) => {
        if (live) setCounts(items);
      })
      .catch((error: unknown) => {
        if (live) {
          setCounts([]);
          setMessage(describeApiError(error, locale));
        }
      });
    return () => {
      live = false;
    };
  }, [locale, institution]);

  const rows = React.useMemo(() => {
    const visible = (counts ?? []).filter((row) => {
      if (availability === "served") return row.served;
      if (availability === "unserved") return !row.served;
      return true;
    });
    const title = (row: SubjectDemandCount) =>
      locale === "ar" ? row.subject_title_ar : row.subject_title_en;
    return [...visible].sort((left, right) => {
      if (sort === "subject") return title(left).localeCompare(title(right), locale);
      // Highest demand first, then by name so equal counts hold a stable,
      // readable order rather than the database's arrival order.
      if (right.students !== left.students) return right.students - left.students;
      return title(left).localeCompare(title(right), locale);
    });
  }, [counts, availability, sort, locale]);

  return (
    <section className="p-6">
      <h1 className="font-display text-2xl font-bold text-foreground">{t.title}</h1>
      <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t.intro}</p>

      <div className="mt-6 flex flex-wrap items-end gap-4">
        <div>
          <label
            htmlFor="demand-institution"
            className="block text-sm font-semibold text-foreground"
          >
            {t.institution}
          </label>
          <select
            id="demand-institution"
            className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
            value={institution}
            onChange={(event) => setInstitution(event.target.value)}
            data-testid="demand-institution-filter"
          >
            <option value="">{t.allInstitutions}</option>
            {institutions.map((option) => (
              <option key={option.id} value={option.slug}>
                {locale === "ar" ? option.name_ar : option.name_en}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label
            htmlFor="demand-availability"
            className="block text-sm font-semibold text-foreground"
          >
            {t.availability}
          </label>
          <select
            id="demand-availability"
            className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
            value={availability}
            onChange={(event) => setAvailability(event.target.value as Availability)}
            data-testid="demand-availability-filter"
          >
            <option value="all">{t.all}</option>
            <option value="served">{t.served}</option>
            <option value="unserved">{t.unserved}</option>
          </select>
        </div>

        <div>
          <label htmlFor="demand-sort" className="block text-sm font-semibold text-foreground">
            {t.sortBy}
          </label>
          <select
            id="demand-sort"
            className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
            value={sort}
            onChange={(event) => setSort(event.target.value as SortKey)}
            data-testid="demand-sort"
          >
            <option value="students">{t.sortStudents}</option>
            <option value="subject">{t.sortSubject}</option>
          </select>
        </div>
      </div>

      {message ? (
        <div className="mt-4 max-w-lg">
          <Alert tone="error" title={message} />
        </div>
      ) : null}

      <div className="mt-6">
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableHeaderCell scope="col">{t.institution}</TableHeaderCell>
                <TableHeaderCell scope="col">{t.code}</TableHeaderCell>
                <TableHeaderCell scope="col">{t.subject}</TableHeaderCell>
                <TableHeaderCell scope="col">{t.students}</TableHeaderCell>
                <TableHeaderCell scope="col">{t.status}</TableHeaderCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {counts === null ? (
                <TableSkeletonRows columns={5} />
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5}>{t.empty}</TableCell>
                </TableRow>
              ) : (
                rows.map((row) => (
                  <TableRow key={row.subject_id} data-testid="demand-row">
                    <TableCell>
                      <bdi>{row.institution_name_en}</bdi>
                    </TableCell>
                    <TableCell>
                      <bdi>{row.subject_code ?? "—"}</bdi>
                    </TableCell>
                    <TableCell>
                      <bdi>
                        {locale === "ar" ? row.subject_title_ar : row.subject_title_en}
                      </bdi>
                    </TableCell>
                    <TableCell data-testid="demand-students">{row.students}</TableCell>
                    <TableCell>
                      <span
                        className={
                          row.served
                            ? "rounded-full bg-gx-success-soft px-3 py-1 text-xs font-bold text-gx-navy"
                            : "rounded-full bg-muted px-3 py-1 text-xs font-bold text-muted-foreground"
                        }
                        title={row.served ? t.servedNote : undefined}
                      >
                        {row.served ? t.served : t.unserved}
                      </span>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </div>
    </section>
  );
}
