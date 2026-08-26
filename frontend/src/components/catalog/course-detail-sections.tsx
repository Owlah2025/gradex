import Link from "next/link";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { PublicCourseDetail } from "@/lib/api/public-catalog";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Eyebrow, Prose, SectionHeading } from "@/components/ui/typography";
import { academicFacts, instructorInitials } from "./course-detail-presentation";

type Copy = Dictionary["courseDetail"];

/**
 * The public Course Details sections that only draw what they are handed.
 *
 * They are server-renderable on purpose — none of them holds state, so the interactive parts of the
 * page (the outline's disclosure, the preview player, the access panel) stay the only client
 * boundaries and the bulk of the page is markup.
 */

/**
 * The first viewport: what this course is, whose it is, and where it belongs.
 *
 * Kept deliberately short. A masthead tall enough to need scrolling before the first real sentence
 * is a decoration tax the reader pays on every visit, so the hero carries the university it belongs
 * to, the title, and the instructor's name — and then gets out of the way.
 */
export function CourseHero({
  course,
  copy,
  className,
}: {
  course: PublicCourseDetail;
  copy: Copy;
  className?: string;
}) {
  return (
    <header className={className}>
      {course.university ? (
        <Eyebrow data-testid="course-detail-eyebrow">
          <bdi>{course.university.label}</bdi>
        </Eyebrow>
      ) : null}

      <SectionHeading as="h1" className="mt-2" data-testid="course-detail-title">
        <bdi>{course.title}</bdi>
      </SectionHeading>

      <p
        className="mt-4 text-[16.5px] leading-relaxed text-muted-foreground"
        data-testid="course-detail-instructor-line"
      >
        {copy.taughtBy}{" "}
        <bdi className="font-semibold text-foreground">
          {course.instructor_display_name}
        </bdi>
      </p>
    </header>
  );
}

/**
 * "Is this course for me?", answered from the study plan rather than from the database.
 *
 * The previous version of this page rendered the same four terms as unlabelled pills under an
 * `sr-only` heading reading "Taxonomy" — an English word, hardcoded past the Arabic dictionary,
 * naming an internal data structure rather than anything a student recognises. Each term is now
 * named for what it is to a reader, and the Programs the Course is studied in are listed beside
 * them, because that is the field that actually answers the question.
 */
export function CourseAcademicContext({
  course,
  copy,
}: {
  course: PublicCourseDetail;
  copy: Copy;
}) {
  const facts = academicFacts(course, {
    university: copy.university,
    major: copy.major,
    subject: copy.subject,
    level: copy.level,
  });
  const audience = course.program_audience ?? [];
  if (facts.length === 0 && audience.length === 0) return null;

  return (
    <section
      className="mt-12"
      aria-labelledby="course-academic-heading"
      data-testid="course-academic-context"
    >
      <h2
        id="course-academic-heading"
        className="font-display text-2xl font-bold text-foreground"
      >
        {copy.academicHeading}
      </h2>
      <Prose className="mt-2 max-w-2xl text-[15.5px]">{copy.academicLead}</Prose>

      {facts.length > 0 ? (
        <dl className="mt-6 grid gap-x-8 gap-y-5 sm:grid-cols-2">
          {facts.map((fact) => (
            <div key={fact.key} data-testid={`course-academic-${fact.key}`}>
              <dt className="text-sm text-muted-foreground">{fact.label}</dt>
              <dd className="mt-1 font-display text-[17px] font-bold leading-snug text-foreground">
                <bdi>{fact.value}</bdi>
                {fact.code ? (
                  <>
                    {" "}
                    <span className="sr-only">{copy.subjectCode}: </span>
                    <bdi className="font-mono text-[13px] font-semibold text-muted-foreground">
                      {fact.code}
                    </bdi>
                  </>
                ) : null}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}

      {audience.length > 0 ? (
        <div className="mt-6" data-testid="course-academic-audience">
          <p className="text-sm text-muted-foreground">{copy.audience}</p>
          <ul className="mt-2 flex flex-wrap gap-2">
            {audience.map((program) => (
              <li key={program}>
                <Badge variant="neutral" className="px-3 py-1.5 text-[12.5px]">
                  <bdi>{program}</bdi>
                </Badge>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
}

/**
 * Who wrote the course.
 *
 * Authorship, not authority. The public contract carries one field about this person — a display
 * name — so that is all this section may state. No biography, no degree, no institution, no years
 * of teaching, no student total: every one of those would have to be invented, and a fabricated
 * credential on a page a student uses to judge relevance is the worst possible thing to invent.
 */
export function CourseInstructor({
  course,
  copy,
}: {
  course: PublicCourseDetail;
  copy: Copy;
}) {
  return (
    <section
      className="mt-12"
      aria-labelledby="course-instructor-heading"
      data-testid="course-instructor"
    >
      <h2
        id="course-instructor-heading"
        className="font-display text-2xl font-bold text-foreground"
      >
        {copy.instructorHeading}
      </h2>
      <div className="mt-5 flex items-start gap-4">
        <Avatar size="lg" aria-hidden>
          <AvatarFallback>{instructorInitials(course.instructor_display_name)}</AvatarFallback>
        </Avatar>
        <div className="min-w-0">
          <p className="font-display text-[19px] font-bold leading-snug text-foreground">
            <bdi>{course.instructor_display_name}</bdi>
          </p>
          <p className="mt-1 text-sm text-muted-foreground">{copy.instructorRole}</p>
          <Prose className="mt-3 max-w-xl text-[15.5px]">{copy.instructorNote}</Prose>
        </div>
      </div>
    </section>
  );
}

/**
 * The way back into the catalogue the visitor actually came from.
 *
 * `href` is decided by the caller from the stored academic context, so a student who arrived from a
 * catalogue narrowed to their university and program returns to that same narrowed catalogue rather
 * than to an unfiltered list they then have to re-narrow.
 */
export function BackToCatalogue({ href, label }: { href: string; label: string }) {
  return (
    <Link
      href={href}
      data-testid="course-detail-back"
      className="inline-flex items-center gap-2 rounded-md text-sm font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
    >
      {label}
    </Link>
  );
}
