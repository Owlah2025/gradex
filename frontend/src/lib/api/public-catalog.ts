import { isProblem, ProblemError } from "./problem";

export type PublicTaxonomy = { label: string; code?: string };
export type PublicPrice = { minor_units: number; currency: "KWD" };
export type PublicCourse = {
  id: string; slug: string; title: string; instructor_display_name: string;
  major?: PublicTaxonomy; subject?: PublicTaxonomy; study_year?: PublicTaxonomy;
  price?: PublicPrice; has_preview: boolean;
};
export type PublicCourseDetail = PublicCourse & {
  description: string;
  sections: { title: string; position: number; lesson_count: number }[];
};
export type PublicCourseList = { items: PublicCourse[]; page: number; page_size: number; total: number };

async function publicRequest<T>(path: string, locale: "ar" | "en"): Promise<T> {
  const response = await fetch(`/api/v1/catalog${path}`, {
    headers: { Accept: "application/json, application/problem+json", "Accept-Language": locale },
    cache: "no-store",
  });
  const body: unknown = await response.json();
  if (!response.ok) throw isProblem(body) ? new ProblemError(body) : new Error("Public catalogue request failed");
  return body as T;
}

export function getPublicCourses(locale: "ar" | "en", query = "") {
  const parameters = new URLSearchParams();
  if (query !== "") parameters.set("q", query);
  const suffix = parameters.size === 0 ? "" : `?${parameters}`;
  return publicRequest<PublicCourseList>(`/courses${suffix}`, locale);
}
export function getPublicCourse(idOrSlug: string, locale: "ar" | "en") { return publicRequest<PublicCourseDetail>(`/courses/${encodeURIComponent(idOrSlug)}`, locale); }
