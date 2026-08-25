export type CourseRosterAccessStatus = "ACTIVE" | "EXPIRED" | "REVOKED" | "SUSPENDED" | "DENIED";

export type CourseRosterViewState = "loading" | "error" | "empty" | "ready";

export function courseRosterViewState(
  loading: boolean,
  error: string | null,
  itemCount: number,
): CourseRosterViewState {
  if (loading) return "loading";
  if (error !== null) return "error";
  return itemCount === 0 ? "empty" : "ready";
}

export function courseRosterStatusLabel(
  status: CourseRosterAccessStatus,
  labels: Record<CourseRosterAccessStatus, string>,
): string {
  return labels[status];
}
