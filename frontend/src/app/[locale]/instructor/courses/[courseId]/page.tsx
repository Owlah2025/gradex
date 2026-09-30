import { CourseBuilder } from "@/components/instructor/course-builder";

export default async function InstructorCourseBuilderPage({ params }: { params: Promise<{ courseId: string }> }) {
  const { courseId } = await params;
  return (
    <main id="main">
      <CourseBuilder initialCourseID={courseId} />
    </main>
  );
}
