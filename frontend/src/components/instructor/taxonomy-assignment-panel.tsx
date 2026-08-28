"use client";

import { useEffect, useState } from "react";
import { TaxonomyTermSelect } from "@/components/catalog/taxonomy-term-select";
import {
  assignInstructorTaxonomy,
  getTaxonomyTerms,
  type OwnedCourseSummary,
  type TaxonomyTerm,
} from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { courseDisplayTitle } from "./course-standing";

type LegacyLabels = Dictionary["instructor"]["legacyTaxonomy"];

/**
 * Legacy taxonomy compatibility (D-093 §6). Removed at T5.
 *
 * It edits `major_term_id` / `subject_term_id`, which only a LEGACY_TAXONOMY course carries, and
 * disappears entirely once an Instructor owns none. The server refuses these fields on an academic
 * course regardless of what is rendered, so this is presentation and never the control.
 *
 * It used to fetch the owned-course list itself. That was the studio's third request for the same
 * list on one page load — the builder's, the price panel's, and this one's — and it existed only
 * to populate a course selector beside two others. The courses are handed in now.
 *
 * Its single `message` string carried successes and failures alike, in the same grey paragraph
 * under the same `role="status"`, so a refused save read exactly like a saved one.
 */
export function TaxonomyAssignmentPanel({
  courses,
  labels,
}: {
  courses: OwnedCourseSummary[];
  labels: LegacyLabels;
}) {
  const { locale } = useLocale();
  const [terms, setTerms] = useState<TaxonomyTerm[]>([]);
  const [courseID, setCourseID] = useState("");
  const [majorTermID, setMajorTermID] = useState("");
  const [subjectTermID, setSubjectTermID] = useState("");
  const [notice, setNotice] = useState<{ tone: "ok" | "fail"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    getTaxonomyTerms(locale)
      .then(setTerms)
      .catch(() => setNotice({ tone: "fail", text: labels.loadFailed }));
  }, [locale, labels.loadFailed]);

  const selectedCourse = courses.find((course) => course.id === courseID);
  const revision = selectedCourse?.editable_revision;

  const selectCourse = (selectedID: string) => {
    setCourseID(selectedID);
    const selected = courses.find((course) => course.id === selectedID)?.editable_revision;
    setMajorTermID(selected?.major_term_id ?? "");
    setSubjectTermID(selected?.subject_term_id ?? "");
    setNotice(null);
  };

  const save = async () => {
    if (!revision?.id || !majorTermID || !subjectTermID) {
      setNotice({ tone: "fail", text: labels.incomplete });
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setNotice({ tone: "fail", text: labels.saveFailed });
      return;
    }
    setBusy(true);
    setNotice(null);
    try {
      await assignInstructorTaxonomy({
        courseID,
        revisionID: revision.id,
        majorTermID,
        subjectTermID,
        locale,
        csrf,
      });
      setNotice({ tone: "ok", text: labels.saved });
    } catch (error) {
      setNotice({
        tone: "fail",
        text: error instanceof Error && error.message ? error.message : labels.saveFailed,
      });
    } finally {
      setBusy(false);
    }
  };

  const editable = courses.filter((course) => course.editable_revision?.id);

  return (
    <section
      className="rounded-lg border border-border bg-card p-5"
      aria-labelledby="legacy-taxonomy-title"
      data-testid="legacy-taxonomy-panel"
    >
      <h2 id="legacy-taxonomy-title" className="font-display text-base font-bold text-foreground">
        {labels.title}
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">{labels.lead}</p>

      <div className="mt-4 grid gap-4 md:grid-cols-3">
        <Field label={labels.courseLabel} htmlFor="taxonomy-course">
          <Select
            id="taxonomy-course"
            value={courseID}
            onChange={(event) => selectCourse(event.target.value)}
            data-testid="taxonomy-course"
          >
            <option value="">{labels.coursePlaceholder}</option>
            {editable.map((course) => (
              <option key={course.id} value={course.id}>
                {courseDisplayTitle(course, locale, labels.coursePlaceholder)}
              </option>
            ))}
          </Select>
        </Field>
        <TaxonomyTermSelect
          kind="MAJOR"
          locale={locale}
          terms={terms}
          value={majorTermID}
          onChange={setMajorTermID}
          disabled={!revision?.id || busy}
        />
        <TaxonomyTermSelect
          kind="SUBJECT"
          locale={locale}
          terms={terms}
          value={subjectTermID}
          onChange={setSubjectTermID}
          disabled={!revision?.id || busy}
        />
      </div>

      <Button
        type="button"
        size="sm"
        className="mt-4"
        disabled={busy || !revision?.id}
        onClick={save}
        data-testid="save-taxonomy"
      >
        {busy ? labels.saving : labels.save}
      </Button>

      {notice ? (
        /* A refusal is announced as one, and painted as one. */
        <p
          role={notice.tone === "fail" ? "alert" : "status"}
          data-testid="taxonomy-notice"
          className={
            notice.tone === "fail"
              ? "mt-3 text-sm font-medium text-destructive"
              : "mt-3 text-sm text-muted-foreground"
          }
        >
          {notice.text}
        </p>
      ) : null}
    </section>
  );
}
