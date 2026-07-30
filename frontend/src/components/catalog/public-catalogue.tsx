"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  getPublicCourse,
  getPublicCourses,
  type PublicCourse,
  type PublicCourseDetail,
} from "@/lib/api/public-catalog";
import { ProblemError } from "@/lib/api/problem";

const copy = {
  ar: {
    catalogue: "الكتالوج",
    loading: "جارٍ تحميل الدورات…",
    empty: "لا توجد دورات منشورة الآن.",
    unavailable: "هذه الدورة غير متاحة.",
    failed: "تعذر تحميل الكتالوج. حاول مرة أخرى.",
    instructor: "المدرب",
    outline: "محتوى الدورة",
    lessons: "دروس",
    price: "السعر الإرشادي",
    preview: "تتوفر معاينة عامة",
    navigation: "التنقل الرئيسي",
    skip: "انتقل إلى المحتوى",
    switchLanguage: "التبديل إلى الإنجليزية",
  },
  en: {
    catalogue: "Catalogue",
    loading: "Loading courses…",
    empty: "No published courses are available yet.",
    unavailable: "This course is unavailable.",
    failed: "The catalogue could not be loaded. Try again.",
    instructor: "Instructor",
    outline: "Course outline",
    lessons: "lessons",
    price: "Price guidance",
    preview: "Public preview available",
    navigation: "Primary navigation",
    skip: "Skip to content",
    switchLanguage: "Switch to Arabic",
  },
};

function CatalogueLanguageToggle() {
  const { locale, setLocale } = useLocale();
  const pathname = usePathname();
  const router = useRouter();
  const nextLocale = locale === "ar" ? "en" : "ar";

  function switchLanguage() {
    setLocale(nextLocale);
    const segments = pathname.split("/");
    segments[1] = nextLocale;
    router.push(segments.join("/"));
  }

  return (
    <button
      type="button"
      onClick={switchLanguage}
      aria-label={copy[locale].switchLanguage}
      className="rounded-md border border-slate-300 px-3 py-2 font-display text-sm font-bold focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-700"
    >
      <span dir="ltr">{nextLocale.toUpperCase()}</span>
    </button>
  );
}

function Shell({ children }: { children: ReactNode }) {
  const { locale, dir } = useLocale();
  const t = copy[locale];

  return (
    <div dir={dir} className="min-h-screen bg-slate-50 text-slate-950">
      <a className="sr-only focus:not-sr-only" href="#catalogue-main">
        {t.skip}
      </a>
      <header className="border-b border-slate-200 bg-white">
        <nav
          aria-label={t.navigation}
          className="mx-auto flex max-w-6xl items-center justify-between px-5 py-4"
        >
          <Link href={`/${locale}/catalog`} className="font-display text-xl font-bold">
            Gradex · {t.catalogue}
          </Link>
          <CatalogueLanguageToggle />
        </nav>
      </header>
      {children}
    </div>
  );
}

function Taxonomy({ course }: { course: PublicCourse }) {
  return (
    <dl className="flex flex-wrap gap-2 text-sm text-slate-600">
      {[course.major, course.subject, course.study_year]
        .filter(Boolean)
        .map((term) => (
          <div key={term!.label} className="rounded-full bg-slate-100 px-3 py-1">
            <dt className="sr-only">Taxonomy</dt>
            <dd>
              {term!.label}
              {term!.code ? ` · ${term!.code}` : ""}
            </dd>
          </div>
        ))}
    </dl>
  );
}

function Price({ course, label }: { course: PublicCourse; label: string }) {
  if (!course.price) return null;

  return (
    <p className="mt-3 text-sm font-semibold text-teal-800">
      {label}: {(course.price.minor_units / 1000).toFixed(3)} {course.price.currency}
    </p>
  );
}

function Failure({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="rounded-lg border border-amber-300 bg-amber-50 p-5 text-amber-950">
      {children}
    </p>
  );
}

export function CatalogueList() {
  const { locale } = useLocale();
  const t = copy[locale];
  const [state, setState] = useState<{ items?: PublicCourse[]; error?: string }>({});

  useEffect(() => {
    setState({});
    getPublicCourses(locale)
      .then((result) => setState({ items: result.items }))
      .catch(() => setState({ error: t.failed }));
  }, [locale, t.failed]);

  return (
    <Shell>
      <main id="catalogue-main" className="mx-auto max-w-6xl px-5 py-10">
        <h1 className="font-display text-4xl font-bold">{t.catalogue}</h1>
        {!state.items && !state.error && (
          <p className="mt-8" aria-live="polite">
            {t.loading}
          </p>
        )}
        {state.error && (
          <div className="mt-8">
            <Failure>{state.error}</Failure>
          </div>
        )}
        {state.items?.length === 0 && <p className="mt-8 text-slate-600">{t.empty}</p>}
        <section aria-label={t.catalogue} className="mt-8 grid gap-5 md:grid-cols-2">
          {state.items?.map((course) => (
            <article key={course.id} className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
              <Taxonomy course={course} />
              <h2 className="mt-4 font-display text-2xl font-bold">
                <Link
                  href={`/${locale}/catalog/${course.slug}`}
                  className="focus-visible:outline focus-visible:outline-2 focus-visible:outline-teal-700"
                >
                  {course.title}
                </Link>
              </h2>
              <p className="mt-2 text-slate-600">
                {t.instructor}: {course.instructor_display_name}
              </p>
              <Price course={course} label={t.price} />
              {course.has_preview && <p className="mt-3 text-sm text-teal-800">{t.preview}</p>}
            </article>
          ))}
        </section>
      </main>
    </Shell>
  );
}

export function CatalogueDetail({ idOrSlug }: { idOrSlug: string }) {
  const { locale } = useLocale();
  const t = copy[locale];
  const [state, setState] = useState<{
    course?: PublicCourseDetail;
    error?: string;
    missing?: boolean;
  }>({});

  useEffect(() => {
    setState({});
    getPublicCourse(idOrSlug, locale)
      .then((course) => setState({ course }))
      .catch((error) => {
        setState(
          error instanceof ProblemError && error.problem.status === 404
            ? { missing: true }
            : { error: t.failed },
        );
      });
  }, [idOrSlug, locale, t.failed]);

  return (
    <Shell>
      <main id="catalogue-main" className="mx-auto max-w-4xl px-5 py-10">
        {!state.course && !state.error && !state.missing && <p aria-live="polite">{t.loading}</p>}
        {state.missing && <Failure>{t.unavailable}</Failure>}
        {state.error && <Failure>{state.error}</Failure>}
        {state.course && (
          <article>
            <Taxonomy course={state.course} />
            <h1 className="mt-4 font-display text-4xl font-bold">{state.course.title}</h1>
            <p className="mt-3 text-slate-600">
              {t.instructor}: {state.course.instructor_display_name}
            </p>
            <Price course={state.course} label={t.price} />
            {state.course.has_preview && <p className="mt-3 text-sm text-teal-800">{t.preview}</p>}
            <p className="mt-8 whitespace-pre-wrap text-lg leading-8">{state.course.description}</p>
            <section className="mt-10" aria-labelledby="outline">
              <h2 id="outline" className="font-display text-2xl font-bold">
                {t.outline}
              </h2>
              <ol className="mt-4 space-y-3">
                {state.course.sections.map((section) => (
                  <li key={section.position} className="flex justify-between rounded-lg border bg-white p-4">
                    <span>{section.title}</span>
                    <span className="text-slate-600">
                      {section.lesson_count} {t.lessons}
                    </span>
                  </li>
                ))}
              </ol>
            </section>
          </article>
        )}
      </main>
    </Shell>
  );
}
