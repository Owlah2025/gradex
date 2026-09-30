import { CourseAnalytics } from "@/components/instructor/course-analytics";

export default async function InstructorCourseAnalyticsPage({ params }: { params: Promise<{ courseId: string }> }) {
  const { courseId } = await params;
  return (
    <main id="main">
      <CourseAnalytics courseID={courseId} />
    </main>
  );
}
