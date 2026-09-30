import type { InstructorDashboardCourse } from "@/lib/api/instructor";

export function canManageCourseAnnouncements(course: InstructorDashboardCourse): boolean {
  return Boolean(course.published_revision);
}
