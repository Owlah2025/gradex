"use client";

import { useCallback, useEffect, useState } from "react";
import {
  archiveCourse,
  delistCourse,
  getCourseLifecycleDirectory,
  relistCourse,
  restoreCourseAccess,
  retireCourse,
  suspendCourseAccess,
  type CourseLifecycleSummary,
  type SuspensionCause,
} from "@/lib/api/catalog";
import { ProblemError } from "@/lib/api/problem";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * AD-12 — the Admin Course lifecycle surface.
 *
 * The lifecycle commands already existed as routes and as an API client; what did not exist was a
 * screen that mounts them, so no Admin could reach delist, relist, retire, archive, suspension or
 * restoration through the product. This workspace is that screen, and it is deliberately built on
 * the Admin lifecycle directory rather than the public catalogue: a delisted, retired or archived
 * Course is invisible publicly by design, and relisting it has to stay possible.
 *
 * The screen holds no lifecycle policy of its own. Every button issues the canonical request and
 * then refetches the directory; what it renders afterwards is the server's state, never an
 * optimistic guess. A refused transition renders the server's refusal.
 */
export function CourseLifecycleWorkspace() {
  const { locale } = useLocale();
  const isAr = locale === "ar";

  const [search, setSearch] = useState("");
  const [courses, setCourses] = useState<CourseLifecycleSummary[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [cause, setCause] = useState<SuspensionCause>("SECURITY");
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    async (query: string) => {
      setLoading(true);
      try {
        setCourses(await getCourseLifecycleDirectory(locale, query));
      } catch (loadError) {
        setError(problemMessage(loadError, isAr ? "تعذّر تحميل الدورات" : "Could not load Courses"));
      } finally {
        setLoading(false);
      }
    },
    [locale, isAr],
  );

  useEffect(() => {
    void load("");
  }, [load]);

  const selected = courses.find((course) => course.id === selectedID) ?? null;

  const invoke = async (label: string, action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }
    setBusy(true);
    setMessage(null);
    setError(null);
    try {
      await action(csrf);
      setMessage(isAr ? `تم ${label}` : `${label} completed`);
    } catch (actionError) {
      setError(problemMessage(actionError, isAr ? "فشلت العملية" : "Operation failed"));
    } finally {
      // The directory is reread whether the command succeeded or was refused, so the rendered
      // state is always the server's and a refusal cannot leave a stale screen behind.
      await load(search);
      setBusy(false);
    }
  };

  const withReason = (run: () => void) => {
    if (!reason.trim()) {
      setError(isAr ? "السبب إجباري لهذا الإجراء" : "A reason is required for this action");
      return;
    }
    run();
  };

  return (
    <section className="space-y-4 p-4" data-testid="course-lifecycle-workspace">
      <header className="space-y-1">
        <h1 className="text-lg font-semibold">
          {isAr ? "حالة الدورة وإجراءات الطوارئ" : "Course Lifecycle & Emergency Controls"}
        </h1>
        <p className="text-xs text-slate-500">
          {isAr
            ? "الحذف غير معروض هنا؛ الدورات ذات الوصول القائم يجب أرشفتها."
            : "Deletion is intentionally absent here; Courses with existing access must be archived."}
        </p>
      </header>

      <form
        className="flex flex-wrap gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          void load(search);
        }}
      >
        <input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={isAr ? "ابحث بعنوان الدورة" : "Search by Course title"}
          aria-label={isAr ? "ابحث بعنوان الدورة" : "Search by Course title"}
          data-testid="lifecycle-course-search"
          className="p-2 border rounded text-sm bg-white dark:bg-slate-900"
        />
        <button
          type="submit"
          data-testid="lifecycle-course-search-submit"
          className="px-3 py-2 rounded bg-slate-700 text-white text-xs"
        >
          {isAr ? "بحث" : "Search"}
        </button>
      </form>

      <ul className="space-y-1" data-testid="lifecycle-course-list">
        {loading && <li className="text-xs text-slate-500">{isAr ? "جارٍ التحميل…" : "Loading…"}</li>}
        {!loading && courses.length === 0 && (
          <li className="text-xs text-slate-500" data-testid="lifecycle-course-list-empty">
            {isAr ? "لا توجد دورات مطابقة" : "No Courses match this search"}
          </li>
        )}
        {courses.map((course) => (
          <li
            key={course.id}
            data-testid="lifecycle-course-row"
            data-lifecycle-state={course.lifecycle}
            className="flex flex-wrap items-center justify-between gap-2 rounded border border-slate-200 dark:border-slate-700 p-2"
          >
            <span className="text-sm">
              {isAr ? course.title_ar : course.title_en}
              <span className="ms-2 text-xs text-slate-500">{lifecycleLabel(course, isAr)}</span>
            </span>
            <button
              onClick={() => {
                setSelectedID(course.id);
                setMessage(null);
                setError(null);
              }}
              className="px-3 py-1 rounded bg-blue-700 text-white text-xs"
            >
              {isAr ? "إدارة" : "Manage"}
            </button>
          </li>
        ))}
      </ul>

      {selected && (
        <div
          className="space-y-3 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50 p-4"
          data-testid="lifecycle-selected-course"
          data-lifecycle-state={selected.lifecycle}
          data-access-suspended={selected.access_suspended_at ? "true" : "false"}
          data-retired={selected.retired_at ? "true" : "false"}
        >
          <h2 className="text-sm font-semibold" data-testid="lifecycle-selected-title">
            {isAr ? selected.title_ar : selected.title_en}
          </h2>
          <p className="text-xs text-slate-500" data-testid="lifecycle-selected-state">
            {lifecycleLabel(selected, isAr)}
          </p>

          <div className="flex flex-wrap gap-2">
            <button
              disabled={busy}
              data-testid="lifecycle-delist"
              onClick={() =>
                void invoke(isAr ? "إلغاء الإدراج" : "Delist", (csrf) =>
                  delistCourse({ courseID: selected.id, locale, csrf }),
                )
              }
              className="px-3 py-2 rounded bg-amber-600 text-white text-xs disabled:opacity-50"
            >
              {isAr ? "إلغاء الإدراج" : "Delist"}
            </button>
            <button
              disabled={busy}
              data-testid="lifecycle-relist"
              onClick={() =>
                void invoke(isAr ? "إعادة الإدراج" : "Relist", (csrf) =>
                  relistCourse({ courseID: selected.id, locale, csrf }),
                )
              }
              className="px-3 py-2 rounded bg-emerald-600 text-white text-xs disabled:opacity-50"
            >
              {isAr ? "إعادة الإدراج" : "Relist"}
            </button>
            <button
              disabled={busy}
              data-testid="lifecycle-retire"
              onClick={() =>
                void invoke(isAr ? "التقاعد" : "Retire", (csrf) =>
                  retireCourse({ courseID: selected.id, locale, csrf }),
                )
              }
              className="px-3 py-2 rounded bg-slate-700 text-white text-xs disabled:opacity-50"
            >
              {isAr ? "تقاعد" : "Retire"}
            </button>
            <button
              disabled={busy}
              data-testid="lifecycle-archive"
              onClick={() =>
                void invoke(isAr ? "الأرشفة" : "Archive", (csrf) =>
                  archiveCourse({ courseID: selected.id, locale, csrf }),
                )
              }
              className="px-3 py-2 rounded bg-rose-700 text-white text-xs disabled:opacity-50"
            >
              {isAr ? "أرشفة" : "Archive"}
            </button>
          </div>

          <div className="grid gap-2 sm:grid-cols-3">
            <select
              value={cause}
              onChange={(event) => setCause(event.target.value as SuspensionCause)}
              aria-label={isAr ? "سبب الإيقاف" : "Suspension cause"}
              data-testid="lifecycle-suspension-cause"
              className="p-2 border rounded text-xs bg-white dark:bg-slate-900"
            >
              <option value="LEGAL">{isAr ? "قانوني" : "Legal"}</option>
              <option value="SECURITY">{isAr ? "أمني" : "Security"}</option>
              <option value="MALWARE">{isAr ? "برمجيات خبيثة" : "Malware"}</option>
              <option value="SEVERE_MODERATION">{isAr ? "إشراف جسيم" : "Severe moderation"}</option>
            </select>
            <input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder={isAr ? "سبب الإيقاف أو الاستعادة" : "Suspension or restoration reason"}
              aria-label={isAr ? "سبب الإيقاف أو الاستعادة" : "Suspension or restoration reason"}
              data-testid="lifecycle-suspension-reason"
              className="p-2 border rounded text-xs bg-white dark:bg-slate-900"
            />
            <div className="flex gap-2">
              <button
                disabled={busy}
                data-testid="lifecycle-suspend"
                onClick={() =>
                  withReason(() =>
                    void invoke(isAr ? "إيقاف الوصول" : "Access suspension", (csrf) =>
                      suspendCourseAccess({
                        courseID: selected.id,
                        locale,
                        csrf,
                        cause,
                        reason: reason.trim(),
                      }),
                    ),
                  )
                }
                className="px-3 py-2 rounded bg-rose-600 text-white text-xs disabled:opacity-50"
              >
                {isAr ? "إيقاف" : "Suspend"}
              </button>
              <button
                disabled={busy}
                data-testid="lifecycle-restore"
                onClick={() =>
                  withReason(() =>
                    void invoke(isAr ? "استعادة الوصول" : "Access restoration", (csrf) =>
                      restoreCourseAccess({ courseID: selected.id, locale, csrf, reason: reason.trim() }),
                    ),
                  )
                }
                className="px-3 py-2 rounded bg-emerald-700 text-white text-xs disabled:opacity-50"
              >
                {isAr ? "استعادة" : "Restore"}
              </button>
            </div>
          </div>

          {message && (
            <p role="status" data-testid="lifecycle-message" className="text-xs text-emerald-700 dark:text-emerald-300">
              {message}
            </p>
          )}
          {error && (
            <p role="alert" data-testid="lifecycle-error" className="text-xs text-rose-700 dark:text-rose-300">
              {error}
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/** The rendered state names every authority that is currently acting on the Course, not just one. */
function lifecycleLabel(course: CourseLifecycleSummary, isAr: boolean): string {
  const parts: string[] = [course.lifecycle];
  if (course.retired_at) {
    parts.push(isAr ? "متقاعدة" : "Retired");
  }
  if (course.access_suspended_at) {
    parts.push(isAr ? "الوصول موقوف" : "Access suspended");
  }
  return parts.join(" · ");
}

function problemMessage(cause: unknown, fallback: string): string {
  if (cause instanceof ProblemError) {
    return cause.problem.detail || cause.problem.title || fallback;
  }
  if (cause instanceof Error && cause.message) {
    return cause.message;
  }
  return fallback;
}
