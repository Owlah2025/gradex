import { ProblemError } from "./problem";

/**
 * Renders a server failure as text an Instructor can act on.
 *
 * The server's own reason is preferred over any local wording. Submission
 * rejections carry per-violation codes, and those are surfaced verbatim rather
 * than collapsed into a generic "submission failed": which requirement is
 * missing is the only useful part of that response.
 */
export function describeApiError(error: unknown, locale: "ar" | "en"): string {
  const isAr = locale === "ar";
  if (error instanceof ProblemError) {
    const problem = error.problem as typeof error.problem & {
      // A submission rejection reports `violations`, each naming the missing
      // requirement and the object it applies to; other failures report field
      // `errors`. Both are surfaced, because either one is the server's reason.
      violations?: Array<{ code?: string; target?: string; dimension?: string }>;
    };
    const base = problem.detail || problem.title;
    const violations = ([...(problem.violations ?? []), ...(problem.errors ?? [])] as Array<
      Record<"code" | "target" | "dimension" | "detail", string | undefined>
    >)
      .map((violation) =>
        [violation.code, violation.target, violation.dimension, violation.detail]
          .filter(Boolean)
          .join(" · "),
      )
      .filter(Boolean);
    if (violations.length > 0) {
      return `${base}: ${violations.join(" | ")}`;
    }
    return base;
  }
  if (error instanceof Error && error.message) return error.message;
  return isAr ? "حدث خطأ غير متوقع" : "An unexpected error occurred";
}
